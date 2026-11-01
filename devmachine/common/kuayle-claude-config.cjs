#!/usr/bin/env node
// Usage: kuayle-claude-config.cjs CONFIG_FILE
// Pre-seeds Claude Code's global config (~/.claude.json) so an interactive
// claude skips its first-run onboarding (theme and login screens). It adds
// only hasCompletedOnboarding, and only when missing: a value the user set and
// their own login are kept. It never grants workspace trust or tool
// permissions, so Claude Code still asks before trusting a checkout and
// running its committed hooks. An unreadable file is left unchanged.
'use strict';
const fs = require('fs');
const path = require('path');

const file = process.argv[2];
if (!file) {
  process.stderr.write('usage: kuayle-claude-config.cjs CONFIG_FILE\n');
  process.exit(2);
}
let config = {};
try {
  config = JSON.parse(fs.readFileSync(file, 'utf8'));
} catch (error) {
  if (error.code !== 'ENOENT') process.exit(0);
}
if (!config || typeof config !== 'object' || Array.isArray(config)) process.exit(0);
if (config.hasCompletedOnboarding !== undefined) process.exit(0);
config.hasCompletedOnboarding = true;
const temporary = path.join(path.dirname(file), '.' + path.basename(file) + '.kuayle-tmp');
fs.mkdirSync(path.dirname(file), { recursive: true, mode: 0o700 });
fs.writeFileSync(temporary, JSON.stringify(config, null, 2) + '\n', { mode: 0o600 });
fs.renameSync(temporary, file);
