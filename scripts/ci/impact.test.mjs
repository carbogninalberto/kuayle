import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { mkdtempSync, writeFileSync, readFileSync, mkdirSync, rmSync, renameSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';
import { planForPaths, detectPlan, changedPaths } from './impact.mjs';
import { checkResults } from './check-results.mjs';

const ids = (items) => items.map((item) => item.id);
const selected = (plan) => Object.fromEntries(Object.entries(plan.required).map(([key, jobs]) => [key, jobs]));

test('docs and agent harness changes select only the inexpensive harness check', () => {
  for (const paths of [['README.md', 'TECHNICAL.md'], ['BE/docs/github-integration.md'], ['AGENTS.md', 'AGENTS/TOOLS/check-harness.mjs'], ['WEB/README.md']]) {
    const plan = planForPaths(paths);
    assert.deepEqual(selected(plan), { tests: ['agent-harness'], security: [], build: [], readiness: [] });
    assert.deepEqual(plan.releaseImages, []);
    assert.deepEqual(plan.devImages, []);
  }
});

test('UI runtime changes select browser, audit and both dependent application images', () => {
  for (const path of ['UI/src/routes/+page.svelte', 'UI/messages/it.json', 'UI/package-lock.json', 'UI/static/logo.svg']) {
    const plan = planForPaths([path]);
    assert.equal(plan.frontend, true);
    assert.equal(plan.backend, false);
    assert.equal(plan.web, false);
    assert.deepEqual(ids(plan.releaseImages), ['all-in-one', 'web']);
    assert.deepEqual(plan.required.security, ['frontend-security']);
    assert.deepEqual(plan.devImages, []);
  }
});

test('backend runtime and dependency changes include API, all-in-one and Go runtime images', () => {
  for (const path of ['BE/internal/service/issue.go', 'BE/migrations/000001_schema.up.sql', 'BE/go.sum']) {
    const plan = planForPaths([path]);
    assert.equal(plan.backend, true);
    assert.equal(plan.runtime, true);
    assert.equal(plan.frontend, false);
    assert.deepEqual(ids(plan.releaseImages), ['all-in-one', 'api']);
    assert.deepEqual(ids(plan.devImages), ['gateway', 'manager']);
    assert.deepEqual(ids(plan.securityImages), ['updater']);
  }
});

test('test-only changes avoid production images but retain the affected suites', () => {
  const ui = planForPaths(['UI/tests/e2e/issues.test.ts']);
  assert.equal(ui.frontend, true);
  assert.deepEqual(ui.releaseImages, []);
  const be = planForPaths(['BE/internal/service/issue_test.go']);
  assert.equal(be.backend, true);
  assert.equal(be.runtime, false);
  assert.deepEqual(be.releaseImages, []);
  assert.deepEqual(be.devImages, []);
  assert.equal(planForPaths(['BE/internal/machine/docker_integration_test.go']).runtime, true);
});

test('WEB code, embedded Markdown, assets and dependencies do not build application images', () => {
  for (const path of ['WEB/src/lib/data/features.ts', 'WEB/src/content/page.md', 'WEB/static/social-card.png', 'WEB/package-lock.json']) {
    const plan = planForPaths([path]);
    assert.equal(plan.web, true);
    assert.equal(plan.frontend, false);
    assert.equal(plan.backend, false);
    assert.deepEqual(plan.releaseImages, []);
    assert.deepEqual(plan.devImages, []);
  }
});

test('Dockerfiles and shared dev-machine inputs fan out only to their consumers', () => {
  assert.deepEqual(ids(planForPaths(['Dockerfile']).releaseImages), ['all-in-one']);
  assert.deepEqual(ids(planForPaths(['UI/Dockerfile']).releaseImages), ['web']);
  assert.deepEqual(ids(planForPaths(['BE/Dockerfile.machine-gateway']).devImages), ['gateway']);
  assert.deepEqual(ids(planForPaths(['devmachine/collector/main.go']).devImages), ['collector']);
  assert.deepEqual(ids(planForPaths(['devmachine/common/patch-npm-security.cjs']).devImages), ['ide', 'agent-claude', 'agent-opencode', 'agent-codex']);
  assert.deepEqual(ids(planForPaths(['devmachine/common/kuayle-service-entrypoint']).devImages), ['ide', 'browser', 'collector', 'egress']);
  assert.equal(planForPaths(['devmachine/ide/checkout.sh']).backend, true);
  assert.equal(planForPaths(['devmachine/ide/checkout.sh', 'BE/Dockerfile.updater']).runtime, true);
  assert.equal(planForPaths(['devmachine/security/apparmor.profile']).runtime, true);
});

test('self-hosting scripts and smoke tests retain their checks and packaged runtime coverage', () => {
  for (const path of ['selfhosting/update.sh', 'selfhosting/docker-compose.yml', 'selfhosting/image_smoke_test.py']) {
    const plan = planForPaths([path]);
    assert.equal(plan.selfhosting, true);
    assert.deepEqual(ids(plan.releaseImages), ['all-in-one', 'api', 'web']);
  }
});

test('workflow, routing, unknown and shared root changes conservatively exercise all areas', () => {
  for (const path of ['.github/workflows/test.yml', 'scripts/ci/impact.mjs', 'docker-compose.yml', '.dockerignore', 'new-shared-input.config']) {
    const plan = planForPaths([path]);
    assert.equal(plan.ci && plan.backend && plan.frontend && plan.web && plan.runtime && plan.selfhosting, true);
    assert.equal(plan.releaseImages.length, 3);
    assert.equal(plan.devImages.length, 9);
  }
});

test('mixed changes union their coverage; non-PR events retain the full original jobs', () => {
  const mixed = planForPaths(['UI/src/app.css', 'WEB/static/logo.svg', 'devmachine/collector/main.go']);
  assert.equal(mixed.frontend && mixed.web && mixed.backend && mixed.runtime, true);
  assert.deepEqual(ids(mixed.releaseImages), ['all-in-one', 'web']);
  assert.deepEqual(ids(mixed.devImages), ['collector']);
  for (const event of ['push', 'release', 'schedule', 'workflow_dispatch']) {
    const plan = planForPaths([], event);
    assert.equal(plan.devImages.length, 9);
    assert.equal(plan.securityImages.length, 10);
    assert.deepEqual(plan.required.build, ['build', 'dev-machine-images']);
    assert.ok(plan.required.security.includes('docker-security'));
  }
});

function repository(t) {
  const cwd = mkdtempSync(join(tmpdir(), 'kuayle-ci-diff-'));
  t.after(() => rmSync(cwd, { recursive: true, force: true }));
  const git = (...args) => execFileSync('git', args, { cwd, encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] }).trim();
  const file = (path, text) => { mkdirSync(join(cwd, path, '..'), { recursive: true }); writeFileSync(join(cwd, path), text); };
  // Build disposable history with plumbing and the checkout's existing identity
  // metadata. CI needs no git persona configuration and no identity is overridden.
  const metadata = execFileSync('git', ['cat-file', 'commit', 'HEAD'], { encoding: 'utf8' })
    .split('\n').filter((line) => /^(author|committer) /.test(line)).join('\n');
  const commit = (...paths) => {
    git('add', '--', ...paths);
    const tree = git('write-tree');
    let parent = '';
    try { parent = `parent ${git('rev-parse', '--verify', 'HEAD')}\n`; } catch { /* first fixture */ }
    const sha = execFileSync('git', ['hash-object', '-t', 'commit', '-w', '--stdin'], {
      cwd, encoding: 'utf8', input: `tree ${tree}\n${parent}${metadata}\n\ntest(ci): add diff fixture\n`
    }).trim();
    git('update-ref', 'HEAD', sha);
    return sha;
  };
  git('init', '--initial-branch=main');
  file('README.md', 'base\n');
  file('BE/old.go', 'package old\n');
  const base = commit('README.md', 'BE/old.go');
  git('checkout', '-b', 'task');
  return { cwd, git, file, commit, base };
}

