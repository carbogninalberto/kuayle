#!/usr/bin/env python3
"""Update an explicitly configured Compose instance from published stable releases.

Invoked by release-update.sh. Uses Python, PyYAML and Compose v2.
The checkout may be detached; source is exported to runtime/releases, never pulled
into the operator's checkout. See selfhosting/RELEASE_UPDATER.md.
"""
import copy
import datetime
import fcntl
import json
import os
import re
import shutil
import signal
import subprocess
import sys
import tempfile
import urllib.request
from pathlib import Path

REPOSITORY = 'carbogninalberto/kuayle'
IMAGES = {'backend': 'ghcr.io/carbogninalberto/kuayle-api',
          'frontend': 'ghcr.io/carbogninalberto/kuayle-web'}


def log(message):
    print(message, flush=True)


def atomic_json(path, data):
    path = Path(path)
    with tempfile.NamedTemporaryFile(mode='w', dir=path.parent, delete=False) as f:
        json.dump(data, f, indent=2)
        f.write('\n')
        temp = Path(f.name)
    temp.replace(path)


def build_from_source(service, source):
    build = service['build']
    if isinstance(build, str):
        build = {'context': build}
    build = copy.deepcopy(build)
    build['context'] = str((source / 'selfhosting' / build['context']).resolve())
    return build


def make_candidate(raw, reference, source, project, tag, images, dev_active):
    candidate = copy.deepcopy(raw)
    services = candidate['services']
    for name, image in images.items():
        services[name]['image'] = image
    builds = []
    if not dev_active:
        return candidate, builds
    for name in ('backend', 'machine-manager', 'machine-gateway'):
        current = services[name].setdefault('environment', {})
        for key, value in reference['services'][name].get('environment', {}).items():
            current.setdefault(key, value)
    for name in ('machine-manager', 'machine-gateway'):
        services[name]['build'] = build_from_source(reference['services'][name], source)
        services[name]['image'] = f'local/{project}-{name}:{tag}'
        builds.append(name)
    for name, spec in reference['services'].items():
        if 'dev-machine-images' not in spec.get('profiles', []):
            continue
        runtime = copy.deepcopy(spec)
        runtime['build'] = build_from_source(runtime, source)
        image = f'local/{project}-{name}:{tag}'
        match = re.fullmatch(r'\$\{(DEV_MACHINE_[A-Z_]+_IMAGE)(?::-[^}]+)?\}', runtime['image'])
        if not match:
            raise ValueError(f'Cannot identify runtime image setting for {name}')
        runtime['image'] = image
        services[name] = runtime
        for consumer in ('backend', 'machine-manager'):
            services[consumer]['environment'][match[1]] = image
        builds.append(name)
    role = services['machine-gateway-db-provision']
    volumes = role.get('volumes', [])
    for i, volume in enumerate(volumes):
        if isinstance(volume, str) and ':/etc/kuayle/gateway-role.sql' in volume:
            volumes[i] = str(source / 'selfhosting/gateway-role.sql') + ':' + volume.split(':', 1)[1]
        elif isinstance(volume, dict) and volume.get('target') == '/etc/kuayle/gateway-role.sql':
            volume['source'] = str(source / 'selfhosting/gateway-role.sql')
    return candidate, builds


