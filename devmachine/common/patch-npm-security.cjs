#!/usr/bin/env node
'use strict';

// npm bundles its dependencies, so upgrading npm alone does not refresh them.
// Keep compatible major versions here and remove these overrides when npm ships fixes.
const fs = require('node:fs');
const path = require('node:path');
const os = require('node:os');
const { execFileSync } = require('node:child_process');

const patches = {
  'brace-expansion': '5.0.12',
  undici: '6.28.1',
  tar: '7.5.22',
  'ip-address': '10.5.0',
  'http-cache-semantics': '4.3.0',
};

function guardCacheSemantics(source) {
  // CVE-2026-93748 / GHSA-ch52-4w7c-c8xp remains unfixed in upstream 4.3.0.
  // max-stale must not undo maxAge() security prohibitions. Return the normal
  // revalidation result before either stale-serving path can disclose a response.
  const anchor = '    evaluateRequest(req) {\n        this._assertRequestHasHeaders(req);';
  const guard = `${anchor}

        // Kuayle security backport: CVE-2026-93748 (upstream issue #56).
        if (
            !this.storable() || this._rescc['no-cache'] ||
            (this._isShared && (
                this._rescc['proxy-revalidate'] ||
                (this._resHeaders['set-cookie'] && !this._rescc.public && !this._rescc.immutable)
            ))
        ) {
            return this._evaluateRequestMissResult(req);
        }`;
  if (source.split(anchor).length !== 2) {
    throw new Error('http-cache-semantics changed; review the security backport before building');
  }
  return source.replace(anchor, guard);
}

function findCachePackages(root) {
  const found = [];
  for (const entry of fs.readdirSync(root, { withFileTypes: true })) {
    if (!entry.isDirectory()) continue;
    const location = path.join(root, entry.name);
    if (entry.name === 'http-cache-semantics' && fs.existsSync(path.join(location, 'package.json')) &&
        JSON.parse(fs.readFileSync(path.join(location, 'package.json'))).name === entry.name) {
      found.push(location);
    } else {
      found.push(...findCachePackages(location));
    }
  }
  return found;
}

function patchNpm() {
  const npmRoot = execFileSync('npm', ['root', '--global'], { encoding: 'utf8' }).trim();
  const target = path.join(npmRoot, 'npm', 'node_modules');
  const staging = fs.mkdtempSync(path.join(os.tmpdir(), 'kuayle-npm-security-'));
  try {
    execFileSync('npm', [
      'install', '--prefix', staging, '--ignore-scripts', '--package-lock=false',
      '--no-audit', '--no-fund',
      ...Object.entries(patches).map(([name, version]) => `${name}@${version}`),
    ], { stdio: 'inherit' });
    for (const [name, version] of Object.entries(patches)) {
      const packagePath = path.join(staging, 'node_modules', name);
      const actual = JSON.parse(fs.readFileSync(path.join(packagePath, 'package.json'))).version;
      if (actual !== version) throw new Error(`Unexpected ${name} version ${actual}`);
      const destination = path.join(target, name);
      if (!fs.existsSync(destination)) throw new Error(`npm no longer bundles ${name}; review its override`);
      fs.rmSync(destination, { recursive: true, force: true });
      fs.cpSync(packagePath, destination, { recursive: true });
    }
    // Optional additional roots cover vendor trees such as code-server. Only the
    // dependency with an unfixed upstream security flaw is changed in these trees.
    const cachePackages = new Set([
      path.join(target, 'http-cache-semantics'),
      ...process.argv.slice(2).flatMap(findCachePackages),
    ]);
    for (const location of cachePackages) {
      fs.rmSync(location, { recursive: true, force: true });
      fs.cpSync(path.join(staging, 'node_modules', 'http-cache-semantics'), location, { recursive: true });
      const cacheSource = path.join(location, 'index.js');
      fs.writeFileSync(cacheSource, guardCacheSemantics(fs.readFileSync(cacheSource, 'utf8')));
      execFileSync(process.execPath, [path.join(__dirname, 'test-http-cache-semantics.cjs'), location], { stdio: 'inherit' });
    }
    execFileSync('npm', ['--version'], { stdio: 'inherit' });
  } finally {
    fs.rmSync(staging, { recursive: true, force: true });
  }
}

module.exports = { guardCacheSemantics };
if (require.main === module) patchNpm();
