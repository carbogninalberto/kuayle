import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

export function checkResults(workflow, needs) {
  if (needs.changes?.result !== 'success') return ['Change detection did not succeed'];
  let required;
  try {
    required = JSON.parse(needs.changes.outputs.plan).required[workflow];
    if (!Array.isArray(required)) throw new Error('Missing workflow selection');
  } catch {
    return ['Change detection did not produce a valid job selection'];
  }
  const errors = [];
  for (const job of required) {
    if (needs[job]?.result !== 'success') errors.push(`${job}: expected success, got ${needs[job]?.result ?? 'missing'}`);
  }
  for (const [job, value] of Object.entries(needs)) {
    if (!['success', 'skipped'].includes(value.result)) errors.push(`${job}: ${value.result}`);
  }
  return errors;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const errors = checkResults(process.argv[2], JSON.parse(process.env.NEEDS_JSON));
  if (errors.length) { console.error(errors.join('\n')); process.exitCode = 1; }
  else console.log('All selected checks passed; unselected checks were intentionally omitted.');
}
