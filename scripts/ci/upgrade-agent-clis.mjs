import { readFileSync, writeFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const npmPackages = {
  CLAUDE_CODE: '@anthropic-ai/claude-code', OPENCODE: 'opencode-ai',
  CODEX: '@openai/codex', QWEN: '@qwen-code/qwen-code',
};
const repositories = { CRUSH: 'charmbracelet/crush', GOOSE: 'block/goose', KIMI: 'MoonshotAI/kimi-cli' };
// Same setting, default and range as the machine manager's runtime refresh.
const minAgeVariable = 'DEV_MACHINE_AGENT_CLI_MIN_AGE_HOURS';
const stable = /^\d+\.\d+\.\d+$/;
function version(value) {
  if (typeof value !== 'string' || !stable.test(value)) {
    throw new Error(`Invalid stable CLI version: ${value}`);
  }
  return value;
}
function compare(a, b) {
  const [left, right] = [a, b].map(value => value.split('.').map(Number));
  return left[0] - right[0] || left[1] - right[1] || left[2] - right[2];
}
const newest = values => values.reduce((best, value) => (best && compare(best, value) >= 0 ? best : value), undefined);
function minimumAgeHours(value) {
  if (value === undefined || value === '') return 48;
  const hours = typeof value === 'number' ? value : /^\d+$/.test(value) ? Number(value) : Number.NaN;
  if (!Number.isInteger(hours) || hours < 0 || hours > 720) {
    throw new Error(`${minAgeVariable} must be between 0 and 720`);
  }
  return hours;
}
function replace(text, pattern, replacement, count = 1) {
  if ([...text.matchAll(pattern)].length !== count) throw new Error(`Unexpected pin layout: ${pattern}`);
  return text.replace(pattern, replacement);
}
function pinned(text, pattern) {
  const matches = [...text.matchAll(pattern)];
  if (matches.length !== 1) throw new Error(`Unexpected pin layout: ${pattern}`);
  return version(matches[0][1]);
}

// Resolve and validate every upstream value and local edit before writing files.
// A version is only adopted once it has been published for at least the minimum
// age, so a compromised release has time to be withdrawn; pins are never lowered.
export async function upgradeAgentClis(root, fetchMetadata = fetch, options = {}) {
  const minAgeHours = minimumAgeHours(options.minAgeHours ?? process.env[minAgeVariable]);
  const cutoff = (options.now ?? Date.now()) - minAgeHours * 3600000;
  const log = options.log ?? (() => {});
  const oldEnough = published => typeof published === 'string' && Date.parse(published) <= cutoff;
  async function json(url) {
    const headers = { Accept: 'application/json' };
    if (url.startsWith('https://api.github.com/') && process.env.GH_TOKEN) {
      headers.Authorization = `Bearer ${process.env.GH_TOKEN}`;
    }
    const response = await fetchMetadata(url, { headers, signal: AbortSignal.timeout(30000) });
    if (!response.ok) throw new Error(`Metadata request failed (${response.status}): ${url}`);
    return response.json();
  }

  const installerPath = resolve(root, 'devmachine/common/install-agent-clis.sh');
  const dockerfiles = new Map(['agent', 'ide', 'headless'].map((image) => {
    const path = resolve(root, `devmachine/${image}/Dockerfile`);
    return [image, { path, text: readFileSync(path, 'utf8') }];
  }));
  let installer = readFileSync(installerPath, 'utf8');
  const current = {};
  for (const name of [...Object.keys(npmPackages), ...Object.keys(repositories)]) {
    current[name] = name === 'CRUSH'
      ? newest(['ide', 'headless'].map(image => pinned(dockerfiles.get(image).text, /^ARG CRUSH_VERSION=(.+)$/gm)))
      : pinned(installer, new RegExp(`^${name}_VERSION=(.+)$`, 'gm'));
  }

  // Newest stable version that is old enough; `latest` is the newest stable one upstream.
  function choose(name, latest, eligible) {
    const candidate = newest(eligible);
    const waiting = compare(latest, candidate ?? current[name]) > 0 && compare(latest, current[name]) > 0
      ? `; ${latest} is not ${minAgeHours} hours old yet` : '';
    if (!candidate || compare(candidate, current[name]) <= 0) {
      log(`${name}: keeping ${current[name]}, no newer stable version is at least ${minAgeHours} hours old${waiting}`);
      return current[name];
    }
    log(`${name}: ${current[name]} -> ${candidate}${waiting}`);
    return candidate;
  }

  const versions = {};
  const checksums = {};
  for (const [name, pkg] of Object.entries(npmPackages)) {
    const document = await json(`https://registry.npmjs.org/${encodeURIComponent(pkg)}`);
    const latest = version(document['dist-tags']?.latest);
    // Versions above `latest` belong to other dist-tags (next, beta lines).
    versions[name] = choose(name, latest, Object.keys(document.versions ?? {}).filter(candidate =>
      stable.test(candidate) && compare(candidate, latest) <= 0 && !document.versions[candidate]?.deprecated
      && oldEnough(document.time?.[candidate])));
  }
  for (const [name, repo] of Object.entries(repositories)) {
    const releases = await json(`https://api.github.com/repos/${repo}/releases?per_page=100`);
    if (!Array.isArray(releases)) throw new Error(`Unexpected release list: ${repo}`);
    const released = new Map();
    for (const release of releases) {
      const candidate = typeof release?.tag_name === 'string' ? release.tag_name.replace(/^v/, '') : '';
      if (release?.draft || release?.prerelease || !stable.test(candidate)) continue;
      if (!released.has(candidate)) released.set(candidate, release);
    }
    if (released.size === 0) throw new Error(`Expected stable release: ${repo}`);
    versions[name] = choose(name, newest([...released.keys()]),
      [...released.keys()].filter(candidate => oldEnough(released.get(candidate).published_at)));
    if (name === 'CRUSH' || versions[name] === current[name]) continue;
    const release = released.get(versions[name]);
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
  for (const [name, value] of Object.entries(versions)) {
    if (name === 'CRUSH') continue;
    installer = replace(installer, new RegExp(`^${name}_VERSION=.+$`, 'gm'), `${name}_VERSION=${value}`);
  }
  // A kept pin keeps its checksums; the layout is still checked.
  for (const name of ['GOOSE', 'KIMI']) {
    let index = 0;
    installer = replace(installer, new RegExp(`^(    ${name.toLowerCase()}_sha256=).+$`, 'gm'),
      (line, prefix) => (checksums[name] ? prefix + checksums[name][index++] : line), 2);
  }
  edits.set(installerPath, installer);
  for (const [image, { path, text }] of dockerfiles) {
    let dockerfile = text;
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
  console.log(JSON.stringify(await upgradeAgentClis(root, fetch, { log: console.error }), null, 2));
}
