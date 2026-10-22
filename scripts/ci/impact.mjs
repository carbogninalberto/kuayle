import { execFileSync } from 'node:child_process';
import { appendFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const releaseImages = [
  { id: 'all-in-one', context: '.', dockerfile: './Dockerfile' },
  { id: 'api', context: './BE', dockerfile: './BE/Dockerfile' },
  { id: 'web', context: './UI', dockerfile: './UI/Dockerfile' }
];
const goImage = (id, name) => ({ id, name, context: './BE', dockerfile: `./BE/Dockerfile.${id === 'updater' ? id : `machine-${id}`}`, build_args: '' });
const devImages = [goImage('gateway', 'Gateway'), goImage('manager', 'Manager'),
  ...['ide', 'browser', 'collector', 'egress'].map((id) => ({ id, name: id === 'ide' ? 'IDE' : id[0].toUpperCase() + id.slice(1), context: './devmachine', dockerfile: `./devmachine/${id}/Dockerfile`, build_args: '' })),
  ...[['claude', 'Claude', 'claude-code'], ['opencode', 'OpenCode', 'opencode'], ['codex', 'Codex', 'codex']].map(([id, name, provider]) => ({ id: `agent-${id}`, name: `Agent ${name}`, context: './devmachine', dockerfile: './devmachine/agent/Dockerfile', build_args: `PROVIDER=${provider}` }))
];
const updater = goImage('updater', 'Updater');
const agents = ['agent-claude', 'agent-opencode', 'agent-codex'];
const runtimeImages = ['ide', 'browser', 'collector', 'egress', ...agents];

// Match actual consumers, not every file in a broad Docker COPY context.
// Unknown inputs and changes to the routing/workflow implementation run broadly.
export function planForPaths(paths, event = 'pull_request') {
  const pr = event === 'pull_request';
  const flags = { harness: true, backend: false, frontend: false, web: false, runtime: false, selfhosting: false, ci: false };
  const release = new Set(), dev = new Set(), security = new Set();
  let reason = pr ? 'PR merge-base diff' : `full ${event} coverage`;
  const add = (set, values) => values.forEach((value) => set.add(value));
  const all = () => {
    for (const key of Object.keys(flags)) flags[key] = true;
    add(release, releaseImages.map((image) => image.id));
    add(dev, devImages.map((image) => image.id));
    security.add('updater');
  };
  if (!pr) all();
  for (const path of pr ? paths : []) {
    if (path === 'AGENTS.md' || path.startsWith('AGENTS/')) continue;
    if (/^(README\.md|TECHNICAL\.md|LICENSE)$/.test(path) || path.startsWith('docs/') || path.startsWith('assets/') || path.startsWith('BE/docs/') || /(^|\/)README\.md$/.test(path)) continue;
    if (path.startsWith('.github/') || path.startsWith('scripts/ci/')) { all(); continue; }
    if (path === 'Dockerfile') { release.add('all-in-one'); continue; }
    if (path === 'UI/Dockerfile' || path === 'UI/Caddyfile') { release.add('web'); continue; }
    if (path.startsWith('BE/Dockerfile')) {
      const image = [updater, ...devImages].find((item) => item.dockerfile === `./${path}`);
      if (image) { (image.id === 'updater' ? security : dev).add(image.id); flags.runtime ||= image.id !== 'updater'; }
      else if (path === 'BE/Dockerfile') release.add('api');
      else all();
      continue;
    }
    if (path.startsWith('WEB/')) { flags.web = true; continue; }
    if (path.startsWith('UI/')) {
      flags.frontend = true;
      if (!path.startsWith('UI/tests/') && !/^UI\/playwright.*\.ts$/.test(path)) add(release, ['all-in-one', 'web']);
      if (path === 'UI/static/releases.json') flags.web = true;
      continue;
    }
    if (path.startsWith('BE/')) {
      flags.backend = true;
      if (path.endsWith('_test.go')) {
        if (path === 'BE/internal/machine/docker_integration_test.go') flags.runtime = true;
      } else {
        add(release, ['all-in-one', 'api']);
        add(dev, ['gateway', 'manager']);
        security.add('updater');
        flags.runtime = true;
      }
      continue;
    }
    if (path.startsWith('devmachine/')) {
      // Go tests inspect and execute checkout/terminal/runner scripts here.
      flags.backend = true;
      flags.runtime = true;
      if (path === 'devmachine/common/kuayle-service-entrypoint') add(dev, ['ide', 'browser', 'collector', 'egress']);
      else if (path === 'devmachine/common/kuayle-agent-runner') add(dev, agents);
      else if (['devmachine/common/patch-npm-security.cjs', 'devmachine/common/test-http-cache-semantics.cjs'].includes(path)) add(dev, ['ide', ...agents]);
      else if (path.startsWith('devmachine/agent/')) add(dev, agents);
      else if (path.startsWith('devmachine/security/')) { /* host policy; exercised by runtime tests */ }
      else {
        const area = path.split('/')[1];
        add(dev, ['ide', 'browser', 'collector', 'egress'].includes(area) ? [area] : runtimeImages);
      }
      continue;
    }
    if (path.startsWith('selfhosting/')) {
      flags.selfhosting = true;
      add(release, releaseImages.map((image) => image.id));
      continue;
    }
    all();
    reason = 'broad coverage for shared or unmapped inputs';
  }
  const plan = { ...flags, reason, releaseImages: releaseImages.filter((image) => release.has(image.id)), devImages: devImages.filter((image) => dev.has(image.id)), securityImages: pr ? [updater].filter((image) => security.has(image.id)) : [updater, ...devImages] };
  const enabled = (jobs) => Object.entries(jobs).filter(([, run]) => run).map(([id]) => id);
  plan.required = {
    tests: enabled({ 'agent-harness': plan.harness, 'backend-test': plan.backend, 'frontend-check': plan.frontend, 'web-check': plan.web, 'docker-integration': plan.runtime, 'selfhosting-check': plan.selfhosting, 'ci-validation': plan.ci }),
    security: enabled({ 'go-security': plan.backend, 'frontend-security': plan.frontend, 'docker-security': !pr, 'dev-machine-security': plan.securityImages.length > 0 }),
    build: enabled({ build: !pr, 'dev-machine-images': plan.devImages.length > 0 }),
    readiness: enabled({ images: plan.releaseImages.length > 0 })
  };
  return plan;
}

export function changedPaths(cwd, base, head) {
  if (![base, head].every((sha) => /^[a-f0-9]{40}$/.test(sha ?? ''))) throw new Error('Expected complete base and head commit SHAs');
  // --no-renames emits both old and new names; NUL framing handles whitespace.
  // Three-dot compares from the merge base, retaining every commit in the PR.
  const output = execFileSync('git', ['diff', '--name-only', '--no-renames', '-z', `${base}...${head}`, '--'], { cwd, encoding: 'utf8', maxBuffer: 64 * 1024 * 1024, stdio: ['ignore', 'pipe', 'pipe'] });
  return output.split('\0').filter(Boolean);
}

export function detectPlan({ cwd = process.cwd(), event, base, head }) {
  if (event !== 'pull_request') return planForPaths([], event || 'unknown');
  try {
    return planForPaths(changedPaths(cwd, base, head));
  } catch (error) {
    const plan = planForPaths(['unknown-input']);
    plan.reason = `fallback to broad PR verification: ${error.message.split('\n')[0]}`;
    return plan;
  }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const plan = detectPlan({ event: process.env.GITHUB_EVENT_NAME, base: process.env.PR_BASE_SHA, head: process.env.PR_HEAD_SHA });
  const json = JSON.stringify(plan);
  console.log(JSON.stringify(plan, null, 2));
  if (process.env.GITHUB_OUTPUT) appendFileSync(process.env.GITHUB_OUTPUT, `plan=${json}\n`);
  if (process.env.GITHUB_STEP_SUMMARY) appendFileSync(process.env.GITHUB_STEP_SUMMARY, `## CI impact\n\n${plan.reason}\n\n\`\`\`json\n${JSON.stringify(plan.required, null, 2)}\n\`\`\`\n`);
}
