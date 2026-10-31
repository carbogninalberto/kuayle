#!/usr/bin/env node
// Usage: kuayle-claude-config.cjs CONFIG_FILE TRUSTED_DIR...
// Pre-seeds Claude Code's global config (~/.claude.json) so an interactive
// claude skips the first-run onboarding (theme and login screens) and the
// trust dialog for the machine's workspace. Only missing keys are added: a
// value the user set, and their own login, are kept. An unreadable file is
// left unchanged.
'use strict';
const fs = require('fs');
const path = require('path');

const [file, ...trusted] = process.argv.slice(2);
if (!file) {
  process.stderr.write('usage: kuayle-claude-config.cjs CONFIG_FILE TRUSTED_DIR...\n');
  process.exit(2);
}
let config = {};
try {
  config = JSON.parse(fs.readFileSync(file, 'utf8'));
} catch (error) {
  if (error.code !== 'ENOENT') process.exit(0);
}
if (!config || typeof config !== 'object' || Array.isArray(config)) process.exit(0);
let changed = false;
if (config.hasCompletedOnboarding === undefined) {
  config.hasCompletedOnboarding = true;
  changed = true;
}
if (!config.projects || typeof config.projects !== 'object' || Array.isArray(config.projects)) {
  config.projects = {};
}
for (const directory of trusted) {
  const project = config.projects[directory];
  if (project && typeof project === 'object' && project.hasTrustDialogAccepted !== undefined) continue;
  config.projects[directory] = { ...(project && typeof project === 'object' ? project : {}), hasTrustDialogAccepted: true };
  changed = true;
}
if (!changed) process.exit(0);
const temporary = path.join(path.dirname(file), '.' + path.basename(file) + '.kuayle-tmp');
fs.mkdirSync(path.dirname(file), { recursive: true, mode: 0o700 });
fs.writeFileSync(temporary, JSON.stringify(config, null, 2) + '\n', { mode: 0o600 });
fs.renameSync(temporary, file);