class ReleaseUpdater:
    def __init__(self, directory, config, project):
        self.directory = Path(directory).resolve()
        self.config = Path(config).resolve()
        self.project = project
        self.runtime = self.directory / 'runtime'
        self.runtime.mkdir(exist_ok=True)
        self.marker = self.runtime / 'upgrading'
        self.state = self.runtime / 'release-updater-state.json'
        self.env = dict(os.environ, PWD=str(self.directory), COMPOSE_PARALLEL_LIMIT='1')
        # Never inherit a caller's alternate Compose selection.
        self.env.pop('COMPOSE_FILE', None)
        self.env.pop('COMPOSE_PROFILES', None)

    def run(self, args, *, capture=False, stdout=None):
        result = subprocess.run(args, cwd=self.directory, env=self.env, check=True,
                                stdout=subprocess.PIPE if capture else stdout,
                                text=capture)
        return result.stdout if capture else ''

    def compose(self, *args, config=None, **kwargs):
        return self.run(['docker', 'compose', '--project-directory', str(self.directory),
                         '--env-file', str(self.directory / '.env'), '-p', self.project,
                         '-f', str(config or self.config), '--profile', 'updater',
                         '--profile', 'dev-machines', '--profile', 'dev-machine-images',
                         *args], **kwargs)

    def model(self, config=None, raw=False):
        if raw:
            import yaml
            return yaml.safe_load(Path(config or self.config).read_text())
        return json.loads(self.compose('config', '--format', 'json',
                                       config=config, capture=True))

    def activate(self, candidate, backup, dev_active, running):
        writers = ['backend'] + (['machine-manager', 'machine-gateway'] if dev_active else [])
        restore = [s for s in writers if s in running]
        migration_attempted = False
        stopped = False
        recovering = self.marker.exists()
        self.marker.touch()
        try:
            stopped = True  # also recover a partially failed stop
            self.compose('stop', *writers)
            log('Backing up PostgreSQL before migrations...')
            with (backup / 'database.dump').open('wb') as dump:
                self.compose('exec', '-T', 'postgres', 'sh', '-c',
                             'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc', stdout=dump)
            if not (backup / 'database.dump').stat().st_size:
                raise RuntimeError('Database backup is empty')
            atomic_json(self.config, json.loads(Path(candidate).read_text()))
            migration_attempted = True
            self.compose('run', '--rm', '--no-deps', '-T', '--pull', 'never',
                         'backend', '/app/server', 'migrate', 'up')
            if dev_active:
                self.compose('run', '--rm', '--no-deps', '-T', '--pull', 'never', 'machine-gateway-db-provision')
            self.compose('up', '-d', '--no-deps', '--no-build', '--pull', 'never',
                         '--wait', '--wait-timeout', '180', 'backend', 'frontend')
            if dev_active:
                self.compose('up', '-d', '--no-deps', '--no-build', '--pull', 'never',
                             '--wait', '--wait-timeout', '180', 'machine-gateway', 'machine-manager')
        except BaseException:
            if migration_attempted or recovering:
                # An unsuccessful health wait can leave new writers running.
                try:
                    self.compose('stop', *writers)
                except Exception as exc:
                    log(f'Could not stop application writers: {exc}')
                log(f'Update failed after migrations began. Maintenance remains enabled; '
                    f'do not restart previous images. Backup: {backup}')
            elif stopped:
                if restore:
                    self.compose('start', *restore)
                self.marker.unlink(missing_ok=True)
            raise
        self.marker.unlink(missing_ok=True)

    def update(self):
        repo = self.directory.parent
        request = urllib.request.Request(f'https://api.github.com/repos/{REPOSITORY}/releases/latest',
                                         headers={'User-Agent': 'kuayle-release-updater', 'Accept': 'application/vnd.github+json'})
        with urllib.request.urlopen(request, timeout=30) as response:
            release = json.load(response)
        tag = release['tag_name']
        if release.get('draft') or release.get('prerelease') or not re.fullmatch(r'v\d+\.\d+\.\d+', tag):
            raise ValueError('Expected a published stable release')
        log(f'Preparing published release {tag} for {self.project}...')
        old_state = json.loads(self.state.read_text()) if self.state.exists() else {}
        previous = old_state.get('release', '')
        if re.fullmatch(r'v\d+\.\d+\.\d+', previous) and tuple(map(int, previous[1:].split('.'))) > tuple(map(int, tag[1:].split('.'))):
            raise ValueError(f'Refusing release downgrade from {previous} to {tag}')
        raw = self.model(raw=True)
        resolved = self.model()
        running = self.compose('ps', '--status', 'running', '--services', capture=True).split()
        dev_active = any(s in running for s in ('machine-manager', 'machine-gateway'))
        if self.marker.exists():
            # After failed migration these services are intentionally stopped.
            dev_active = resolved['services']['backend'].get('environment', {}).get('DEV_MACHINES_ENABLED') == 'true'
        images = {}
        image_ids = {}
        for service, repository in IMAGES.items():
            image = f'{repository}:{tag}'
            self.run(['docker', 'pull', image])
            info = json.loads(self.run(['docker', 'image', 'inspect', image], capture=True))[0]
            images[service] = next(d for d in info['RepoDigests'] if d.startswith(repository + '@sha256:'))
            image_ids[service] = info['Id']
        if old_state.get('release') == tag and old_state.get('images') == images and not self.marker.exists():
            ids = self.compose('ps', '-q', 'backend', 'frontend', *(['machine-manager', 'machine-gateway'] if dev_active else []), capture=True).split()
            info = json.loads(self.run(['docker', 'inspect', *ids], capture=True)) if ids else []
            expected = 4 if dev_active else 2
            image_match = all(c['Image'] == image_ids.get(c['Config']['Labels']['com.docker.compose.service'], c['Image']) for c in info)
            config_match = all(resolved['services'][name]['image'] == image for name, image in images.items())
            if config_match and image_match and len(info) == expected and all(c['State'].get('Health', {}).get('Status') == 'healthy' for c in info):
                log(f'{tag} is already installed and healthy; no restart required.')
                return
        # Fetch only the published release ref. Detached operator checkouts stay untouched.
        self.run(['git', '-C', str(repo), 'fetch', 'origin', f'refs/tags/{tag}:refs/tags/{tag}'])
        commit = self.run(['git', '-C', str(repo), 'rev-parse', f'{tag}^{{commit}}'], capture=True).strip()
        source = self.runtime / 'releases' / commit
        if not source.exists():
            source.parent.mkdir(exist_ok=True)
            staging = Path(tempfile.mkdtemp(prefix='export-', dir=source.parent))
            archive = staging / 'source.tar'
            try:
                with archive.open('wb') as f:
                    self.run(['git', '-C', str(repo), 'archive', commit], stdout=f)
                self.run(['tar', '-xf', str(archive), '-C', str(staging)])
                archive.unlink()
                staging.rename(source)
            except BaseException:
                shutil.rmtree(staging)
                raise
        reference = self.model(config=source / 'selfhosting/docker-compose.yml', raw=True)
        candidate, builds = make_candidate(raw, reference, source, self.project, tag, images, dev_active)
        candidate_path = self.runtime / 'release-candidate.json'
        atomic_json(candidate_path, candidate)
        self.model(config=candidate_path)  # reject invalid Compose before disruption
        if builds:
            log('Building release control-plane and runtime images before stopping services...')
            self.compose('build', '--pull', *builds, config=candidate_path)
        backup = self.directory / 'backups' / ('release-' + datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%S%fZ'))
        backup.mkdir(parents=True, mode=0o700)
        for path in (self.config, self.directory / '.env', self.state):
            if path.exists():
                shutil.copy2(path, backup / path.name)
                (backup / path.name).chmod(0o600)
        log(f'Prepared {tag}. Configuration and database backup: {backup}')
        self.activate(candidate_path, backup, dev_active, running)
        atomic_json(self.state, {'release': tag, 'source': commit, 'images': images,
                                'backup': str(backup), 'updated_at': datetime.datetime.now(datetime.timezone.utc).isoformat()})
        log(f'Release {tag} completed successfully; all updated services are healthy.')


def main():
    os.umask(0o077)
    def interrupted(signum, _frame):
        raise SystemExit(128 + signum)
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)
    directory = Path(os.environ['REPO_ROOT']) / 'selfhosting'
    config = Path(os.environ['RELEASE_COMPOSE_FILE'])
    project = os.environ['RELEASE_COMPOSE_PROJECT']
    if not config.is_absolute() or not re.fullmatch(r'[a-z0-9][a-z0-9_-]*', project):
        raise ValueError('An absolute RELEASE_COMPOSE_FILE and explicit Compose project are required')
    updater = ReleaseUpdater(directory, config, project)
    with (updater.runtime / 'release-update.lock').open('w') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        updater.update()


if __name__ == '__main__':
    try:
        main()
    except Exception as exc:
        log(f'Release update failed: {exc}')
        sys.exit(1)
