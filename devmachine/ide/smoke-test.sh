#!/bin/sh
set -eu

image=${1:?usage: smoke-test.sh IMAGE}
container=$(docker run --detach --tmpfs /workspace:rw,uid=1000,gid=1000 "$image")
cleanup() { docker rm --force "$container" >/dev/null 2>&1 || true; }
trap cleanup EXIT INT TERM

attempt=0
until docker exec "$container" test -f /workspace/.kuayle-ready; do
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 60 ]; then
    docker logs "$container"
    exit 1
  fi
  sleep 1
done

docker exec "$container" curl --fail --silent http://127.0.0.1:8080/healthz
docker exec "$container" sh -c 'curl --fail --silent --location http://127.0.0.1:8080/ | grep -q "workbench"'
docker exec "$container" curl --fail --silent --output /dev/null http://127.0.0.1:7681/
for command in code-server node npm claude opencode codex ttyd; do
  docker exec "$container" "$command" --version
done

# Exercise the upgraded FTP dependency through its actual get-uri consumer,
# including passive connection negotiation and a downloaded stream.
docker exec --interactive "$container" /usr/lib/code-server/lib/node - <<'JS'
const assert = require('node:assert/strict');
const net = require('node:net');
const root = '/usr/lib/code-server/node_modules/';
const { getUri } = require(root + 'get-uri');
assert.equal(require(root + 'js-yaml').load('enabled: true').enabled, true);
assert.equal(typeof require('/usr/lib/code-server/lib/vscode/node_modules/undici').fetch, 'function');
const deadline = setTimeout(() => { console.error('FTP compatibility test timed out'); process.exit(1); }, 10000);
const payload = 'kuayle-ftp-compatibility';
let passive;
const server = net.createServer((socket) => {
  socket.write('220 test server\r\n');
  let pending = '';
  socket.on('data', (chunk) => {
    pending += chunk;
    let newline;
    while ((newline = pending.indexOf('\r\n')) >= 0) {
      const line = pending.slice(0, newline);
      pending = pending.slice(newline + 2);
      const command = line.split(' ')[0];
      if (command === 'USER') socket.write('331 password required\r\n');
      else if (command === 'PASS') socket.write('230 logged in\r\n');
      else if (command === 'FEAT') socket.write('211 no features\r\n');
      else if (command === 'MDTM') socket.write('213 20260101000000\r\n');
      else if (command === 'EPSV') {
        passive = net.createServer((data) => {
          data.on('error', () => {});
          socket.once('transfer', () => { data.end(payload); socket.write('226 transfer complete\r\n'); });
        });
        passive.listen(0, '127.0.0.1', () => socket.write(`229 Entering Extended Passive Mode (|||${passive.address().port}|)\r\n`));
      } else if (command === 'RETR') {
        socket.write('150 sending file\r\n');
        socket.emit('transfer');
      } else if (command === 'QUIT') socket.end('221 goodbye\r\n');
      else socket.write('200 accepted\r\n');
    }
  });
});
server.listen(0, '127.0.0.1', async () => {
  try {
    const stream = await getUri(new URL(`ftp://test:secret@127.0.0.1:${server.address().port}/file.txt`));
    let result = '';
    for await (const chunk of stream) result += chunk;
    assert.equal(result, payload);
    console.log('code-server YAML, undici and FTP consumer checks passed');
    clearTimeout(deadline);
    passive.close();
    server.close();
  } catch (error) { console.error(error); process.exit(1); }
});
JS

echo 'IDE startup, HTTP services, providers and dependency smoke tests passed'
