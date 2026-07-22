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

home_dir=${HOME:-/home/kuayle}
credential_helper=${KUAYLE_GIT_CREDENTIAL_HELPER:-$home_dir/.kuayle-git-credential}

install_git_credential_helper() {
  case "$credential_helper" in /* ) ;; * ) echo "invalid credential helper path" >&2; exit 1 ;; esac
  helper_dir=${credential_helper%/*}
  [ "$helper_dir" != "$credential_helper" ] || helper_dir=.
  mkdir -p "$helper_dir"
  old_umask=$(umask)
  umask 077
  cat > "$credential_helper" <<'SCRIPT'
#!/bin/sh
case "${1:-get}" in
  get ) ;;
  store|erase ) exit 0 ;;
  * ) exit 0 ;;
esac
if [ -z "${GITHUB_TOKEN:-}" ]; then
  echo "missing active GitHub token" >&2
  exit 1
fi
printf '%s\n' username=x-access-token
printf '%s\n' "password=$GITHUB_TOKEN"
SCRIPT
  chmod 0700 "$credential_helper"
  umask "$old_umask"
}

configure_git_helper() {
  git -C "$1" config --unset-all core.askPass >/dev/null 2>&1 || true
  git -C "$1" config --unset-all credential.helper >/dev/null 2>&1 || true
  git -C "$1" config credential.helper "$credential_helper"
}

configure_bare_git_helper() {
  git --git-dir="$1" config --unset-all core.askPass >/dev/null 2>&1 || true
  git --git-dir="$1" config --unset-all credential.helper >/dev/null 2>&1 || true
  git --git-dir="$1" config credential.helper "$credential_helper"
}

install_git_credential_helper
git config --global --unset-all core.askPass >/dev/null 2>&1 || true
git config --global --unset-all credential.helper >/dev/null 2>&1 || true
git config --global credential.helper "$credential_helper"
export GIT_TERMINAL_PROMPT=0

if [ ! -d /workspace/.git ] && [ -n "${KUAYLE_REPO_URL:-}" ]; then
  git clone --branch "${KUAYLE_BASE_BRANCH:-main}" --single-branch "$KUAYLE_REPO_URL" /workspace
fi

mkdir -p /workspace/repos /workspace/tasks

if [ -d /workspace/.git ]; then
  configure_git_helper /workspace
fi
for bare_repository in /workspace/repos/*.git; do
  [ -d "$bare_repository" ] || continue
  configure_bare_git_helper "$bare_repository"
done
for checkout in /workspace/tasks/*; do
  [ -e "$checkout/.git" ] || continue
  configure_git_helper "$checkout"
done

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
