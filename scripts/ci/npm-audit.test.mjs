import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { mkdtempSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';
import { evaluate } from './npm-audit.mjs';

const advisory = (id, name, severity) => ({
  source: 1,
  name,
  title: `${name} problem`,
  url: `https://github.com/advisories/${id}`,
  severity,
  range: '<=1.0.0',
});
const report = (...advisories) => ({
  auditReportVersion: 2,
  vulnerabilities: Object.fromEntries(
    advisories.map((a) => [a.name, { name: a.name, severity: a.severity, via: [a] }])
  ),
});
const PICOMATCH = advisory('GHSA-fc5q-39xc-vjpf', 'picomatch', 'high');
const entry = (overrides = {}) => ({ id: 'GHSA-fc5q-39xc-vjpf', package: 'picomatch', reason: 'build-time only', review_by: '2026-12-31', ...overrides });
const TODAY = '2026-11-17';

test('only the allowlisted high advisory passes and is reported as skipped', () => {
  const result = evaluate(report(PICOMATCH), [entry()], TODAY);
  assert.equal(result.ok, true);
  assert.equal(result.skipped.length, 1);
  assert.match(result.skipped[0], /GHSA-fc5q-39xc-vjpf.*build-time only/);
});

test('an additional high or critical advisory fails', () => {
  for (const severity of ['high', 'critical']) {
    const result = evaluate(report(PICOMATCH, advisory('GHSA-aaaa-bbbb-cccc', 'left-pad', severity)), [entry()], TODAY);
    assert.equal(result.ok, false);
    assert.match(result.failures.join('\n'), /GHSA-aaaa-bbbb-cccc/);
  }
});

test('the allowlist does not cover the same id reported for another package', () => {
  const result = evaluate(report(advisory('GHSA-fc5q-39xc-vjpf', 'other', 'high'), PICOMATCH), [entry()], TODAY);
  assert.equal(result.ok, false);
});

test('moderate and low advisories alone pass', () => {
  const result = evaluate(report(advisory('GHSA-aaaa-bbbb-cccc', 'a', 'moderate'), advisory('GHSA-dddd-eeee-ffff', 'b', 'low')), [], TODAY);
  assert.equal(result.ok, true);
});

test('advisories reached only through string via entries are not double counted', () => {
  const data = report(PICOMATCH);
  data.vulnerabilities.vite = { name: 'vite', severity: 'high', via: ['picomatch'] };
  assert.equal(evaluate(data, [entry()], TODAY).ok, true);
});

test('an allowlisted id that is no longer reported fails', () => {
  const result = evaluate(report(), [entry()], TODAY);
  assert.equal(result.ok, false);
  assert.match(result.failures.join('\n'), /no longer reported/);
});

test('an expired review_by fails, and the review day itself still passes', () => {
  const expired = evaluate(report(PICOMATCH), [entry({ review_by: '2026-11-16' })], TODAY);
  assert.equal(expired.ok, false);
  assert.match(expired.failures.join('\n'), /expired on 2026-11-16/);
  assert.equal(evaluate(report(PICOMATCH), [entry({ review_by: TODAY })], TODAY).ok, true);
});

test('malformed allowlist entries and reports fail closed', () => {
  assert.throws(() => evaluate(report(PICOMATCH), [entry({ review_by: 'soon' })], TODAY), /invalid review_by/);
  assert.throws(() => evaluate(report(PICOMATCH), [entry({ reason: '' })], TODAY), /reason/);
  assert.throws(() => evaluate({ error: { code: 'ENOAUDIT', summary: 'offline' } }, [], TODAY), /offline/);
  assert.throws(() => evaluate({}, [], TODAY), /vulnerabilities/);
});

function cli(stdin, allowlist = [entry()]) {
  const dir = mkdtempSync(join(tmpdir(), 'npm-audit-'));
  const file = join(dir, 'allowlist.json');
  writeFileSync(file, JSON.stringify(allowlist));
  return spawnSync(process.execPath, [new URL('./npm-audit.mjs', import.meta.url).pathname, '--stdin', '--allowlist', file, '--today', TODAY], { input: stdin, encoding: 'utf8' });
}

test('cli exits 0 for the allowlisted advisory and 1 for malformed JSON', () => {
  assert.equal(cli(JSON.stringify(report(PICOMATCH))).status, 0);
  const bad = cli('{ not json');
  assert.equal(bad.status, 1);
  assert.match(bad.stderr, /FAIL/);
  assert.equal(cli('').status, 1);
});

test('cli lists every blocking advisory on failure', () => {
  const result = cli(JSON.stringify(report(PICOMATCH, advisory('GHSA-aaaa-bbbb-cccc', 'left-pad', 'critical'))));
  assert.equal(result.status, 1);
  assert.match(result.stderr, /critical advisory GHSA-aaaa-bbbb-cccc/);
  assert.match(result.stdout, /Allowlisted: GHSA-fc5q-39xc-vjpf/);
});
