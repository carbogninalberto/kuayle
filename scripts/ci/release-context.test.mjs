import assert from 'node:assert/strict';
import { execFileSync, spawnSync } from 'node:child_process';
import { chmodSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';

const script = new URL('./release-context.sh', import.meta.url).pathname;

function fixture(t) {
  const cwd = mkdtempSync(join(tmpdir(), 'kuayle-release-context-'));
  t.after(() => rmSync(cwd, { recursive: true, force: true }));
  const git = (...args) => execFileSync('git', args, { cwd, encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] }).trim();
  git('init', '-q', '-b', 'main');
  mkdirSync(join(cwd, 'UI'), { recursive: true });
  const commit = (version, subject, author, email) => {
    writeFileSync(join(cwd, 'UI/package.json'), `${JSON.stringify({ version })}\n`);
    git('add', 'UI/package.json');
    // Explicit identity env overrides any inherited GIT_AUTHOR_* values.
    execFileSync('git', ['commit', '-q', '-m', subject], {
      cwd,
      encoding: 'utf8',
      stdio: ['ignore', 'pipe', 'pipe'],
      env: {
        ...process.env,
        GIT_AUTHOR_NAME: author,
        GIT_AUTHOR_EMAIL: email,
        GIT_COMMITTER_NAME: author,
        GIT_COMMITTER_EMAIL: email
      }
    });
    return git('rev-parse', 'HEAD');
  };
  return { cwd, commit };
}

function mock(t, { latest = 'v0.2.32', release = '{"draft":false}', releaseFails = false } = {}) {
  const bin = mkdtempSync(join(tmpdir(), 'kuayle-release-gh-'));
  t.after(() => rmSync(bin, { recursive: true, force: true }));
  const path = join(bin, 'gh');
  writeFileSync(path, `#!/usr/bin/env bash
set -e
if [[ "$*" == *"releases/tags/"* ]]; then
  ${releaseFails ? 'exit 1' : `printf '%s\\n' '${release}'`}
  exit 0
fi
if [[ "$*" == *"releases/latest"* ]]; then
  printf '%s\\n' '${latest}'
  exit 0
fi
echo "unexpected gh: $*" >&2
exit 2
`);
  chmodSync(path, 0o755);
  return bin;
}

function run({ cwd, bin, event = 'release', tag = 'v0.2.32', include = 'false', sourceSha = 'abc123', ref = 'refs/heads/main' }) {
  const output = join(cwd, 'output');
  const result = spawnSync('bash', [script], {
    cwd,
    encoding: 'utf8',
    env: {
      ...process.env,
      PATH: `${bin}:${process.env.PATH}`,
      GITHUB_REPOSITORY: 'example/repo',
      GITHUB_OUTPUT: output,
      GITHUB_REF: ref,
      EVENT_NAME: event,
      RELEASE_TAG: tag,
      INCLUDE_ARM64: include,
      SOURCE_SHA: sourceSha
    }
  });
  let outputs = {};
  if (result.status === 0) {
    outputs = Object.fromEntries(readFileSync(output, 'utf8').trim().split('\n').map((line) => {
      const separator = line.indexOf('=');
      return [line.slice(0, separator), line.slice(separator + 1)];
    }));
  }
  return { ...result, outputs };
}

test('automatic release uses the refreshed source and selected architectures', (t) => {
  const repo = fixture(t);
  const result = run({ cwd: repo.cwd, bin: mock(t), event: 'release', include: 'false' });
  assert.equal(result.status, 0, result.stderr);
  assert.deepEqual(result.outputs, { tag: 'v0.2.32', source_ref: 'abc123', include_arm64: 'false' });
});

test('manual rebuild reuses the bot-authored release bump commit', (t) => {
  const repo = fixture(t);
  const bump = repo.commit('0.2.32', 'bump(version): v0.2.32', 'github-actions[bot]', '41898282+github-actions[bot]@users.noreply.github.com');
  const result = run({ cwd: repo.cwd, bin: mock(t), event: 'workflow_dispatch', include: 'true' });
  assert.equal(result.status, 0, result.stderr);
  assert.equal(result.outputs.source_ref, bump);
  assert.equal(result.outputs.include_arm64, 'true');
});

test('manual rebuild rejects a non-main ref', (t) => {
  const repo = fixture(t);
  const result = run({ cwd: repo.cwd, bin: mock(t), event: 'workflow_dispatch', ref: 'refs/heads/feature' });
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /manual releases must run from refs\/heads\/main/);
});

test('manual rebuild rejects draft, missing and invalid releases', (t) => {
  const repo = fixture(t);
  for (const [name, options, pattern] of [
    ['draft', { release: '{"draft":true}' }, /published, non-draft release/],
    ['missing', { releaseFails: true }, /does not identify an existing release/],
    ['metadata tag', { tag: 'v0.2.32+meta' }, /server release tag/],
    ['dot suffix', { tag: 'v0.2.32.1' }, /server release tag/],
    ['cli tag', { tag: 'cli-v0.2.32' }, /server release tag/]
  ]) {
    const result = run({ cwd: repo.cwd, bin: mock(t, options), event: 'workflow_dispatch', ...options });
    assert.notEqual(result.status, 0, name);
    assert.match(result.stderr, pattern, name);
  }
});

test('manual rebuild rejects missing, wrong-author and version-mismatched bump commits', (t) => {
  const repo = fixture(t);
  repo.commit('0.2.33', 'bump(version): v0.2.33', 'Test User', 'test@example.com');
  repo.commit('0.2.31', 'bump(version): v0.2.34', 'github-actions[bot]', '41898282+github-actions[bot]@users.noreply.github.com');

  for (const [tag, pattern] of [
    ['v0.2.99', /no 'bump\(version\): v0\.2\.99' commit by github-actions\[bot\]/],
    ['v0.2.33', /no 'bump\(version\): v0\.2\.33' commit by github-actions\[bot\]/],
    ['v0.2.34', /does not match v0\.2\.34/]
  ]) {
    const result = run({ cwd: repo.cwd, bin: mock(t), event: 'workflow_dispatch', tag });
    assert.notEqual(result.status, 0, tag);
    assert.match(result.stderr, pattern, tag);
  }
});

test('automatic releases require the refreshed source SHA', (t) => {
  const repo = fixture(t);
  for (const [name, value] of [['missing', ''], ['invalid include_arm64', 'maybe']]) {
    const result = run({ cwd: repo.cwd, bin: mock(t), event: 'release', sourceSha: value, include: name === 'invalid include_arm64' ? value : 'false' });
    assert.notEqual(result.status, 0, name);
    assert.match(result.stderr, /SOURCE_SHA is required|INCLUDE_ARM64 must be true or false/, name);
  }
});
