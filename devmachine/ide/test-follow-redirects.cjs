'use strict';
// Exercise the actual vendored package: both CVEs leaked credentials on redirects.
const assert = require('node:assert/strict');
const http = require('node:http');
const { once } = require('node:events');
const redirects = require(process.argv[2]);

(async () => {
  const received = [];
  const target = http.createServer((request, response) => {
    received.push(request.headers);
    response.end('redirect reached');
  });
  const source = http.createServer((request, response) => {
    response.writeHead(302, { Location: `http://localhost:${target.address().port}/` });
    response.end();
  });
  const deadline = setTimeout(() => { console.error('Redirect regression test timed out'); process.exit(1); }, 10000);
  try {
    // Listen on both IPv4/IPv6 so localhost's resolver order does not affect the test.
    target.listen(0); await once(target, 'listening');
    source.listen(0); await once(source, 'listening');
    for (const headers of [
      { Authorization: 'secret', Cookie: 'session=secret', 'X-Auth^Token': 'secret', 'X-Auth|Token': 'secret', 'X-Public': 'keep' },
      ['Authorization', 'secret', 'Cookie', 'session=secret', 'X-Auth^Token', 'secret', 'X-Auth|Token', 'secret', 'X-Public', 'keep'],
    ]) {
      await new Promise((resolve, reject) => {
        redirects.http.get({ hostname: '127.0.0.1', path: '/', port: source.address().port, headers, sensitiveHeaders: ['X-Auth^Token', 'X-Auth|Token'] }, (response) => {
          let body = '';
          response.on('data', chunk => { body += chunk; });
          response.on('error', reject);
          response.on('end', () => { try { assert.equal(body, 'redirect reached'); resolve(); } catch (error) { reject(error); } });
        }).on('error', reject);
      });
      const headersAtTarget = received.at(-1);
      for (const name of ['authorization', 'cookie', 'x-auth^token', 'x-auth|token']) assert.equal(headersAtTarget[name], undefined, `${name} leaked across origins`);
      assert.equal(headersAtTarget['x-public'], 'keep');
    }
    console.log('Redirects preserve ordinary headers and strip object/array credentials across origins');
  } finally {
    clearTimeout(deadline);
    source.close(); target.close();
  }
})().catch(error => { console.error(error); process.exitCode = 1; });
