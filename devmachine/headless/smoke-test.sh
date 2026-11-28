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
for command in node npm claude opencode codex pi qwen crush goose kimi ttyd; do
  docker exec "$container" "$command" --version
done
# Every flag Kuayle passes an agent CLI is still in its help.
docker exec "$container" sh /usr/local/lib/kuayle/check-agent-cli-flags.sh
docker exec "$container" go version
docker exec "$container" sh -c 'kuayle-ports list | grep -Eq "^7681 +listening"'
docker exec "$container" test -f /home/kuayle/.claude/skills/kuayle-qa/SKILL.md
docker exec "$container" test -f /home/kuayle/.codex/skills/kuayle-qa/SKILL.md
docker exec "$container" test -f /home/kuayle/.claude/skills/kuayle-agents/SKILL.md
docker exec "$container" test -f /home/kuayle/.codex/skills/kuayle-agents/SKILL.md
docker exec "$container" test -f /home/kuayle/.config/opencode/skill/kuayle-agents/SKILL.md

# A regular Ubuntu: manual pages, the fixed font set and the locale.
docker exec "$container" test -x /usr/bin/man
docker exec "$container" sh -c 'locale | grep -q "^LANG=en_US.UTF-8"'
for family in 'Ubuntu' 'Noto Sans' 'Noto Sans CJK SC' 'Noto Color Emoji' 'Liberation Sans' 'DejaVu Sans'; do
  docker exec "$container" fc-list : family | tr ',' '\n' | grep -qx "$family" || {
    echo "font family missing: $family" >&2
    exit 1
  }
done

# kuayle-screenshot renders with the image's Playwright Chromium as UID 1000,
# into the size it was asked for.
docker exec "$container" kuayle-screenshot --width 640 --height 360 \
  'data:text/html;charset=utf-8,<body style="margin:0;background:%23336699;font-family:Ubuntu">Kuayle 字 😀</body>' \
  /tmp/smoke.png
size=$(docker exec "$container" node -e '
const b = require("fs").readFileSync("/tmp/smoke.png");
if (b.toString("latin1", 1, 4) !== "PNG") process.exit(1);
process.stdout.write(b.readUInt32BE(16) + "x" + b.readUInt32BE(20));')
if [ "$size" != "640x360" ]; then
  echo "kuayle-screenshot produced ${size:-no PNG}, expected 640x360" >&2
  exit 1
fi

# Claude Code's managed settings carry the live inbox hook.
docker exec "$container" grep -q kuayle-live-inbox-hook /etc/claude-code/managed-settings.json

# Agent containers run without no-new-privileges, so sudo works there and
# an agent can install system packages for the attempt.
sudo_output=$(docker run --rm --tmpfs /run/kuayle-secrets:rw,uid=1000,gid=1000 --entrypoint /bin/sh "$image" -c \
  'touch /run/kuayle-secrets/.ready; exec /usr/local/bin/kuayle-agent-runner sudo -n id -u')
if [ "$sudo_output" != "0" ]; then
  echo "sudo in an agent run returned: ${sudo_output}" >&2
  exit 1
fi

runner_output=$(docker run --rm \
  --tmpfs /run/kuayle-secrets:rw,uid=1000,gid=1000 \
  --entrypoint /bin/sh \
  "$image" -c \
  'touch /run/kuayle-secrets/.ready; exec /usr/local/bin/kuayle-agent-runner /bin/sh -c "printf agent-runner-ok"')
if [ "$runner_output" != "agent-runner-ok" ]; then
  echo "agent-runner mode returned unexpected output: ${runner_output}" >&2
  exit 1
fi

echo "Headless terminal, providers, browser QA and agent-runner smoke tests passed"
