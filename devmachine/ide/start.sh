#!/bin/sh
set -eu

rm -f /workspace/.kuayle-ready
mkdir -p "$HOME/.local/share/code-server" "$HOME/.config/code-server"
if [ -d /opt/kuayle-home-template ] && [ ! -f "$HOME/.kuayle-template-seeded" ]; then
  cp -a /opt/kuayle-home-template/. "$HOME/"
  touch "$HOME/.kuayle-template-seeded"
fi
user_settings_dir="$HOME/.local/share/code-server/User"
if [ ! -e "$user_settings_dir/settings.json" ]; then
  mkdir -p "$user_settings_dir"
  printf '%s\n' '{"workbench.colorTheme":"Default Dark Modern"}' > "$user_settings_dir/settings.json"
fi

if [ ! -d /workspace/.git ] && [ -n "${KUAYLE_REPO_URL:-}" ]; then
  if [ -n "${GITHUB_TOKEN:-}" ]; then
    askpass="$HOME/.kuayle-git-askpass"
    cat > "$askpass" <<'SCRIPT'
#!/bin/sh
case "$1" in
  *Username*) printf '%s\n' x-access-token ;;
  *) printf '%s\n' "$GITHUB_TOKEN" ;;
esac
SCRIPT
    chmod 0700 "$askpass"
    git config --global core.askPass "$askpass"
    GIT_ASKPASS="$askpass" GIT_TERMINAL_PROMPT=0 git clone --branch "${KUAYLE_BASE_BRANCH:-main}" --single-branch "$KUAYLE_REPO_URL" /workspace
  else
    GIT_TERMINAL_PROMPT=0 git clone --branch "${KUAYLE_BASE_BRANCH:-main}" --single-branch "$KUAYLE_REPO_URL" /workspace
  fi
fi

mkdir -p /workspace/repos /workspace/tasks

if [ -d /workspace/.git ] && [ -n "${KUAYLE_WORKING_BRANCH:-}" ]; then
  git -C /workspace config --global --add safe.directory /workspace
  if git -C /workspace ls-remote --exit-code --heads origin "$KUAYLE_WORKING_BRANCH" >/dev/null 2>&1; then
    git -C /workspace fetch origin "$KUAYLE_WORKING_BRANCH"
    git -C /workspace checkout -B "$KUAYLE_WORKING_BRANCH" FETCH_HEAD
  else
    git -C /workspace checkout "$KUAYLE_WORKING_BRANCH" 2>/dev/null || git -C /workspace checkout -b "$KUAYLE_WORKING_BRANCH"
  fi
  hooks=/workspace/.git/hooks
  cat > "$hooks/post-commit" <<'SCRIPT'
#!/bin/sh
commit=$(git rev-parse HEAD)
curl -fsS -X POST -H 'Content-Type: application/json' --data "{\"source\":\"git\",\"event_type\":\"git.commit_created\",\"payload\":{\"commit\":\"$commit\"}}" "$KUAYLE_COLLECTOR_URL/event" >/dev/null 2>&1 || true
SCRIPT
  cat > "$hooks/post-checkout" <<'SCRIPT'
#!/bin/sh
branch=$(git branch --show-current)
curl -fsS -X POST -H 'Content-Type: application/json' --data "{\"source\":\"git\",\"event_type\":\"git.branch_changed\",\"payload\":{\"branch\":\"$branch\"}}" "$KUAYLE_COLLECTOR_URL/event" >/dev/null 2>&1 || true
SCRIPT
  cat > "$hooks/pre-push" <<'SCRIPT'
#!/bin/sh
curl -fsS -X POST -H 'Content-Type: application/json' --data '{"source":"git","event_type":"git.push_started","payload":{}}' "$KUAYLE_COLLECTOR_URL/event" >/dev/null 2>&1 || true
SCRIPT
  chmod 0755 "$hooks/post-commit" "$hooks/post-checkout" "$hooks/pre-push"
fi

default_workspace=/workspace/tasks
[ ! -d /workspace/.git ] || default_workspace=/workspace
code-server --bind-addr 0.0.0.0:8080 --auth none --disable-telemetry "$default_workspace" &
code_server_pid=$!
ttyd --port 7681 --writable --url-arg --terminal-type xterm-256color /usr/local/bin/kuayle-terminal-session &
ttyd_pid=$!

cleanup() {
  kill "$code_server_pid" "$ttyd_pid" 2>/dev/null || true
  wait "$code_server_pid" "$ttyd_pid" 2>/dev/null || true
}
trap cleanup INT TERM EXIT

attempt=0
while ! curl -fsS http://127.0.0.1:8080/healthz >/dev/null 2>&1 || ! kill -0 "$ttyd_pid" 2>/dev/null; do
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 300 ]; then
    echo "developer services did not become ready" >&2
    exit 1
  fi
  sleep 0.1
done
touch /workspace/.kuayle-ready

while kill -0 "$code_server_pid" 2>/dev/null && kill -0 "$ttyd_pid" 2>/dev/null; do
  sleep 1
done
exit 1
