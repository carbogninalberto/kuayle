import { readFileSync, writeFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const npmPackages = {
  CLAUDE_CODE: '@anthropic-ai/claude-code', OPENCODE: 'opencode-ai',
  CODEX: '@openai/codex', QWEN: '@qwen-code/qwen-code',
};
const repositories = { CRUSH: 'charmbracelet/crush', GOOSE: 'block/goose', KIMI: 'MoonshotAI/kimi-cli' };
function version(value) {
  if (typeof value !== 'string' || !/^\d+\.\d+\.\d+$/.test(value)) {
    throw new Error(`Invalid stable CLI version: ${value}`);
  }
  return value;
}
function replace(text, pattern, replacement, count = 1) {
  if ([...text.matchAll(pattern)].length !== count) throw new Error(`Unexpected pin layout: ${pattern}`);
  return text.replace(pattern, replacement);
}

// Resolve and validate every upstream value and local edit before writing files.
export async function upgradeAgentClis(root, fetchMetadata = fetch) {
  async function json(url) {
    const headers = { Accept: 'application/json' };
    if (url.startsWith('https://api.github.com/') && process.env.GH_TOKEN) {
      headers.Authorization = `Bearer ${process.env.GH_TOKEN}`;
    }
    const response = await fetchMetadata(url, { headers, signal: AbortSignal.timeout(30000) });
    if (!response.ok) throw new Error(`Metadata request failed (${response.status}): ${url}`);
    return response.json();
  }
  const versions = {};
  const checksums = {};
  for (const [name, pkg] of Object.entries(npmPackages)) {
    versions[name] = version((await json(`https://registry.npmjs.org/${encodeURIComponent(pkg)}/latest`)).version);
  }
  for (const [name, repo] of Object.entries(repositories)) {
    const release = await json(`https://api.github.com/repos/${repo}/releases/latest`);
    versions[name] = version(release.tag_name?.replace(/^v/, ''));
    if (release.draft || release.prerelease) throw new Error(`Expected stable release: ${repo}`);
    if (name === 'CRUSH') continue;
    // These tags are interpolated by the installer; reject upstream naming changes.
    const expectedTag = name === 'GOOSE' ? `v${versions[name]}` : versions[name];
    if (release.tag_name !== expectedTag) throw new Error(`Unexpected release tag: ${repo}`);
    checksums[name] = ['x86_64', 'aarch64'].map(arch => {
      const assetName = name === 'GOOSE' ? `goose-${arch}-unknown-linux-gnu.tar.gz`
        : `kimi-${versions[name]}-${arch}-unknown-linux-gnu.tar.gz`;
      const assets = release.assets?.filter(asset => asset.name === assetName) ?? [];
      if (assets.length !== 1 || !/^sha256:[a-f0-9]{64}$/.test(assets[0].digest ?? '')) {
        throw new Error(`Missing asset or SHA-256 digest: ${repo}/${assetName}`);
      }
      return assets[0].digest.slice(7);
    });
  }
  const edits = new Map();
  const installerPath = resolve(root, 'devmachine/common/install-agent-clis.sh');
  let installer = readFileSync(installerPath, 'utf8');
  for (const [name, value] of Object.entries(versions)) {
    if (name === 'CRUSH') continue;
    installer = replace(installer, new RegExp(`^${name}_VERSION=.+$`, 'gm'), `${name}_VERSION=${value}`);
  }
  for (const [name, values] of Object.entries(checksums)) {
    let index = 0;
    installer = replace(installer, new RegExp(`^(    ${name.toLowerCase()}_sha256=).+$`, 'gm'),
      (_, prefix) => prefix + values[index++], 2);
  }
  edits.set(installerPath, installer);
  for (const image of ['agent', 'ide', 'headless']) {
    const path = resolve(root, `devmachine/${image}/Dockerfile`);
    let dockerfile = readFileSync(path, 'utf8');
    for (const name of image === 'agent' ? ['CLAUDE_CODE', 'OPENCODE', 'CODEX'] : ['CRUSH']) {
      dockerfile = replace(dockerfile, new RegExp(`^ARG ${name}_VERSION=.+$`, 'gm'), `ARG ${name}_VERSION=${versions[name]}`);
    }
    edits.set(path, dockerfile);
  }
  for (const [path, content] of edits) writeFileSync(path, content);
  return versions;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const root = fileURLToPath(new URL('../../', import.meta.url));
  console.log(JSON.stringify(await upgradeAgentClis(root), null, 2));
}
