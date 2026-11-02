import importlib.util
import io
import json
import tempfile
import unittest
from unittest.mock import patch
from pathlib import Path

spec = importlib.util.spec_from_file_location('release_update', Path(__file__).resolve().parents[1] / 'release_update.py')
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)


class CandidateTests(unittest.TestCase):
    def test_release_preserves_instance_policy_and_adds_new_runtime(self):
        raw = {'services': {
            'backend': {'image': 'old-api@sha256:a', 'environment': {'JWT_SECRET': '${JWT_SECRET}', 'DEV_MACHINE_DOMAIN': 'private.example'}},
            'frontend': {'image': 'old-web@sha256:b'},
            'machine-manager': {'image': 'old-manager', 'environment': {'DEV_MACHINE_DOMAIN': 'private.example'}, 'networks': ['private']},
            'machine-gateway': {'image': 'old-gateway', 'environment': {}},
            'machine-gateway-db-provision': {'volumes': ['./gateway-role.sql:/etc/kuayle/gateway-role.sql:ro']},
            'updater': {'image': 'keep-me'}, 'postgres': {'image': 'keep-db'},
        }, 'networks': {'private': {'internal': True}}}
        reference = {'services': {
            'backend': {'environment': {'DEV_MACHINE_DOMAIN': 'wrong.example', 'DEV_MACHINE_HEADLESS_IMAGE': '${DEV_MACHINE_HEADLESS_IMAGE:-default}'}},
            'machine-manager': {'build': {'context': '../BE', 'dockerfile': 'Dockerfile.machine-manager'}, 'environment': {'DEV_MACHINE_HEADLESS_IMAGE': '${DEV_MACHINE_HEADLESS_IMAGE:-default}'}},
            'machine-gateway': {'build': {'context': '../BE', 'dockerfile': 'Dockerfile.machine-gateway'}, 'environment': {}},
            'dev-machine-headless': {'profiles': ['dev-machine-images'], 'image': '${DEV_MACHINE_HEADLESS_IMAGE:-default}', 'build': {'context': '../devmachine', 'dockerfile': 'headless/Dockerfile'}},
        }}
        candidate, builds = m.make_candidate(raw, reference, Path('/repo/releases/abc'), 'instance', 'v1.2.3', {'backend': 'api@sha256:new', 'frontend': 'web@sha256:new'}, True)
        services = candidate['services']
        self.assertEqual(services['backend']['environment']['JWT_SECRET'], '${JWT_SECRET}')
        self.assertEqual(services['backend']['environment']['DEV_MACHINE_DOMAIN'], 'private.example')
        self.assertEqual(services['machine-manager']['networks'], ['private'])
        self.assertEqual(services['postgres'], raw['services']['postgres'])
        self.assertEqual(services['updater'], raw['services']['updater'])
        self.assertEqual(services['backend']['image'], 'api@sha256:new')
        self.assertEqual(services['backend']['environment']['DEV_MACHINE_HEADLESS_IMAGE'], services['dev-machine-headless']['image'])
        self.assertEqual(services['machine-manager']['environment']['DEV_MACHINE_HEADLESS_IMAGE'], services['dev-machine-headless']['image'])
        self.assertEqual(services['dev-machine-headless']['build']['context'], '/repo/releases/abc/devmachine')
        self.assertIn('dev-machine-headless', builds)
        self.assertEqual(raw['services']['backend']['image'], 'old-api@sha256:a')
        published = {'machine-manager': 'ghcr.io/x/kuayle-machine-manager@sha256:m',
                     'machine-gateway': 'ghcr.io/x/kuayle-machine-gateway@sha256:g',
                     'dev-machine-headless': 'ghcr.io/x/kuayle-dev-machine-headless@sha256:h'}
        pinned, builds = m.make_candidate(raw, reference, Path('/repo/releases/abc'), 'instance', 'v1.2.3', {'backend': 'api@sha256:new', 'frontend': 'web@sha256:new'}, True, published)
        self.assertEqual(builds, [])
        for name, image in published.items():
            self.assertEqual(pinned['services'][name]['image'], image)
            self.assertNotIn('build', pinned['services'][name])
        for consumer in ('backend', 'machine-manager'):
            self.assertEqual(pinned['services'][consumer]['environment']['DEV_MACHINE_HEADLESS_IMAGE'], published['dev-machine-headless'])
        self.assertEqual(pinned['services']['machine-manager']['networks'], ['private'])
        partial, builds = m.make_candidate(raw, reference, Path('/repo/releases/abc'), 'instance', 'v1.2.3', {'backend': 'new', 'frontend': 'new'}, True, {'machine-manager': published['machine-manager']})
        self.assertEqual(builds, ['machine-gateway', 'dev-machine-headless'])
        self.assertEqual(partial['services']['machine-gateway']['image'], 'local/instance-machine-gateway:v1.2.3')
        disabled, builds = m.make_candidate(raw, reference, Path('/repo/releases/abc'), 'instance', 'v1.2.3', {'backend': 'new', 'frontend': 'new'}, False)
        self.assertEqual(builds, [])
        self.assertNotIn('dev-machine-headless', disabled['services'])


class ActivationTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.config = self.root / 'production.json'
        self.config.write_text('{"old": true}')
        self.candidate = self.root / 'candidate.json'
        self.candidate.write_text('{"new": true}')
        self.backup = self.root / 'backup'
        self.backup.mkdir()
        self.calls = []
        self.fail = None
        self.updater = m.ReleaseUpdater(self.root, self.config, 'test-project')
        def compose(*args, **kwargs):
            self.calls.append(args)
            if self.fail and self.fail(args):
                raise RuntimeError('injected failure')
            if 'stdout' in kwargs:
                kwargs['stdout'].write(b'database backup')
            return ''
        self.updater.compose = compose

    def test_success_backs_up_before_migration_and_waits_for_health(self):
        self.updater.activate(self.candidate, self.backup, True, ['backend', 'machine-manager', 'machine-gateway'])
        stop = next(i for i,x in enumerate(self.calls) if x[0] == 'stop')
        dump = next(i for i,x in enumerate(self.calls) if x[0] == 'exec')
        migrate = next(i for i,x in enumerate(self.calls) if 'migrate' in x)
        self.assertLess(stop, dump)
        self.assertLess(dump, migrate)
        self.assertTrue((self.backup / 'database.dump').stat().st_size)
        self.assertEqual(json.loads(self.config.read_text()), {'new': True})
        self.assertFalse(self.updater.marker.exists())
        self.assertEqual(sum(x[0] == 'up' and '--wait' in x for x in self.calls), 2)

    def test_backup_failure_restores_same_containers_without_recreating(self):
        self.fail = lambda args: args[0] == 'exec'
        with self.assertRaises(RuntimeError):
            self.updater.activate(self.candidate, self.backup, True, ['backend', 'machine-manager', 'machine-gateway'])
        self.assertEqual(json.loads(self.config.read_text()), {'old': True})
        self.assertIn(('start', 'backend', 'machine-manager', 'machine-gateway'), self.calls)
        self.assertFalse(any(x[0] == 'up' for x in self.calls))
        self.assertFalse(self.updater.marker.exists())

    def test_migration_failure_keeps_old_writers_stopped_and_marker(self):
        self.fail = lambda args: 'migrate' in args
        with self.assertRaises(RuntimeError):
            self.updater.activate(self.candidate, self.backup, True, ['backend', 'machine-manager', 'machine-gateway'])
        self.assertTrue(self.updater.marker.exists())
        self.assertFalse(any(x[0] in ('start', 'up') for x in self.calls))
        self.assertEqual(json.loads(self.config.read_text()), {'new': True})

    def test_health_failure_keeps_marker_and_stops_new_writers(self):
        self.fail = lambda args: args[0] == 'up'
        with self.assertRaises(RuntimeError):
            self.updater.activate(self.candidate, self.backup, False, ['backend'])
        self.assertTrue(self.updater.marker.exists())
        self.assertFalse(any(x[0] == 'start' for x in self.calls))
        self.assertEqual(self.calls[-1], ('stop', 'backend'))

    def test_retry_backup_failure_preserves_existing_maintenance(self):
        self.updater.marker.touch()
        self.fail = lambda args: args[0] == 'exec'
        with self.assertRaises(RuntimeError):
            self.updater.activate(self.candidate, self.backup, False, [])
        self.assertTrue(self.updater.marker.exists())
        self.assertFalse(any(x[0] == 'start' for x in self.calls))

    def test_disabled_profile_is_never_started(self):
        self.updater.activate(self.candidate, self.backup, False, ['backend'])
        self.assertFalse(any('machine-gateway' in x or 'machine-manager' in x or 'machine-gateway-db-provision' in x for x in self.calls))


class UpdateFlowTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.directory = Path(self.tmp.name) / 'selfhosting'
        self.directory.mkdir()
        self.config = self.directory / 'production.json'
        self.raw = {'services': {'backend': {'image': 'old-api'}, 'frontend': {'image': 'old-web'}}}
        self.config.write_text(json.dumps(self.raw))
        (self.directory / '.env').write_text('SECRET=private\n')
        self.updater = m.ReleaseUpdater(self.directory, self.config, 'instance')
        (self.updater.runtime / 'releases' / 'abc').mkdir(parents=True)
        self.calls = []
        self.fail_pull = False
        self.activated = False
        def run(args, **kwargs):
            self.calls.append(args)
            if args[:2] == ['docker', 'pull']:
                if self.fail_pull:
                    raise RuntimeError('registry unavailable')
            if args[:3] == ['docker', 'image', 'inspect']:
                repository = args[-1].split(':')[0]
                return json.dumps([{'RepoDigests': [repository + '@sha256:new'], 'Id': repository}])
            if 'rev-parse' in args:
                return 'abc'
            return ''
        def compose(*args, **kwargs):
            self.calls.append(list(args))
            if args[0] == 'ps':
                return 'backend\nfrontend'
            return ''
        self.updater.run = run
        self.updater.compose = compose
        self.updater.model = lambda **kw: self.raw
        def activate(*args):
            self.activated = True
        self.updater.activate = activate
        self.response = {'tag_name': 'v1.2.3', 'draft': False, 'prerelease': False}
        self.api = patch.object(m.urllib.request, 'urlopen', side_effect=lambda *a, **kw: io.BytesIO(json.dumps(self.response).encode()))
        self.api.start()
        self.addCleanup(self.api.stop)

    def test_detached_checkout_uses_release_ref_without_pull_or_switch(self):
        self.updater.update()
        self.assertTrue(self.activated)
        git = [c for c in self.calls if c[0] == 'git']
        self.assertTrue(any('refs/tags/v1.2.3:refs/tags/v1.2.3' in c for c in git))
        self.assertFalse(any(x in c for c in git for x in ['pull', 'checkout', 'reset', 'switch']))
        state = json.loads(self.updater.state.read_text())
        self.assertEqual(state['release'], 'v1.2.3')
        backup = Path(state['backup'])
        self.assertEqual((backup / '.env').read_text(), 'SECRET=private\n')
        self.assertEqual((backup / '.env').stat().st_mode & 0o777, 0o600)

    def test_missing_release_image_does_not_stop_services(self):
        self.fail_pull = True
        with self.assertRaises(RuntimeError):
            self.updater.update()
        self.assertFalse(self.activated)
        self.assertFalse(self.updater.marker.exists())
        self.assertEqual(json.loads(self.config.read_text()), self.raw)

    def test_current_healthy_release_is_noop_but_image_drift_is_repaired(self):
        images = {name: repository + '@sha256:new' for name, repository in m.IMAGES.items()}
        self.updater.state.write_text(json.dumps({'release': 'v1.2.3', 'images': images}))
        for name, image in images.items():
            self.raw['services'][name]['image'] = image
        containers = [{'Image': repository, 'Config': {'Labels': {'com.docker.compose.service': name}}, 'State': {'Health': {'Status': 'healthy'}}} for name, repository in m.IMAGES.items()]
        original_run = self.updater.run
        def run(args, **kwargs):
            if args[:2] == ['docker', 'inspect']:
                return json.dumps(containers)
            return original_run(args, **kwargs)
        self.updater.run = run
        self.updater.update()
        self.assertFalse(self.activated)
        self.assertFalse(any(c[0] == 'git' for c in self.calls))
        containers[0]['Image'] = 'wrong-running-image'
        self.updater.update()
        self.assertTrue(self.activated)

    def dev_profile(self, missing=()):
        self.raw = {'services': {
            'backend': {'image': 'old-api', 'environment': {'DEV_MACHINES_ENABLED': 'true'}},
            'frontend': {'image': 'old-web'},
            'machine-manager': {'image': 'old-manager', 'environment': {}},
            'machine-gateway': {'image': 'old-gateway', 'environment': {}},
            'machine-gateway-db-provision': {'volumes': []},
        }}
        self.config.write_text(json.dumps(self.raw))
        reference = {'services': {
            'backend': {'environment': {}},
            'machine-manager': {'build': {'context': '../BE', 'dockerfile': 'Dockerfile.machine-manager'}, 'environment': {}},
            'machine-gateway': {'build': {'context': '../BE', 'dockerfile': 'Dockerfile.machine-gateway'}, 'environment': {}},
            'dev-machine-ide': {'profiles': ['dev-machine-images'], 'image': '${DEV_MACHINE_IDE_IMAGE:-default}', 'build': {'context': '../devmachine', 'dockerfile': 'ide/Dockerfile'}},
        }}
        self.updater.model = lambda config=None, raw=False: reference if config and str(config).endswith('docker-compose.yml') else self.raw
        original_run, original_compose = self.updater.run, self.updater.compose
        def run(args, **kwargs):
            if args[:2] == ['docker', 'pull'] and any(args[2].startswith(m.published_repository(name) + ':') for name in missing):
                self.calls.append(args)
                raise m.subprocess.CalledProcessError(1, args)
            return original_run(args, **kwargs)
        def compose(*args, **kwargs):
            if args[0] == 'ps':
                self.calls.append(list(args))
                return 'backend\nfrontend\nmachine-manager\nmachine-gateway'
            return original_compose(*args, **kwargs)
        self.updater.run, self.updater.compose = run, compose
        return ['machine-manager', 'machine-gateway', 'dev-machine-ide']

    def test_dev_profile_pulls_published_release_images_and_records_digests(self):
        services = self.dev_profile()
        out = io.StringIO()
        with patch('sys.stdout', out):
            self.updater.update()
        self.assertTrue(self.activated)
        for name in services:
            self.assertIn(['docker', 'pull', m.published_repository(name) + ':v1.2.3'], self.calls)
        self.assertFalse(any(c[0] == 'build' for c in self.calls))
        candidate = json.loads((self.updater.runtime / 'release-candidate.json').read_text())['services']
        digest = m.published_repository('dev-machine-ide') + '@sha256:new'
        self.assertEqual(candidate['dev-machine-ide']['image'], digest)
        self.assertEqual(candidate['machine-manager']['environment']['DEV_MACHINE_IDE_IMAGE'], digest)
        self.assertEqual(candidate['backend']['environment']['DEV_MACHINE_IDE_IMAGE'], digest)
        state = json.loads(self.updater.state.read_text())
        self.assertEqual(state['dev_machine_images'], {name: m.published_repository(name) + '@sha256:new' for name in services})
        self.assertEqual(state['dev_machine_builds'], [])
        self.assertNotIn('building', out.getvalue().lower())

    def test_missing_published_dev_image_falls_back_to_local_build(self):
        self.dev_profile(missing=['machine-gateway'])
        out = io.StringIO()
        with patch('sys.stdout', out):
            self.updater.update()
        self.assertTrue(self.activated)
        build = next(c for c in self.calls if c[0] == 'build')
        self.assertEqual(build, ['build', '--pull', 'machine-gateway'])
        candidate = json.loads((self.updater.runtime / 'release-candidate.json').read_text())['services']
        self.assertEqual(candidate['machine-gateway']['image'], 'local/instance-machine-gateway:v1.2.3')
        self.assertEqual(candidate['machine-manager']['image'], m.published_repository('machine-manager') + '@sha256:new')
        state = json.loads(self.updater.state.read_text())
        self.assertEqual(state['dev_machine_builds'], ['machine-gateway'])
        self.assertNotIn('machine-gateway', state['dev_machine_images'])
        self.assertIn('building machine-gateway locally', out.getvalue())

    def test_forced_local_build_skips_dev_image_pulls(self):
        services = self.dev_profile()
        self.updater.build_dev_images = True
        with patch('sys.stdout', io.StringIO()):
            self.updater.update()
        self.assertFalse(any(c[:2] == ['docker', 'pull'] and 'machine' in c[2] for c in self.calls))
        self.assertEqual(next(c for c in self.calls if c[0] == 'build'), ['build', '--pull', *services])

    def installed_dev_release(self, pulled, builds, missing=()):
        """Same release already installed and healthy with Dev Machines active."""
        services = self.dev_profile(missing=missing)
        images = {name: repository + '@sha256:new' for name, repository in m.IMAGES.items()}
        for name, image in images.items():
            self.raw['services'][name]['image'] = image
        for name, image in pulled.items():
            self.raw['services'].setdefault(name, {})['image'] = image
        self.updater.state.write_text(json.dumps({'release': 'v1.2.3', 'images': images,
                                                  'dev_machine_images': pulled, 'dev_machine_builds': builds}))
        names = ['backend', 'frontend', 'machine-manager', 'machine-gateway']
        containers = [{'Image': m.IMAGES.get(name, 'local'), 'Config': {'Labels': {'com.docker.compose.service': name}},
                       'State': {'Health': {'Status': 'healthy'}}} for name in names]
        dev_run = self.updater.run
        def run(args, **kwargs):
            if args[:2] == ['docker', 'inspect']:
                return json.dumps(containers)
            return dev_run(args, **kwargs)
        self.updater.run = run
        return services

    def test_same_release_with_pulled_dev_images_is_noop(self):
        services = self.dev_profile()
        pulled = {name: m.published_repository(name) + '@sha256:new' for name in services}
        self.installed_dev_release(pulled, [])
        with patch('sys.stdout', io.StringIO()):
            self.updater.update()
        self.assertFalse(self.activated)
        self.assertFalse(any(c[0] == 'build' for c in self.calls))

    def test_same_release_replaces_a_local_build_once_its_image_is_published(self):
        services = self.dev_profile()
        pulled = {name: m.published_repository(name) + '@sha256:new' for name in services if name != 'machine-gateway'}
        self.installed_dev_release(pulled, ['machine-gateway'])
        out = io.StringIO()
        with patch('sys.stdout', out):
            self.updater.update()
        self.assertTrue(self.activated)
        self.assertIn('now published', out.getvalue())
        state = json.loads(self.updater.state.read_text())
        self.assertEqual(state['dev_machine_builds'], [])

    def test_same_release_keeps_a_local_build_while_its_image_is_unpublished(self):
        services = self.dev_profile()
        pulled = {name: m.published_repository(name) + '@sha256:new' for name in services if name != 'machine-gateway'}
        self.installed_dev_release(pulled, ['machine-gateway'], missing=['machine-gateway'])
        with patch('sys.stdout', io.StringIO()):
            self.updater.update()
        self.assertFalse(self.activated)

    def test_same_release_reconciles_drifted_dev_images_and_build_mode(self):
        services = self.dev_profile()
        pulled = {name: m.published_repository(name) + '@sha256:new' for name in services}
        self.installed_dev_release(pulled, [])
        self.raw['services']['dev-machine-ide']['image'] = 'local/instance-dev-machine-ide:v1.2.3'
        with patch('sys.stdout', io.StringIO()):
            self.updater.update()
        self.assertTrue(self.activated, 'drifted references are repaired')
        self.activated = False
        self.installed_dev_release(pulled, [])
        self.updater.build_dev_images = True
        with patch('sys.stdout', io.StringIO()):
            self.updater.update()
        self.assertTrue(self.activated, 'switching to local builds rebuilds the same release')

    def test_state_without_dev_records_reconciles_once(self):
        self.installed_dev_release({}, [])
        state = json.loads(self.updater.state.read_text())
        del state['dev_machine_builds'], state['dev_machine_images']
        self.updater.state.write_text(json.dumps(state))
        with patch('sys.stdout', io.StringIO()):
            self.updater.update()
        self.assertTrue(self.activated)

    def test_prerelease_is_rejected_before_commands(self):
        self.response['prerelease'] = True
        with self.assertRaises(ValueError):
            self.updater.update()
        self.assertEqual(self.calls, [])

    def test_downgrade_is_rejected_before_commands(self):
        self.updater.state.write_text(json.dumps({'release': 'v1.2.4'}))
        with self.assertRaises(ValueError):
            self.updater.update()
        self.assertEqual(self.calls, [])


if __name__ == '__main__':
    unittest.main()
