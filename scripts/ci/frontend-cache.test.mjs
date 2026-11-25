import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const action = new URL('../../.github/actions/frontend-dependencies/action.yml', import.meta.url);

// These contracts exercise the fast and cold paths defined by the composite action.
// Changing either guard can silently skip package validation or stale the build types.
test('frontend dependencies reuse exact installs and prepare each fresh checkout', () => {
  const steps = readFileSync(action, 'utf8').split(/^    - /m);
  const step = (text) => steps.find((block) => block.includes(text));
  assert.match(step('actions/setup-node@'), /cache: npm\n\s+cache-dependency-path: .*inputs.directory.*\/package-lock\.json/);
  const modules = step('actions/cache/restore@');
  for (const input of ['node-version', 'runner.arch', 'package-lock.json', 'package.json', '.npmrc']) assert.ok(modules.includes(input), input);
  assert.doesNotMatch(modules, /restore-keys/);
  assert.match(step('run: npm ci'), /if: steps\.modules\.outputs\.cache-hit != 'true'/);
  assert.match(step('run: npm run prepare'), /if: steps\.modules\.outputs\.cache-hit == 'true'/);
  assert.match(step('actions/cache/save@'), /key: \$\{\{ steps\.modules\.outputs\.cache-primary-key \}\}/);
});

test('Chromium cache follows the installed Playwright version and restores OS dependencies on hits', () => {
  const steps = readFileSync(action, 'utf8').split(/^    - /m);
  const browser = steps.find((step) => step.includes('path: ~/.cache/ms-playwright'));
  assert.match(browser, /if: inputs\.chromium == 'true'/);
  assert.match(browser, /key: .*runner\.arch.*steps\.playwright\.outputs\.version/);
  assert.match(steps.find((step) => step.includes('id: playwright')), /playwright-core\/package\.json/);
  const install = steps.find((step) => step.includes('install --with-deps chromium'));
  assert.match(install, /if: inputs\.chromium == 'true'/);
  assert.doesNotMatch(install, /cache-hit/);
});