test('full merge-base diff includes earlier commits, deletions and both renamed paths', (t) => {
  const repo = repository(t);
  repo.file('UI/src/earlier.ts', 'export const earlier = true;\n');
  repo.commit('UI/src/earlier.ts');
  mkdirSync(join(repo.cwd, 'WEB/src'), { recursive: true });
  renameSync(join(repo.cwd, 'BE/old.go'), join(repo.cwd, 'WEB/src/renamed.go'));
  const head = repo.commit('BE/old.go', 'WEB/src/renamed.go');
  repo.git('checkout', 'main');
  repo.file('main-only.config', 'base advance\n');
  const advancedBase = repo.commit('main-only.config');
  const paths = changedPaths(repo.cwd, advancedBase, head);
  assert.deepEqual(paths.sort(), ['BE/old.go', 'UI/src/earlier.ts', 'WEB/src/renamed.go']);
  const plan = detectPlan({ cwd: repo.cwd, event: 'pull_request', base: advancedBase, head });
  assert.equal(plan.backend && plan.frontend && plan.web, true);
});

test('diff handles more than 300 files and whitespace without truncation', (t) => {
  const repo = repository(t);
  const paths = Array.from({ length: 305 }, (_, i) => `docs/${i}.md`);
  paths.push('UI/src/name with\nnewline.ts');
  for (const path of paths) repo.file(path, 'content\n');
  const head = repo.commit(...paths);
  assert.equal(changedPaths(repo.cwd, repo.base, head).length, 306);
  assert.equal(detectPlan({ cwd: repo.cwd, event: 'pull_request', base: repo.base, head }).frontend, true);
});

