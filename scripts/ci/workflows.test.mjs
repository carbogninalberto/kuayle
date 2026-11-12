import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import { planForPaths } from './impact.mjs';

// actionlint validates YAML/expressions separately. These contract checks inspect
// our intentionally simple job blocks, inline needs lists and planner selectors;
// fail visibly if those conventions change instead of silently dropping coverage.
const workflows = { tests: 'test', security: 'security', build: 'build', readiness: 'release-readiness' };
const plan = planForPaths(['.github/workflows/ci-impact.yml']);
for (const [scope, file] of Object.entries(workflows)) {
  test(`${scope} workflow and result gate cover the complete planner catalog`, () => {
    const text = readFileSync(new URL(`../../.github/workflows/${file}.yml`, import.meta.url), 'utf8');
    const jobsText = text.split('\njobs:\n')[1];
    assert.ok(jobsText, 'expected top-level jobs');
    const blocks = [...jobsText.matchAll(/^  ([\w-]+):\n([\s\S]*?)(?=^  [\w-]+:\n|$(?![\s\S]))/gm)];
    const jobs = Object.fromEntries(blocks.map((match) => [match[1], match[2]]));
    assert.deepEqual(Object.keys(jobs).filter((id) => !['changes', 'result'].includes(id)).sort(), [...plan.jobs[scope]].sort());
    const dependencies = jobs.result.match(/^    needs: \[([^\]]+)\]$/m)?.[1].split(',').map((id) => id.trim());
    assert.deepEqual(dependencies?.sort(), ['changes', ...plan.jobs[scope]].sort());
    assert.match(jobs.result, /^    if: always\(\)$/m);
    assert.match(jobs.result, /ref: \$\{\{ github.sha \}\}/);
    assert.match(jobs.result, new RegExp(`run: node scripts/ci/check-results.mjs ${scope}`));
    for (const [, block] of Object.entries(jobs)) {
      for (const match of block.matchAll(/fromJSON\(needs\.changes\.outputs\.plan\)\.([\w.]+)/g)) {
        let value = plan;
        for (const key of match[1].split('.')) {
          assert.ok(value != null && Object.hasOwn(value, key), `unknown planner selector ${match[1]}`);
          value = value[key];
        }
      }
    }
  });
}


test('release runtime checks consume refreshed source while PR checks survive skipped publishing', () => {
  const workflow = readFileSync(new URL('../../.github/workflows/build.yml', import.meta.url), 'utf8');
  const source = workflow.split('  release-source:\n')[1].split('  release-context:\n')[0];
  const context = workflow.split('  release-context:\n')[1].split('  build:\n')[0];
  const build = workflow.split('  build:\n')[1].split('  dev-machine-images:\n')[0];
  const runtime = workflow.split('  dev-machine-images:\n')[1].split('  result:\n')[0];
  assert.ok(source.indexOf('run: node scripts/ci/upgrade-agent-clis.mjs') < source.indexOf('- name: Commit version bump'));
  assert.match(source, /source_sha: \$\{\{ steps.source.outputs.sha \}\}/);
  assert.match(source, /git add devmachine\/common\/install-agent-clis.sh devmachine\/\{agent,ide,headless\}\/Dockerfile/);
  // Automatic releases and manual republishes share one validated source/tag context.
  assert.match(context, /gh api "repos\/\$GITHUB_REPOSITORY\/releases\/tags\/\$RELEASE_TAG"/);
  assert.match(context, /must identify a published, non-draft release/);
  assert.match(context, /does not identify an existing release/);
  assert.match(context, /releases\/latest/);
  assert.match(context, /git log HEAD --format='%H%x09%s'.*bump\(version\): \$RELEASE_TAG/);
  assert.match(context, /no 'bump\(version\): \$RELEASE_TAG' commit found on main/);
  assert.match(context, /does not match \$RELEASE_TAG/);
  assert.match(context, /ref: main\n          fetch-depth: 0/);
  assert.match(context, /publish_latest=false/);
  // Application images are built from that context, without repeating the bump.
  assert.match(build, /needs: \[changes, release-source, release-context\]/);
  assert.match(build, /ref: \$\{\{ needs.release-context.outputs.source_ref \}\}/);
  assert.doesNotMatch(build, /Commit version bump|git push/);
  assert.match(runtime, /needs: \[changes, release-source\]/);
  assert.match(runtime, /!cancelled\(\)/);
  assert.match(runtime, /github.event_name != 'release' \|\| needs.release-source.result == 'success'/);
  assert.match(runtime, /ref: \$\{\{ needs.release-source.outputs.source_sha \|\| github.event.pull_request.head.sha \|\| github.sha \}\}/);
});

