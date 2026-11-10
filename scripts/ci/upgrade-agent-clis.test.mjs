import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, copyFileSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, dirname } from 'node:path';
import test from 'node:test';
import { upgradeAgentClis } from './upgrade-agent-clis.mjs';

const files = ['devmachine/common/install-agent-clis.sh', 'devmachine/agent/Dockerfile', 'devmachine/ide/Dockerfile', 'devmachine/headless/Dockerfile'];
const names = ['CLAUDE_CODE', 'OPENCODE', 'CODEX', 'QWEN', 'CRUSH', 'GOOSE', 'KIMI'];
const now = Date.parse('2026-11-10T12:00:00Z');
const hoursAgo = hours => new Date(now - hours * 3600000).toISOString();
function fixture(t) {
  const root = mkdtempSync(join(tmpdir(), 'agent-upgrade-'));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  for (const file of files) {
    mkdirSync(dirname(join(root, file)), { recursive: true });
    copyFileSync(new URL(`../../${file}`, import.meta.url), join(root, file));
  }
  return root;
}
const read = root => files.map(file => readFileSync(join(root, file), 'utf8'));
// published: [version, hours since publication, extra release/version fields], oldest first.
function npmDocument(published, latest = published.at(-1)[0]) {
  return {
    'dist-tags': { latest },
    versions: Object.fromEntries(published.map(([version, , extra]) => [version, { version, ...extra }])),
    time: Object.fromEntries(published.map(([version, hours]) => [version, hoursAgo(hours)])),
  };
}
function githubReleases(url, published) {
  const kimi = url.includes('kimi-cli');
  return published.toReversed().map(([version, hours, extra]) => ({
    tag_name: kimi ? version : `v${version}`, draft: false, prerelease: false, published_at: hoursAgo(hours),
    assets: ['x86_64', 'aarch64'].map((arch, i) => ({
      name: kimi ? `kimi-${version}-${arch}-unknown-linux-gnu.tar.gz` : `goose-${arch}-unknown-linux-gnu.tar.gz`,
      digest: `sha256:${`${version.at(-1)}${i + 1}`.repeat(32)}`,
    })),
    ...extra,
  }));
}
function metadata(url, published = [['9.8.7', 72]]) {
  return url.startsWith('https://registry.npmjs.org/') ? npmDocument(published) : githubReleases(url, published);
}
const respond = build => async (url) => ({ ok: true, json: async () => build(url) });
const checksums = (installer, name) => [...installer.matchAll(new RegExp(`${name}_sha256=(\\w+)`, 'g'))].map(m => m[1]);
function assertPins(root, expected) {
  const [installer, agent, ide, headless] = read(root);
  for (const name of ['CLAUDE_CODE', 'OPENCODE', 'CODEX', 'QWEN', 'GOOSE', 'KIMI']) {
    assert.match(installer, new RegExp(`^${name}_VERSION=${expected}$`, 'm'));
  }
  for (const name of ['CLAUDE_CODE', 'OPENCODE', 'CODEX']) assert.match(agent, new RegExp(`^ARG ${name}_VERSION=${expected}$`, 'm'));
  for (const text of [ide, headless]) assert.match(text, new RegExp(`^ARG CRUSH_VERSION=${expected}$`, 'm'));
  return installer;
}

test('updates all seven CLIs, synchronized Dockerfile pins and both archive checksums', async (t) => {
  const root = fixture(t);
  const versions = await upgradeAgentClis(root, respond(metadata), { now });
  assert.deepEqual(versions, Object.fromEntries(names.map(name => [name, '9.8.7'])));
  const installer = assertPins(root, '9.8.7');
  for (const name of ['goose', 'kimi']) assert.deepEqual(checksums(installer, name), ['71'.repeat(32), '72'.repeat(32)]);
  const first = read(root);
  await upgradeAgentClis(root, respond(metadata), { now });
  assert.deepEqual(read(root), first);
});

test('skips a version published an hour ago for the newest one older than the gate', async (t) => {
  const root = fixture(t);
  const lines = [];
  const published = [['9.8.5', 500], ['9.8.6', 49], ['9.8.7', 1]];
  await upgradeAgentClis(root, respond(url => metadata(url, published)), { now, log: line => lines.push(line) });
  const installer = assertPins(root, '9.8.6');
  // Checksums come from the chosen release, not from the newest one.
  for (const name of ['goose', 'kimi']) assert.deepEqual(checksums(installer, name), ['61'.repeat(32), '62'.repeat(32)]);
  assert.equal(lines.length, 7);
  for (const line of lines) assert.match(line, /9\.8\.6.*9\.8\.7.*48 hours/);
});

