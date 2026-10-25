'use strict';
const assert = require('node:assert/strict');
const { pathToFileURL } = require('node:url');
const path = require('node:path');

async function check(z) {
  let calls = 0;
  const element = z.string().transform(value => { calls++; return value; });
  for (const schema of [z.array(element).max(1), z.array(element).length(1)]) {
    const result = schema.safeParse(['a', 'b']);
    assert.equal(result.success, false);
    assert.equal(result.error.issues.length, 1);
    assert.equal(result.error.issues[0].code, 'too_big');
    assert.equal(calls, 0, 'oversized arrays must not parse elements');
    const asyncResult = await schema.safeParseAsync(['a', 'b']);
    assert.equal(asyncResult.success, false);
    assert.equal(calls, 0);
    assert.deepEqual(schema.parse(['a']), ['a']);
    calls = 0;
  }
  const set = z.set(element).max(1);
  assert.equal(set.safeParse(new Set(['a', 'b'])).success, false);
  assert.equal(calls, 0, 'oversized sets must not parse elements');
  assert.equal(z.object({ name: z.string() }).parse({ name: 'ok' }).name, 'ok');
}
(async () => {
  const root = path.resolve(process.argv[2]);
  for (const sub of ['', 'v3', 'v4']) {
    const dir = path.join(root, sub);
    await check(require(path.join(dir, 'index.cjs')));
    await check(await import(pathToFileURL(path.join(dir, 'index.js')).href));
  }
  console.log('Zod CommonJS/ESM collection bounds and compatibility passed');
})().catch(error => { console.error(error); process.exit(1); });
