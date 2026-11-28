import { execFileSync } from 'node:child_process';
import { appendFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const releaseImages = [
  { id: 'all-in-one', name: 'All-in-one', package: 'kuayle', context: '.', dockerfile: './Dockerfile' },
  { id: 'api', name: 'API', package: 'kuayle-api', context: './BE', dockerfile: './BE/Dockerfile' },
  { id: 'web', name: 'Web', package: 'kuayle-web', context: './UI', dockerfile: './UI/Dockerfile' }
];
const goImage = (id, name) => ({ id, name, context: './BE', dockerfile: `./BE/Dockerfile.${id === 'updater' ? id : `machine-${id}`}`, build_args: '', package: `kuayle-${id === 'updater' ? id : `machine-${id}`}` });
// `package` is the GHCR repository published on release: kuayle-<Compose service>,
// which selfhosting/release_update.py pulls for an active Dev Machines profile.
const devImages = [goImage('gateway', 'Gateway'), goImage('manager', 'Manager'),
  ...['headless', 'egress', 'docker'].map((id) => ({ id, name: id[0].toUpperCase() + id.slice(1), context: './devmachine', dockerfile: `./devmachine/${id}/Dockerfile`, build_args: '', package: `kuayle-dev-machine-${id}` })),
];
const updater = goImage('updater', 'Updater');
// The single machine image (headless) serves the developer terminal and every
// agent run; egress and docker are the machine's other containers.
const runtimeImages = ['headless', 'egress', 'docker'];

// Files no job reads: prose, images for docs, and repository metadata. Markdown
// under UI/, WEB/ and devmachine/ is content that ships, so it keeps its checks.
export function isDocumentation(path) {
  if (/(^|\/)README\.md$/.test(path)) return true;
  if (['LICENSE', 'NOTICE', '.gitignore', '.env.example'].includes(path)) return true;
  if (/^[^/]+\.md$/.test(path)) return true;
  return ['docs/', 'assets/', 'BE/docs/'].some((prefix) => path.startsWith(prefix)) ||
    (path.startsWith('selfhosting/') && path.endsWith('.md'));
}

// Inputs whose result can differ between amd64 and arm64: image definitions,
// dependency locks, native build scripts and the advisory allow-list.
export function platformSensitive(path) {
  return /(^|\/)Dockerfile[^/]*$/.test(path) ||
    ['BE/go.mod', 'BE/go.sum', 'UI/package.json', 'UI/package-lock.json', '.trivyignore.yaml', '.dockerignore', 'selfhosting/image_smoke_test.py'].includes(path) ||
    (path.startsWith('devmachine/') && !path.startsWith('devmachine/security/'));
}

// Only leaf pages whose browser coverage is known can narrow a runtime change.
// The page-height spec also visits each of these settings pages in several viewports.
const settingsSpecs = {
  backups: ['backups'],
  forges: ['forges-settings'],
  import: ['settings-import'],
  lessons: ['lessons'],
  mcp: ['mcp-settings'],
  push: ['push-settings'],
  slack: ['slack-settings'],
  telegram: ['telegram'],
  templates: ['issue-templates'],
  tokens: ['mcp-settings', 'tokens'],
  voice: ['voice'],
  webhooks: ['webhooks-settings']
};

function specsForUIPath(path) {
  const spec = /^UI\/tests\/e2e\/([a-z0-9][a-z0-9-]*\.test\.ts)$/.exec(path);
  if (spec) return [spec[1]];
  const settings = /^UI\/src\/routes\/\[workspaceSlug\]\/settings\/([a-z-]+)\/\+page\.svelte$/.exec(path);
  if (settings && Object.hasOwn(settingsSpecs, settings[1])) {
    return ['page-height', ...settingsSpecs[settings[1]]].map(name => `${name}.test.ts`);
  }
  if (path === 'UI/src/routes/integrations/slack/callback/+page.svelte') return ['slack-settings.test.ts'];
  if (path === 'UI/src/routes/[workspaceSlug]/insights/+page.svelte') return ['insights.test.ts', 'page-height.test.ts'];
  return null;
}

// Match actual consumers, not every file in a broad Docker COPY context.
// Unknown inputs and changes to the routing/workflow implementation run broadly.
export function planForPaths(paths, event = 'pull_request', includeArm64 = false, { frontendOnly = false, integration = false } = {}) {
  const pr = event === 'pull_request';
  const flags = { harness: true, backend: false, frontend: false, web: false, runtime: false, selfhosting: false, ci: false, cli: false };
  const release = new Set(), dev = new Set(), security = new Set();
  const fullRun = !pr && !frontendOnly;
  let reason = pr ? 'PR merge-base diff' : `full ${event} coverage`;
  // Automatic releases build amd64 only; the manual include_arm64 input adds
  // arm64. PRs still add arm64 for inputs that can behave differently there.
  let arm = fullRun && includeArm64;
  const add = (set, values) => values.forEach((value) => set.add(value));
  let frontendSpecs = new Set();
  const selectFrontend = (specs = null) => {
    flags.frontend = true;
    if (specs === null) frontendSpecs = null;
    else if (frontendSpecs) add(frontendSpecs, specs);
  };
  const all = () => {
    selectFrontend();
    for (const key of Object.keys(flags)) flags[key] = true;
    if (pr) arm = true;
    add(release, releaseImages.map((image) => image.id));
    add(dev, devImages.map((image) => image.id));
    security.add('updater');
  };
  if (fullRun) all();
  else if (frontendOnly) {
    flags.harness = false;
    selectFrontend();
    reason = `full frontend suite for ${event}`;
  } else if (integration) {
    selectFrontend();
    reason = 'PR merge-base diff with full frontend coverage on release-integration';
  }
  for (const path of pr ? paths : []) {
    if (path === 'AGENTS.md' || path.startsWith('AGENTS/')) continue;
    if (isDocumentation(path)) continue;
    if (path === 'BE/scripts/install-codex-login.sh') {
      arm = true;
      add(release, ['all-in-one', 'api']);
      continue;
    }
    if (platformSensitive(path)) arm = true;
    if (path.startsWith('.github/workflows/') || path.startsWith('.github/actions/') || path.startsWith('scripts/ci/')) { all(); continue; }
    // Other .github files (funding, issue and PR templates, CODEOWNERS) never reach a job.
    if (path.startsWith('.github/')) continue;
    // The Rust CLI is built only from CLI/; no image or other package uses it.
    if (path.startsWith('CLI/')) { flags.cli = true; continue; }
    if (path === 'Dockerfile') { release.add('all-in-one'); continue; }
    if (path === 'UI/Dockerfile' || path === 'UI/Caddyfile') { release.add('web'); continue; }
    if (path.startsWith('BE/Dockerfile')) {
      const image = [updater, ...devImages].find((item) => item.dockerfile === `./${path}`);
      if (image) { (image.id === 'updater' ? security : dev).add(image.id); }
      else if (path === 'BE/Dockerfile') release.add('api');
      else all();
      continue;
    }
    if (path.startsWith('WEB/')) { flags.web = true; continue; }
    if (path.startsWith('UI/')) {
      selectFrontend(specsForUIPath(path));
      if (!path.startsWith('UI/tests/') && !/^UI\/playwright.*\.ts$/.test(path)) add(release, ['all-in-one', 'web']);
      continue;
    }
    if (path.startsWith('BE/')) {
      flags.backend = true;
      if (path.startsWith('BE/testdata/room-turn/')) {
        selectFrontend(['task-room.test.ts']);
        continue;
      }
      if (/^BE\/internal\/domain\/(?:task_)?room/.test(path)) selectFrontend(['task-room.test.ts']);
      if (path.startsWith('BE/assistant-tools/')) {
        if (['BE/assistant-tools/fetch_url.json', 'BE/assistant-tools/fetch_url.py'].includes(path)) selectFrontend();
        continue;
      }
      // The UI PAT scope contract test reads these backend definitions.
      if (['BE/internal/domain/personal_access_token.go', 'BE/internal/domain/permission.go'].includes(path)) selectFrontend();
      // The UI sanitizer tests read the class allowlist contract shared with the Go sanitizer.
      if (['BE/pkg/sanitize/editor_classes.go', 'BE/pkg/sanitize/testdata/editor_html_contract.json'].includes(path)) selectFrontend();
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
      // Go tests inspect and execute the start, terminal and runner scripts here.
      flags.backend = true;
      flags.runtime = true;
      if (['devmachine/common/start-common.sh', 'devmachine/common/kuayle-service-entrypoint'].includes(path)) add(dev, ['headless', 'egress']);
      else if (path.startsWith('devmachine/common/') || path.startsWith('devmachine/headless/')) add(dev, ['headless']);
      else if (path.startsWith('devmachine/security/')) { /* host policy; exercised by runtime tests */ }
      else {
        const area = path.split('/')[1];
        add(dev, ['egress', 'docker'].includes(area) ? [area] : runtimeImages);
      }
      continue;
    }
    if (path.startsWith('selfhosting/')) {
      flags.selfhosting = true;
      // Release Readiness runs this smoke test against every application image;
      // the other self-hosting files are only read by the self-hosting tests.
      if (path === 'selfhosting/image_smoke_test.py') add(release, releaseImages.map((image) => image.id));
      continue;
    }
    all();
    reason = 'broad coverage for shared or unmapped inputs';
  }
  const plan = { ...flags, reason, frontendSpecs: frontendSpecs && [...frontendSpecs].sort(), architectures: arm ? ['amd64', 'arm64'] : ['amd64'], releaseImages: releaseImages.filter((image) => release.has(image.id)), devImages: devImages.filter((image) => dev.has(image.id)), securityImages: fullRun ? [updater, ...devImages] : [updater].filter((image) => security.has(image.id)) };
  const enabled = (jobs) => Object.entries(jobs).filter(([, run]) => run).map(([id]) => id);
  const jobs = {
    tests: { 'agent-harness': plan.harness, 'backend-test': plan.backend, 'frontend-fast': plan.frontend, 'frontend-browser': plan.frontend, 'frontend-check': plan.frontend, 'web-check': plan.web, 'docker-integration': plan.runtime, 'selfhosting-check': plan.selfhosting, 'ci-validation': plan.ci, 'cli-check': plan.cli },
    security: { 'go-security': plan.backend, 'frontend-security': fullRun, 'docker-security': fullRun, 'dev-machine-security': plan.securityImages.length > 0, 'cli-security': plan.cli },
    // Build & Push runs for PRs, releases and manual republishes. A release
    // builds, scans and publishes every runtime image once in
    // dev-machine-publish instead. Manual dispatch reuses an existing tag, so
    // it skips the release-source version bump.
    build: { 'release-source': fullRun, 'release-context': fullRun, build: fullRun, 'promote-latest': fullRun, 'dev-machine-images': pr && plan.devImages.length > 0, 'dev-machine-publish': fullRun, 'dev-machine-manifests': fullRun },
    readiness: { images: plan.releaseImages.length > 0 }
  };
  plan.jobs = Object.fromEntries(Object.entries(jobs).map(([scope, entries]) => [scope, Object.keys(entries)]));
  plan.required = Object.fromEntries(Object.entries(jobs).map(([scope, entries]) => [scope, enabled(entries)]));
  // A manual republish builds an existing tag, so it does not run the
  // release-source version bump even though that job stays in the catalog.
  if (event === 'workflow_dispatch') plan.required.build = plan.required.build.filter((job) => job !== 'release-source');
  return plan;
}

export function changedPaths(cwd, base, head) {
  if (![base, head].every((sha) => /^[a-f0-9]{40}$/.test(sha ?? ''))) throw new Error('Expected complete base and head commit SHAs');
  // --no-renames emits both old and new names; NUL framing handles whitespace.
  // Three-dot compares from the merge base, retaining every commit in the PR.
  const output = execFileSync('git', ['diff', '--name-only', '--no-renames', '-z', `${base}...${head}`, '--'], { cwd, encoding: 'utf8', maxBuffer: 64 * 1024 * 1024, stdio: ['ignore', 'pipe', 'pipe'] });
  return output.split('\0').filter(Boolean);
}

// A cli-v* release belongs to CLI Release (cli-release.yml). The server
// release jobs in Build & Push must not run for it, so nothing is selected.
export function cliReleasePlan() {
  const plan = planForPaths([]);
  plan.harness = false;
  plan.reason = 'CLI release: published by CLI Release; server release jobs skipped';
  plan.required = Object.fromEntries(Object.keys(plan.required).map((scope) => [scope, []]));
  return plan;
}

export function detectPlan({ cwd = process.cwd(), event, base, head, releaseTag = '', includeArm64 = false, frontendOnly = false, headRef = '', baseRef = '' }) {
  if (event === 'release' && releaseTag.startsWith('cli-v')) return cliReleasePlan();
  if (event !== 'pull_request') return planForPaths([], event || 'unknown', includeArm64, { frontendOnly });
  try {
    const integration = [headRef, baseRef].some(ref => ref.includes('release-integration'));
    const plan = planForPaths(changedPaths(cwd, base, head), event, includeArm64, { integration });
    if (plan.frontendSpecs?.length) {
      // Product tests run at the PR head, while the planner runs at its merge
      // commit. A deletion, rename or missing mapped spec needs the full suite.
      const paths = execFileSync('git', ['ls-tree', '-r', '-z', '--name-only', head, '--', 'UI/tests/e2e/'], { cwd, encoding: 'utf8' });
      const files = new Set(paths.split('\0'));
      if (!plan.frontendSpecs.every(spec => files.has(`UI/tests/e2e/${spec}`))) plan.frontendSpecs = null;
    }
    return plan;
  } catch (error) {
    const plan = planForPaths(['unknown-input'], 'pull_request', includeArm64);
    plan.reason = `fallback to broad PR verification: ${error.message.split('\n')[0]}`;
    return plan;
  }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const plan = detectPlan({
    event: process.env.GITHUB_EVENT_NAME,
    base: process.env.PR_BASE_SHA,
    head: process.env.PR_HEAD_SHA,
    releaseTag: process.env.RELEASE_TAG,
    headRef: process.env.GITHUB_HEAD_REF,
    baseRef: process.env.GITHUB_BASE_REF,
    frontendOnly: process.env.FRONTEND_ONLY === 'true',
    includeArm64: process.env.INCLUDE_ARM64 === 'true'
  });
  const json = JSON.stringify(plan);
  console.log(JSON.stringify(plan, null, 2));
  if (process.env.GITHUB_OUTPUT) appendFileSync(process.env.GITHUB_OUTPUT, `plan=${json}\n`);
  if (process.env.GITHUB_STEP_SUMMARY) appendFileSync(process.env.GITHUB_STEP_SUMMARY, `## CI impact\n\n${plan.reason}\n\n\`\`\`json\n${JSON.stringify({ ...plan.required, frontendSpecs: plan.frontendSpecs ?? 'full suite' }, null, 2)}\n\`\`\`\n`);
}