test('honours DEV_MACHINE_AGENT_CLI_MIN_AGE_HOURS semantics: boundary, custom and zero', async (t) => {
  const published = [['9.8.5', 500], ['9.8.6', 48], ['9.8.7', 47]];
  const build = respond(url => metadata(url, published));
  let root = fixture(t);
  await upgradeAgentClis(root, build, { now });
  assertPins(root, '9.8.6');
  root = fixture(t);
  await upgradeAgentClis(root, build, { now, minAgeHours: 100 });
  assertPins(root, '9.8.5');
  root = fixture(t);
  await upgradeAgentClis(root, build, { now, minAgeHours: 0 });
  assertPins(root, '9.8.7');
  for (const invalid of [-1, 721, 1.5, '48h', Number.NaN]) {
    root = fixture(t);
    const before = read(root);
    await assert.rejects(upgradeAgentClis(root, build, { now, minAgeHours: invalid }), /DEV_MACHINE_AGENT_CLI_MIN_AGE_HOURS/);
    assert.deepEqual(read(root), before);
  }
});

test('ignores prereleases, drafts, deprecated, undated and non-latest-line versions', async (t) => {
  const root = fixture(t);
  await upgradeAgentClis(root, respond((url) => {
    if (url.startsWith('https://registry.npmjs.org/')) {
      const document = npmDocument([
        ['9.8.6', 300], ['9.8.7', 200], ['9.8.8', 150, { deprecated: 'broken build' }],
        ['9.9.0-beta.1', 100], ['10.0.0', 100], ['9.8.9', 100],
      ], '9.8.8');
      delete document.time['9.8.9'];
      // Published under another dist-tag: newer than "latest", so not a stable choice.
      document['dist-tags'].next = '10.0.0';
      return document;
    }
    const releases = githubReleases(url, [
      ['9.8.6', 300], ['9.8.7', 200], ['9.9.0', 100, { prerelease: true }], ['9.9.1', 100, { draft: true }],
      ['9.9.2', 100, { published_at: null }],
    ]);
    releases.unshift({ tag_name: 'stable', draft: false, prerelease: false, published_at: hoursAgo(90), assets: [] });
    releases.unshift({ tag_name: 'v9.9.3-rc.1', draft: false, prerelease: false, published_at: hoursAgo(90), assets: [] });
    return releases;
  }), { now });
  assertPins(root, '9.8.7');
});

test('never downgrades and keeps the pins when nothing newer is old enough', async (t) => {
  const root = fixture(t);
  const before = read(root);
  const lines = [];
  const published = [['0.0.1', 900], ['0.0.2', 800], ['9.8.7', 1]];
  const versions = await upgradeAgentClis(root, respond(url => metadata(url, published)), { now, log: line => lines.push(line) });
  assert.deepEqual(read(root), before);
  const pinned = name => before.join('\n').match(new RegExp(`^(?:ARG )?${name}_VERSION=(.+)$`, 'm'))[1];
  assert.deepEqual(versions, Object.fromEntries(names.map(name => [name, pinned(name)])));
  assert.equal(lines.length, 7);
  for (const line of lines) assert.match(line, /keeping .*9\.8\.7.*48 hours/);
});

for (const scenario of ['http', 'version', 'no-stable-release', 'tag', 'asset', 'digest', 'layout']) {
  test(`fails without writing any pins on invalid ${scenario}`, async (t) => {
    const root = fixture(t);
    if (scenario === 'layout') rmSync(join(root, files[3]));
    const existing = files.filter(file => scenario !== 'layout' || file !== files[3]);
    const before = existing.map(file => readFileSync(join(root, file), 'utf8'));
    await assert.rejects(upgradeAgentClis(root, async (url) => {
      let data = metadata(url);
      const npm = !Array.isArray(data);
      if (scenario === 'version' && npm) data['dist-tags'].latest = 'latest; bad';
      if (scenario === 'no-stable-release' && !npm) data = [];
      // Goose tags carry a "v" and Kimi tags do not; the installer interpolates both shapes.
      if (scenario === 'tag' && !npm) data[0].tag_name = url.includes('kimi-cli') ? 'v9.8.7' : '9.8.7';
      if (scenario === 'asset' && !npm) data[0].assets = [];
      if (scenario === 'digest' && !npm) data[0].assets[0].digest = null;
      return { ok: scenario !== 'http', status: 503, json: async () => data };
    }, { now }));
    assert.deepEqual(existing.map(file => readFileSync(join(root, file), 'utf8')), before);
  });
}
