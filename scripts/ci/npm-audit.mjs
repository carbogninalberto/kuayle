#!/usr/bin/env node
// Frontend dependency audit gate.
//
// Runs `npm audit --json` in the current directory and fails on every high or
// critical advisory except the entries in the allowlist (default:
// ./npm-audit-allowlist.json). It also fails when an allowlisted advisory is no
// longer reported (the exception must be removed) or its review date has passed.
//
// Usage: node npm-audit.mjs [--allowlist <file>] [--stdin] [--today YYYY-MM-DD]
//   --stdin   read the `npm audit --json` output from stdin instead of running npm
import { spawnSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

const BLOCKING = new Set(['high', 'critical']);
const DATE = /^\d{4}-\d{2}-\d{2}$/;

// Every advisory object in the report. String `via` entries only name another
// vulnerable package, so they carry no advisory of their own.
export function collectAdvisories(report) {
  if (report === null || typeof report !== 'object' || Array.isArray(report)) {
    throw new Error('npm audit output is not a JSON object');
  }
  if (report.error) {
    const detail = typeof report.error === 'object' ? report.error.summary || report.error.code : report.error;
    throw new Error(`npm audit reported an error: ${detail ?? 'unknown'}`);
  }
  const vulnerabilities = report.vulnerabilities;
  if (vulnerabilities === null || typeof vulnerabilities !== 'object' || Array.isArray(vulnerabilities)) {
    throw new Error('npm audit output has no "vulnerabilities" object');
  }
  const advisories = new Map();
  for (const [pkg, vulnerability] of Object.entries(vulnerabilities)) {
    for (const via of vulnerability?.via ?? []) {
      if (via === null || typeof via !== 'object') continue;
      const id = String(via.url ?? '').match(/GHSA(?:-[0-9a-z]{4}){3}/i)?.[0] ?? (via.source != null ? String(via.source) : null);
      if (!id) throw new Error(`advisory without id in ${pkg}`);
      if (!advisories.has(id)) {
        advisories.set(id, { id, package: via.name ?? pkg, severity: String(via.severity ?? '').toLowerCase(), title: via.title ?? '', url: via.url ?? '' });
      }
    }
  }
  return [...advisories.values()];
}

export function validateAllowlist(entries) {
  if (!Array.isArray(entries)) throw new Error('allowlist must be a JSON array');
  const seen = new Set();
  for (const entry of entries) {
    for (const key of ['id', 'package', 'reason', 'review_by']) {
      if (typeof entry?.[key] !== 'string' || !entry[key].trim()) throw new Error(`allowlist entry needs a non-empty "${key}": ${JSON.stringify(entry)}`);
    }
    if (!DATE.test(entry.review_by) || Number.isNaN(Date.parse(entry.review_by))) {
      throw new Error(`allowlist entry ${entry.id} has an invalid review_by "${entry.review_by}" (use YYYY-MM-DD)`);
    }
    if (seen.has(entry.id)) throw new Error(`allowlist lists ${entry.id} twice`);
    seen.add(entry.id);
  }
  return entries;
}

// Returns { ok, failures, skipped } without printing or exiting.
export function evaluate(report, allowlist, today) {
  const advisories = collectAdvisories(report);
  validateAllowlist(allowlist);
  const failures = [];
  const skipped = [];
  const reported = new Set(advisories.map((advisory) => advisory.id));

  const active = new Map();
  for (const entry of allowlist) {
    if (today > entry.review_by) {
      failures.push(`Allowlist entry ${entry.id} (${entry.package}) expired on ${entry.review_by}: re-check the advisory, then extend review_by or remove the entry.`);
    } else {
      active.set(entry.id, entry);
    }
    if (!reported.has(entry.id)) {
      failures.push(`Allowlist entry ${entry.id} (${entry.package}) is no longer reported by npm audit: remove it from the allowlist.`);
    }
  }

  for (const advisory of advisories) {
    if (!BLOCKING.has(advisory.severity)) continue;
    const entry = active.get(advisory.id);
    if (entry && entry.package === advisory.package) {
      skipped.push(`${advisory.id} ${advisory.package} (${advisory.severity}): ${entry.reason} [review by ${entry.review_by}]`);
    } else {
      failures.push(`${advisory.severity} advisory ${advisory.id} in ${advisory.package}: ${advisory.title} ${advisory.url}`.trim());
    }
  }
  return { ok: failures.length === 0, failures, skipped, advisories };
}

function runNpmAudit() {
  const result = spawnSync('npm', ['audit', '--json'], { encoding: 'utf8', maxBuffer: 256 * 1024 * 1024 });
  if (result.error) throw new Error(`could not run npm audit: ${result.error.message}`);
  // npm audit exits non-zero when it finds vulnerabilities, so parse the output
  // whatever the status is; unparsable output fails closed.
  if (!result.stdout.trim()) throw new Error(`npm audit produced no output (exit ${result.status}): ${result.stderr.trim()}`);
  return result.stdout;
}

function main(argv) {
  const option = (name) => { const index = argv.indexOf(name); return index >= 0 ? argv[index + 1] : undefined; };
  const allowlistPath = option('--allowlist') ?? 'npm-audit-allowlist.json';
  const today = option('--today') ?? new Date().toISOString().slice(0, 10);
  try {
    const allowlist = JSON.parse(readFileSync(allowlistPath, 'utf8'));
    const raw = argv.includes('--stdin') ? readFileSync(0, 'utf8') : runNpmAudit();
    const { ok, failures, skipped, advisories } = evaluate(JSON.parse(raw), allowlist, today);
    for (const line of skipped) console.log(`Allowlisted: ${line}`);
    if (!ok) {
      for (const line of failures) console.error(`FAIL: ${line}`);
      process.exit(1);
    }
    console.log(`npm audit: ${advisories.length} advisories, no unallowlisted high or critical ones.`);
  } catch (error) {
    console.error(`FAIL: ${error.message}`);
    process.exit(1);
  }
}

if (process.argv[1] === fileURLToPath(import.meta.url)) main(process.argv.slice(2));