test('missing or invalid comparison data falls back to broad verification', () => {
  const plan = detectPlan({ cwd: tmpdir(), event: 'pull_request', base: '--bad', head: '' });
  assert.ok(plan.reason.includes('fallback'));
  assert.equal(detectPlan({}).devImages.length, 9);
  assert.equal(plan.backend && plan.frontend && plan.web && plan.runtime, true);
  assert.equal(plan.devImages.length, 9);
});

test('gates allow intentional omissions but reject failed detection or selected skipped/cancelled jobs', () => {
  const plan = planForPaths(['UI/src/app.css']);
  const needs = { changes: { result: 'success', outputs: { plan: JSON.stringify(plan) } } };
  for (const job of ['agent-harness', 'backend-test', 'frontend-check', 'web-check', 'docker-integration', 'selfhosting-check', 'ci-validation']) {
    needs[job] = { result: plan.required.tests.includes(job) ? 'success' : 'skipped' };
  }
  assert.deepEqual(checkResults('tests', needs), []);
  for (const result of ['failure', 'cancelled', 'skipped']) {
    assert.ok(checkResults('tests', { ...needs, 'frontend-check': { result } }).length);
  }
  assert.ok(checkResults('tests', { ...needs, changes: { result: 'failure', outputs: {} } }).length);
  assert.ok(checkResults('tests', { ...needs, changes: { result: 'success', outputs: {} } }).length);
  assert.ok(checkResults('tests', { ...needs, 'backend-test': { result: 'failure' } }).length);
});

test('backend token scope contracts also exercise the UI consumer', () => {
  for (const path of ['BE/internal/domain/personal_access_token.go', 'BE/internal/domain/permission.go']) {
    assert.equal(planForPaths([path]).frontend, true);
  }
});

test('gates reject unregistered jobs and incomplete dependency declarations', () => {
  const plan = planForPaths(['README.md']);
  const needs = { changes: { result: 'success', outputs: { plan: JSON.stringify(plan) } }, 'agent-harness': { result: 'success' } };
  assert.ok(checkResults('tests', { ...needs, 'unregistered-job': { result: 'skipped' } }).length);
  assert.ok(checkResults('tests', needs).length);
});

test('unreachable SHAs and absent comparison data retain full PR coverage', () => {
  for (const args of [{ base: 'a'.repeat(40), head: 'b'.repeat(40) }, {}]) {
    const plan = detectPlan({ cwd: tmpdir(), event: 'pull_request', ...args });
    assert.ok(plan.reason.startsWith('fallback'));
    assert.equal(plan.ci && plan.backend && plan.frontend && plan.runtime, true);
  }
});


test('CLI writes one parseable plan output and an inspectable summary', (t) => {
  const repo = repository(t);
  const output = join(repo.cwd, 'output');
  const summary = join(repo.cwd, 'summary');
  execFileSync(process.execPath, [join(import.meta.dirname, 'impact.mjs')], {
    cwd: repo.cwd,
    env: { ...process.env, GITHUB_EVENT_NAME: 'pull_request', PR_BASE_SHA: repo.base, PR_HEAD_SHA: repo.base, GITHUB_OUTPUT: output, GITHUB_STEP_SUMMARY: summary }
  });
  const lines = readFileSync(output, 'utf8').trim().split('\n');
  assert.equal(lines.length, 1);
  assert.deepEqual(JSON.parse(lines[0].slice('plan='.length)).required.tests, ['agent-harness']);
  assert.ok(readFileSync(summary, 'utf8').includes('agent-harness'));
});

test('manifest and Go control-plane Dockerfiles do not select unrelated consumers', () => {
  assert.equal(planForPaths(['UI/static/releases.json']).web, false);
  for (const path of ['BE/Dockerfile.machine-gateway', 'BE/Dockerfile.machine-manager']) {
    const plan = planForPaths([path]);
    assert.equal(plan.runtime, false);
    assert.equal(plan.devImages.length, 1);
  }
});
