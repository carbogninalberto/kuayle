#!/usr/bin/env node
'use strict';

const assert = require('node:assert/strict');
const CachePolicy = require(process.argv[2]);
const request = { url: 'https://registry.example.test/package', method: 'GET', headers: {} };

function policy(headers, shared = true) {
  const cache = new CachePolicy(request, { status: 200, headers }, { shared });
  // Make each response stale without depending on timers or the wall clock.
  cache.now = () => cache._responseTime + 10_000;
  return cache;
}

let checks = 0;
for (const directive of ['max-stale', 'max-stale=999999']) {
  const nextRequest = { ...request, headers: { 'cache-control': directive } };
  for (const headers of [
    { 'cache-control': 'max-age=1', 'set-cookie': 'session=other-user' },
    { 'cache-control': 'max-age=1, proxy-revalidate' },
    { 'cache-control': 'max-age=1, no-cache' },
    { 'cache-control': 'max-age=1, no-store' },
    { 'cache-control': 'max-age=1, private' },
  ]) {
    const original = policy(headers);
    for (const cache of [original, CachePolicy.fromObject(original.toObject())]) {
      cache.now = original.now;
      const result = cache.evaluateRequest(nextRequest);
      assert.equal(result.response, undefined, JSON.stringify(headers));
      assert.equal(result.revalidation.synchronous, true);
      assert.equal(cache.satisfiesWithoutRevalidation(nextRequest), false);
      checks++;
    }
  }
  // Ordinary expiry and explicit cache opt-ins retain max-stale behavior.
  for (const cache of [
    policy({ 'cache-control': 'max-age=1' }),
    policy({ 'cache-control': 'max-age=1, public', 'set-cookie': 'session=public' }),
    policy({ 'cache-control': 'max-age=1, immutable', 'set-cookie': 'session=immutable' }),
    policy({ 'cache-control': 'max-age=1, private', 'set-cookie': 'session=private' }, false),
  ]) {
    assert.ok(cache.evaluateRequest(nextRequest).response);
    assert.equal(cache.satisfiesWithoutRevalidation(nextRequest), true);
    checks++;
  }
}
console.log(`http-cache-semantics security regression: ${checks} checks passed`);
