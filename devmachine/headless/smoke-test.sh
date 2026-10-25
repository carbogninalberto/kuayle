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

docker exec "$container" curl --fail --silent --output /dev/null http://127.0.0.1:7681/
for command in node npm claude opencode codex qwen crush goose kimi ttyd; do
  docker exec "$container" "$command" --version
done
docker exec "$container" go version

runner_output=$(docker run --rm \
  --tmpfs /run/kuayle-secrets:rw,uid=1000,gid=1000 \
  --entrypoint /bin/sh \
  "$image" -c \
  'touch /run/kuayle-secrets/.ready; exec /usr/local/bin/kuayle-agent-runner /bin/sh -c "printf agent-runner-ok"')
if [ "$runner_output" != "agent-runner-ok" ]; then
  echo "agent-runner mode returned unexpected output: ${runner_output}" >&2
  exit 1
fi

echo 'Headless terminal, providers and agent-runner smoke tests passed'