test('releases publish the scanned runtime images the updater pulls; PR builds never push', () => {
  const workflow = readFileSync(new URL('../../.github/workflows/build.yml', import.meta.url), 'utf8');
  const action = readFileSync(new URL('../../.github/actions/dev-machine-image/action.yml', import.meta.url), 'utf8');
  const compose = readFileSync(new URL('../../selfhosting/docker-compose.yml', import.meta.url), 'utf8');
  const runtime = workflow.split('  dev-machine-images:\n')[1].split('  dev-machine-publish:\n')[0];
  const publish = workflow.split('  dev-machine-publish:\n')[1].split('  dev-machine-manifests:\n')[0];
  const manifests = workflow.split('  dev-machine-manifests:\n')[1].split('  result:\n')[0];
  for (const job of [runtime, publish]) assert.match(job, /uses: \.\/\.github\/actions\/dev-machine-image/);
  assert.doesNotMatch(runtime, /packages: write|docker push|login-action/);
  assert.match(action, /push: false\n\s+load: true/);
  // The pushed image is the one that passed the smoke tests and scan.
  assert.ok(action.indexOf('Scan native architecture image') > action.indexOf('Build native architecture image'));
  assert.match(publish, /needs: \[changes, release-source, release-context\]/);
  assert.match(publish, /needs.release-context.result == 'success'/);
  // The planner decides whether the native arm64 jobs run.
  assert.match(publish, /architecture: \$\{\{ fromJSON\(needs\.changes\.outputs\.plan\)\.architectures \}\}/);
  assert.doesNotMatch(publish, /architecture: \[amd64, arm64\]/);
  assert.doesNotMatch(publish, /needs.build|max-parallel/);
  assert.match(publish, /packages: write/);
  assert.ok(publish.indexOf('docker push') > publish.indexOf('uses: ./.github/actions/dev-machine-image'));
  assert.match(publish, /ref: \$\{\{ needs.release-context.outputs.source_ref \}\}/);
  // Release tags appear only once the application images are published too.
  assert.match(manifests, /needs: \[changes, release-context, build, dev-machine-publish\]/);
  assert.match(manifests, /needs.build.result == 'success' && needs.dev-machine-publish.result == 'success'/);
  assert.match(manifests, /imagetools create/);
  assert.match(manifests, /if \[ "\$INCLUDE_ARM64" = "true" \]/);
  assert.match(manifests, /sources\+=\("\$REPOSITORY:\$VERSION-arm64"\)/);
  assert.match(manifests, /PUBLISH_LATEST/);
  // Updater convention: Compose service NAME is published as kuayle-NAME.
  const services = [...compose.matchAll(/^  ([a-z0-9-]+):\n(?:    .*\n)*?    profiles: \["dev-machine-images"\]/gm)].map((match) => match[1]);
  assert.equal(services.length, 9);
  const packages = planForPaths([], 'release').devImages.map((image) => image.package);
  assert.deepEqual(packages.sort(), ['machine-gateway', 'machine-manager', ...services].map((name) => `kuayle-${name}`).sort());
});

test('automatic releases are amd64-only; manual dispatch opts into arm64', () => {
  const workflow = readFileSync(new URL('../../.github/workflows/build.yml', import.meta.url), 'utf8');
  const context = workflow.split('  release-context:\n')[1].split('  build:\n')[0];
  const build = workflow.split('  build:\n')[1].split('  dev-machine-images:\n')[0];
  assert.match(workflow, /workflow_dispatch:\n    inputs:\n      release_tag:/);
  assert.match(workflow, /include_arm64:[\s\S]*?type: boolean\n        default: false/);
  assert.match(workflow, /include_arm64: \$\{\{ inputs\.include_arm64 \|\| false \}\}/);
  // The planner is the single source of the architecture selection.
  assert.match(context, /include_arm64=\$\(printf '%s' "\$PLAN" \| jq -r 'any\(\.architectures\[\]; \. == "arm64"\)'\)/);
  assert.match(build, /include_arm64 == 'true'/);
  assert.match(build, /platforms: \$\{\{ needs\.release-context\.outputs\.include_arm64 == 'true' && 'linux\/amd64,linux\/arm64' \|\| 'linux\/amd64' \}\}/);
  // Application images stay on the single build-push-action push path so their
  // provenance attestations are preserved.
  assert.match(build, /uses: docker\/build-push-action@/);
  assert.match(build, /push: true/);
  assert.doesNotMatch(build, /imagetools create/);
  const tags = build.split('- name: Prepare application image tags\n')[1]?.split('- name:')[0];
  assert.ok(tags, 'expected application tag preparation');
  assert.match(tags, /PUBLISH_LATEST/);
  assert.match(tags, /:latest/);
  assert.match(build, /tags: \$\{\{ steps\.tags\.outputs\.all_in_one \}\}/);
  assert.match(build, /tags: \$\{\{ steps\.tags\.outputs\.api \}\}/);
  assert.match(build, /tags: \$\{\{ steps\.tags\.outputs\.web \}\}/);
});

