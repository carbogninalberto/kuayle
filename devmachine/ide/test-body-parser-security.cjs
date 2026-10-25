'use strict';
const assert = require('node:assert/strict');
const http = require('node:http');
const net = require('node:net');
const parser = require(process.argv[2]).json({ limit: 64 });
const server = http.createServer((req, res) => parser(req, res, error => {
  res.statusCode = error ? error.status || 500 : 200;
  res.end(error ? 'rejected' : JSON.stringify(req.body));
}));
async function request(body) {
  return new Promise((resolve, reject) => {
    const req = http.request({ host: '127.0.0.1', port: server.address().port, method: 'POST', headers: { 'Content-Type': 'application/json' } }, res => {
      let data = ''; res.on('data', part => data += part); res.on('end', () => resolve([res.statusCode, data]));
    });
    req.on('error', reject); req.end(body);
  });
}
(async () => {
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  try {
    assert.deepEqual(await request('{"ok":true}'), [200, '{"ok":true}']);
    assert.equal((await request('{broken'))[0], 400);
    assert.equal((await request(JSON.stringify({ value: 'a'.repeat(100) })))[0], 413);
    await new Promise((resolve, reject) => {
      const socket = net.connect(server.address().port, '127.0.0.1', () => {
        socket.write('POST / HTTP/1.1\r\nHost: localhost\r\nContent-Type: application/json\r\nContent-Length: 60\r\n\r\n{"incomplete":');
        setTimeout(() => socket.destroy(), 30);
      });
      socket.on('error', reject); socket.on('close', resolve);
    });
    assert.equal((await request('{"alive":true}'))[0], 200);
    console.log('body-parser normal, invalid, oversized and aborted requests passed');
  } finally { server.closeAllConnections(); await new Promise(resolve => server.close(resolve)); }
})().catch(error => { console.error(error); process.exit(1); });
