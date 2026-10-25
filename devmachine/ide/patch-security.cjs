'use strict';

const fs = require('node:fs');
const path = require('node:path');
const os = require('node:os');
const { execFileSync } = require('node:child_process');

// Zod 4.6.7 fixes GHSA-mrhx-c7xc-m3w5 in v4 but still bundles the old v3
// implementation. Backport the same early upper-bound rejection to v3 instead
// of changing v3 consumers to an incompatible major API or suppressing the scan.
function patchZodV3(root) {
  for (const extension of ['cjs', 'js']) {
    const file = path.join(root, 'v3', `types.${extension}`);
    let source = fs.readFileSync(file, 'utf8');
    const invalid = extension === 'cjs' ? 'parseUtil_js_1.INVALID' : 'INVALID';
    for (const field of ['exactLength', 'maxLength', 'maxSize']) {
      const pattern = new RegExp(`(message: def\\.${field}\\.message,\\n\\s+\\}\\);\\n\\s+)status\\.dirty\\(\\);`, 'g');
      let count = 0;
      source = source.replace(pattern, (_, prefix) => {
        count++;
        return prefix + (field === 'exactLength' ? `if (tooBig) return ${invalid};\n                status.dirty();` : `return ${invalid};`);
      });
      if (count !== 1) throw new Error(`Zod v3 ${field}/${extension} changed; review the security backport`);
    }
    fs.writeFileSync(file, source);
  }
}

function findPackages(root, name) {
  const found = [];
  for (const entry of fs.readdirSync(root, { withFileTypes: true })) {
    if (!entry.isDirectory()) continue;
    const dir = path.join(root, entry.name);
    const manifest = path.join(dir, 'package.json');
    if (entry.name === name && fs.existsSync(manifest) && JSON.parse(fs.readFileSync(manifest)).name === name) found.push(dir);
    else found.push(...findPackages(dir, name));
  }
  return found;
}

function patch(root) {
  const staging = fs.mkdtempSync(path.join(os.tmpdir(), 'kuayle-ide-security-'));
  try {
    execFileSync('npm', ['install', '--prefix', staging, '--ignore-scripts', '--install-strategy=nested', '--package-lock=false', '--no-audit', '--no-fund', 'body-parser@2.4.0', 'zod@4.6.7'], { stdio: 'inherit' });
    const source = path.join(staging, 'node_modules', 'zod');
    patchZodV3(source);
    const targets = findPackages(root, 'zod');
    if (!targets.length) throw new Error('No vendor Zod copies found; review this override');
    for (const target of targets) {
      const original = JSON.parse(fs.readFileSync(path.join(target, 'package.json'))).version;
      if (!/^[34]\./.test(original)) throw new Error(`Unsupported Zod version ${original}`);
      fs.rmSync(target, { recursive: true });
      fs.cpSync(source, target, { recursive: true });
      if (original.startsWith('3.')) {
        // Preserve the v3 root entrypoint as well as explicit zod/v3 and zod/v4.
        fs.writeFileSync(path.join(target, 'index.cjs'), 'module.exports = require("./v3/index.cjs");\n');
        for (const ext of ['js', 'd.ts', 'd.cts']) fs.writeFileSync(path.join(target, `index.${ext}`), 'export * from "./v3/index.js";\nexport { default } from "./v3/index.js";\n');
        if (require(target).ZodFirstPartyTypeKind.ZodString !== 'ZodString') throw new Error('Zod v3 root API was not preserved');
      }
      execFileSync(process.execPath, [path.join(__dirname, 'test-zod-security.cjs'), target], { stdio: 'inherit' });
    }
    const parsers = findPackages(root, 'body-parser');
    if (!parsers.length) throw new Error('No vendor body-parser copies found; review this override');
    for (const target of parsers) {
      const version = JSON.parse(fs.readFileSync(path.join(target, 'package.json'))).version;
      if (!version.startsWith('2.')) throw new Error(`Unsupported body-parser version ${version}`);
      fs.rmSync(target, { recursive: true });
      // Nested install carries the parser's complete dependency tree, not only
      // its entrypoint with potentially incompatible old transitive packages.
      fs.cpSync(path.join(staging, 'node_modules', 'body-parser'), target, { recursive: true });
      execFileSync(process.execPath, [path.join(__dirname, 'test-body-parser-security.cjs'), target], { stdio: 'inherit' });
    }
  } finally { fs.rmSync(staging, { recursive: true, force: true }); }
}
module.exports = { patchZodV3 };
if (require.main === module) patch(process.argv[2] || '/usr/lib/code-server');
