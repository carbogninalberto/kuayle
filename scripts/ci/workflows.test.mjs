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
  const build = workflow.split('  build:\n')[1].split('  dev-machine-images:\n')[0];
  const runtime = workflow.split('  dev-machine-images:\n')[1].split('  result:\n')[0];
  assert.ok(build.indexOf('run: node scripts/ci/upgrade-agent-clis.mjs') < build.indexOf('- name: Commit version bump'));
  assert.match(build, /source_sha: \$\{\{ steps.source.outputs.sha \}\}/);
  assert.match(build, /git add devmachine\/common\/install-agent-clis.sh devmachine\/\{agent,ide,headless\}\/Dockerfile/);
  assert.match(runtime, /needs: \[changes, build\]/);
  assert.match(runtime, /!cancelled\(\)/);
  assert.match(runtime, /github.event_name != 'release' \|\| needs.build.result == 'success'/);
  assert.match(runtime, /ref: \$\{\{ needs.build.outputs.source_sha \|\| github.event.pull_request.head.sha \|\| github.sha \}\}/);
});
