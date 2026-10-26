import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, copyFileSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, dirname } from 'node:path';
import test from 'node:test';
import { upgradeAgentClis } from './upgrade-agent-clis.mjs';

const files = ['devmachine/common/install-agent-clis.sh', 'devmachine/agent/Dockerfile', 'devmachine/ide/Dockerfile', 'devmachine/headless/Dockerfile'];
function fixture(t) {
  const root = mkdtempSync(join(tmpdir(), 'agent-upgrade-'));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  for (const file of files) {
    mkdirSync(dirname(join(root, file)), { recursive: true });
    copyFileSync(new URL(`../../${file}`, import.meta.url), join(root, file));
  }
  return root;
}
function metadata(url) {
  if (url.startsWith('https://registry.npmjs.org/')) return { version: '9.8.7' };
  const kimi = url.includes('kimi-cli');
  return { tag_name: kimi ? '9.8.7' : 'v9.8.7', assets: ['x86_64', 'aarch64'].map((arch, i) => ({
    name: kimi ? `kimi-9.8.7-${arch}-unknown-linux-gnu.tar.gz` : `goose-${arch}-unknown-linux-gnu.tar.gz`,
    digest: `sha256:${String(i + 1).repeat(64)}`,
  })) };
}
const fetchMetadata = async (url) => ({ ok: true, json: async () => metadata(url) });
test('updates all seven CLIs, synchronized Dockerfile pins and both archive checksums', async (t) => {
  const root = fixture(t);
  await upgradeAgentClis(root, fetchMetadata);
  const installer = readFileSync(join(root, files[0]), 'utf8');
  for (const name of ['CLAUDE_CODE', 'OPENCODE', 'CODEX', 'QWEN', 'GOOSE', 'KIMI']) {
    assert.match(installer, new RegExp(`^${name}_VERSION=9.8.7$`, 'm'));
  }
  for (const name of ['goose', 'kimi']) {
    assert.deepEqual([...installer.matchAll(new RegExp(`${name}_sha256=(\\w+)`, 'g'))].map(m => m[1]), ['1'.repeat(64), '2'.repeat(64)]);
  }
  for (const file of files.slice(1)) {
    const text = readFileSync(join(root, file), 'utf8');
    for (const name of file.includes('/agent/') ? ['CLAUDE_CODE', 'OPENCODE', 'CODEX'] : ['CRUSH']) {
      assert.match(text, new RegExp(`^ARG ${name}_VERSION=9.8.7$`, 'm'));
    }
  }
  const first = files.map(file => readFileSync(join(root, file), 'utf8'));
  await upgradeAgentClis(root, fetchMetadata);
  assert.deepEqual(files.map(file => readFileSync(join(root, file), 'utf8')), first);
});
for (const scenario of ['http', 'version', 'asset', 'digest', 'layout']) {
  test(`fails without writing any pins on invalid ${scenario}`, async (t) => {
    const root = fixture(t);
    if (scenario === 'layout') rmSync(join(root, files[3]));
    const existing = files.filter(file => scenario !== 'layout' || file !== files[3]);
    const before = existing.map(file => readFileSync(join(root, file), 'utf8'));
    await assert.rejects(upgradeAgentClis(root, async (url) => {
      const data = metadata(url);
      if (scenario === 'version') data.version = 'latest; bad';
      if (scenario === 'asset') data.assets = [];
      if (scenario === 'digest' && data.assets) data.assets[0].digest = null;
      return { ok: scenario !== 'http', status: 503, json: async () => data };
    }));
    assert.deepEqual(existing.map(file => readFileSync(join(root, file), 'utf8')), before);
  });
}
