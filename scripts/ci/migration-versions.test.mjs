import assert from 'node:assert/strict';
import { execFileSync, spawnSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import test from 'node:test';

const guard = fileURLToPath(new URL('./migration-versions.mjs', import.meta.url));

test('check PR additions against the live base and the complete head tree', (t) => {
  const cwd = mkdtempSync(join(tmpdir(), 'migration-versions-'));
  t.after(() => rmSync(cwd, { recursive: true, force: true }));
  const git = (...args) => execFileSync('git', ['-c', 'user.name=Fixture', '-c', 'user.email=fixture@example.invalid', ...args], { cwd });
  const add = (name) => {
    writeFileSync(join(cwd, 'BE/migrations', name), '-- migration\n');
    git('add', 'BE/migrations');
    git('commit', '-qm', name);
  };
  const check = (...args) => spawnSync(process.execPath, [guard, ...args], { cwd, encoding: 'utf8' });

  git('init', '-qb', 'main');
  mkdirSync(join(cwd, 'BE/migrations'), { recursive: true });
  add('000001_initial.up.sql');
  git('branch', 'task');
  add('000002_other.up.sql');
  git('checkout', '-q', 'task');
  add('000003_task.up.sql');
  add('000003_task.down.sql');
  assert.equal(check('main', 'HEAD').status, 0, 'a new pair and inherited versions pass');
  add('2_task.down.sql');
  const collision = check('main', 'HEAD');
  assert.equal(collision.status, 1);
  assert.match(collision.stderr, /000002_other.up.sql/);
  assert.match(collision.stderr, /2_task.down.sql/);
  git('checkout', '-qb', 'same-path', 'HEAD^');
  add('000002_other.up.sql');
  const samePath = check('main', 'HEAD');
  assert.equal(samePath.status, 1);
  assert.match(samePath.stderr, /reuses version 2/);

  git('checkout', '-q', 'main');
  git('checkout', '-qb', 'duplicate');
  add('000004_first.up.sql');
  add('4_second.down.sql');
  const duplicate = check('main', 'HEAD');
  assert.equal(duplicate.status, 1);
  assert.match(duplicate.stderr, /000004_first.up.sql/);
  assert.match(duplicate.stderr, /4_second.down.sql/);
  assert.notEqual(check('unreadable-base', 'HEAD').status, 0);
});