test('QEMU is installed only for the opt-in arm64 application build', () => {
  const workflow = readFileSync(new URL('../../.github/workflows/build.yml', import.meta.url), 'utf8');
  const build = workflow.split('  build:\n')[1].split('  dev-machine-images:\n')[0];
  const qemu = build.split('- name: Set up QEMU for optional ARM64 application images\n')[1]?.split('- name:')[0];
  assert.ok(qemu, 'expected a QEMU setup step in Build Images');
  assert.match(qemu, /if: \$\{\{ needs\.release-context\.outputs\.include_arm64 == 'true' \}\}/);
  assert.match(qemu, /platforms: arm64/);
  assert.equal((workflow.match(/docker\/setup-qemu-action/g) ?? []).length, 1);
  assert.doesNotMatch(workflow.split('  dev-machine-images:\n')[1].split('  dev-machine-publish:\n')[0], /setup-qemu/);
  assert.doesNotMatch(workflow.split('  dev-machine-publish:\n')[1].split('  dev-machine-manifests:\n')[0], /setup-qemu/);
});

test('manual release readiness defaults to amd64 and include_arm64 adds both', () => {
  const workflow = readFileSync(new URL('../../.github/workflows/release-readiness.yml', import.meta.url), 'utf8');
  assert.match(workflow, /workflow_dispatch:\n    inputs:\n      include_arm64:/);
  assert.match(workflow, /include_arm64:[\s\S]*?type: boolean\n        default: false/);
  assert.match(workflow, /include_arm64: \$\{\{ inputs\.include_arm64 \|\| false \}\}/);
  assert.match(workflow, /architecture: \$\{\{ fromJSON\(needs\.changes\.outputs\.plan\)\.architectures \}\}/);
  assert.doesNotMatch(workflow, /setup-qemu/);
});

test('CI impact forwards the manual include_arm64 selection', () => {
  const workflow = readFileSync(new URL('../../.github/workflows/ci-impact.yml', import.meta.url), 'utf8');
  assert.match(workflow, /include_arm64:[\s\S]*?type: boolean\n        default: false/);
  assert.match(workflow, /INCLUDE_ARM64: \$\{\{ inputs\.include_arm64 \}\}/);
});

test('release API image is built with the release number', () => {
  // Release Readiness runs the built binary to verify the stamp itself.
  const workflow = readFileSync(new URL('../../.github/workflows/build.yml', import.meta.url), 'utf8');
  const api = workflow.split('- name: Build & push API\n')[1].split('- name: Build & push Web\n')[0];
  assert.match(api, /VERSION=\$\{\{ steps.version.outputs.number \}\}/);
  const build = workflow.split('  build:\n')[1].split('  dev-machine-images:\n')[0];
  assert.match(build, /echo "number=\$\{version#v\}" >> "\$GITHUB_OUTPUT"/);
});

test('server release jobs skip cli-v* releases, which CLI Release publishes', () => {
  const build = readFileSync(new URL('../../.github/workflows/build.yml', import.meta.url), 'utf8');
  const source = build.split('  release-source:\n')[1].split('\n    name:')[0];
  assert.match(source, /if: github.event_name == 'release' && !startsWith\(github.event.release.tag_name, 'cli-v'\)/);
  const impact = readFileSync(new URL('../../.github/workflows/ci-impact.yml', import.meta.url), 'utf8');
  assert.match(impact, /RELEASE_TAG: \$\{\{ github.event.release.tag_name \}\}/);
  const cli = readFileSync(new URL('../../.github/workflows/cli-release.yml', import.meta.url), 'utf8');
  assert.match(cli, /tags: \['cli-v\*'\]/);
  assert.match(cli, /--latest=false/);
  assert.match(cli, /sha256sum -c checksums.txt/);
  assert.ok(cli.indexOf('sha256sum -c checksums.txt') < cli.indexOf('gh release create'));
  // Only the publishing job may write; the release is cut from the verified tag.
  assert.equal(cli.match(/contents: write/g).length, 1);
  assert.match(cli, /--verify-tag/);
});
