#!/bin/sh

workspace_root=${KUAYLE_WORKSPACE_ROOT:-/workspace}

reset_ready_marker() {
  rm -f "$workspace_root/.kuayle-ready"
}

seed_home_template() {
  if [ -d /opt/kuayle-home-template ] && [ ! -f "$HOME/.kuayle-template-seeded" ]; then
    cp -a /opt/kuayle-home-template/. "$HOME/"
    touch "$HOME/.kuayle-template-seeded"
  fi
}

# Points an interactive Codex at the workspace's default model server. Agent
# runs pass the server explicitly as -c overrides, so this file only serves
# terminals and the IDE, and an existing user-managed config is left unchanged.
configure_codex_model() {
  if [ -z "${KUAYLE_MODEL_API_KEY:-}" ] || [ -z "${KUAYLE_MODEL_OPENAI_BASE_URL:-}" ] || [ -z "${KUAYLE_MODEL:-}" ]; then
    return 0
  fi

  codex_config="$HOME/.codex/config.toml"
  if [ -e "$codex_config" ] || [ -L "$codex_config" ]; then
    return 0
  fi

  # Values are written into TOML strings: refuse anything that needs quoting.
  model=$KUAYLE_MODEL
  case "$model" in
    ''|*[!A-Za-z0-9._:/@+-]* ) echo "invalid model name" >&2; return 1 ;;
  esac
  base_url=$KUAYLE_MODEL_OPENAI_BASE_URL
  case "$base_url" in
    http://*|https://* ) ;;
    * ) echo "invalid model server URL" >&2; return 1 ;;
  esac
  case "$base_url" in
    *[\"\'\\[:space:]]* ) echo "invalid model server URL" >&2; return 1 ;;
  esac
  thinking=${KUAYLE_MODEL_THINKING:-high}
  case "$thinking" in
    off ) reasoning_effort=none ;;
    low|high|max ) reasoning_effort=$thinking ;;
    * ) echo "invalid thinking level: ${thinking}" >&2; return 1 ;;
  esac

  mkdir -p "$HOME/.codex"
  if ! (umask 077; set -C; : > "$codex_config") 2>/dev/null; then
    return 0
  fi
  # Codex's /model picker otherwise lists OpenAI models the server may reject.
  # A failed generation only loses the picker list; the configured model works.
  catalog_line=
  catalog_path="$HOME/.codex/model-catalogs/kuayle.json"
  catalog_generator=${KUAYLE_CODEX_CATALOG_GENERATOR:-/usr/local/lib/kuayle/codex-model-catalog.cjs}
  if [ -n "${KUAYLE_MODEL_LIST:-}" ] && mkdir -p "${catalog_path%/*}" && node "$catalog_generator" "$catalog_path" "$KUAYLE_MODEL_LIST" 2>/dev/null; then
    catalog_line="model_catalog_json = \"${catalog_path}\""
  fi
  cat > "$codex_config" <<CONFIG
model = "${model}"
model_provider = "kuayle"
model_reasoning_effort = "${reasoning_effort}"
${catalog_line}

[model_providers.kuayle]
name = "Kuayle model server"
base_url = "${base_url}"
env_key = "KUAYLE_MODEL_API_KEY"
wire_api = "responses"
CONFIG
}

# Codex keeps its managed app-server (~400 MB per version) in ~/.codex/packages.
# The home is a small in-memory tmpfs, so the cache lives on the machine's
# workspace volume, shared by its terminal and agent containers.
link_codex_packages() {
  packages="$HOME/.codex/packages"
  if [ -e "$packages" ] || [ -L "$packages" ]; then
    return 0
  fi
  cache="$workspace_root/.kuayle/codex-packages"
  mkdir -p "$cache" "$HOME/.codex" 2>/dev/null || return 0
  ln -s "$cache" "$packages" 2>/dev/null || true
}

load_scoped_secrets() {
  attempt=0
  while [ ! -f /run/kuayle-secrets/.ready ]; do
    attempt=$((attempt + 1))
    if [ "$attempt" -ge 300 ]; then
      echo "timed out waiting for scoped secrets" >&2
      exit 1
    fi
    sleep 0.1
  done

  old_ifs=$IFS
  IFS=,
  for secret_name in ${KUAYLE_SECRET_NAMES:-}; do
    case "$secret_name" in
      ''|*[!A-Za-z0-9_]*) echo "invalid secret name" >&2; exit 1 ;;
    esac
    secret_file="/run/kuayle-secrets/$secret_name"
    if [ -f "$secret_file" ]; then
      secret_value=$(cat "$secret_file"; printf x)
      secret_value=${secret_value%x}
      export "$secret_name=$secret_value"
      rm -f "$secret_file"
    fi
  done
  IFS=$old_ifs
  rm -f /run/kuayle-secrets/.ready
}

# Writes the user's stored agent sign-in, delivered as a scoped secret, into
# the run's in-memory home where Claude Code or Codex expects it.
install_agent_sign_in() {
  sign_in_file=
  sign_in_value=
  if [ -n "${KUAYLE_CLAUDE_CREDENTIALS:-}" ]; then
    sign_in_file="$HOME/.claude/.credentials.json"
    sign_in_value=$KUAYLE_CLAUDE_CREDENTIALS
  elif [ -n "${KUAYLE_CODEX_AUTH:-}" ]; then
    sign_in_file="$HOME/.codex/auth.json"
    sign_in_value=$KUAYLE_CODEX_AUTH
  fi
  unset KUAYLE_CLAUDE_CREDENTIALS KUAYLE_CODEX_AUTH
  [ -n "$sign_in_file" ] || return 0
  mkdir -p "${sign_in_file%/*}"
  (umask 077; printf '%s' "$sign_in_value" > "$sign_in_file")
  sign_in_value=
}

# Hands the sign-in back after the run, so Kuayle keeps a token the agent
# refreshed. The run's home is in memory and disappears when it exits, so the
# file goes to the run's own path on the workspace volume; the manager reads
# and deletes it immediately.
return_agent_sign_in() {
  [ -n "${sign_in_file:-}" ] && [ -f "$sign_in_file" ] || return 0
  run_id=${KUAYLE_AGENT_RUN_ID:-}
  case "$run_id" in
    ''|*[!0-9a-f-]*) return 0 ;;
  esac
  return_dir="$workspace_root/.kuayle/sign-in-return"
  (umask 077; mkdir -p "$return_dir") || return 0
  chmod 700 "$return_dir" 2>/dev/null || true
  (umask 077; cp "$sign_in_file" "$return_dir/$run_id.json.tmp" && mv -f "$return_dir/$run_id.json.tmp" "$return_dir/$run_id.json") || true
}

install_git_credential_helper() {
  home_dir=${HOME:-/home/kuayle}
  credential_helper=${KUAYLE_GIT_CREDENTIAL_HELPER:-$home_dir/.kuayle-git-credential}
  case "$credential_helper" in /* ) ;; * ) echo "invalid credential helper path" >&2; exit 1 ;; esac
  helper_dir=${credential_helper%/*}
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

# Creates the machine's root checkout of KUAYLE_REPO_URL once. The workspace
# volume is not empty by then: it holds Kuayle's own state (the Codex package
# cache, repository caches, task worktrees) and, after a restart, the user's
# work. The repository is cloned into a staging directory on the same volume,
# its files are moved into place without replacing anything already there,
# and .git is moved last, so an interrupted preparation is completed by the
# next start and an existing .git always means the workspace is prepared.
prepare_root_checkout() {
  kuayle_root_checkout_created=0
  [ -n "${KUAYLE_REPO_URL:-}" ] || return 0
  [ ! -e "$workspace_root/.git" ] || return 0
  staging="$workspace_root/.kuayle/root-checkout"
  rm -rf "$staging"
  mkdir -p "$workspace_root/.kuayle"
  git clone --branch "${KUAYLE_BASE_BRANCH:-main}" --single-branch "$KUAYLE_REPO_URL" "$staging"
  for entry in "$staging"/* "$staging"/.[!.]* "$staging"/..?*; do
    [ -e "$entry" ] || [ -L "$entry" ] || continue
    name=${entry##*/}
    [ "$name" != .git ] || continue
    if [ -e "$workspace_root/$name" ] || [ -L "$workspace_root/$name" ]; then
      echo "workspace already contains ${name}; keeping the existing copy" >&2
      continue
    fi
    mv "$entry" "$workspace_root/$name"
  done
  mv "$staging/.git" "$workspace_root/.git"
  rm -rf "$staging"
  kuayle_root_checkout_created=1
}

prepare_developer_workspace() {
  install_git_credential_helper
  git config --global --unset-all core.askPass >/dev/null 2>&1 || true
  git config --global --unset-all credential.helper >/dev/null 2>&1 || true
  git config --global credential.helper "$credential_helper"
  export GIT_TERMINAL_PROMPT=0

  prepare_root_checkout
  root_checkout_created=$kuayle_root_checkout_created

  mkdir -p "$workspace_root/repos" "$workspace_root/tasks"

  if [ -d "$workspace_root/.git" ]; then
    configure_git_helper "$workspace_root"
  fi
  # Repository caches: repos/<owner>/<repo>.git, and the flat
  # repos/<owner>-<repo>.git layout of earlier checkouts.
  for bare_repository in "$workspace_root"/repos/*.git "$workspace_root"/repos/*/*.git; do
    [ -d "$bare_repository" ] || continue
    configure_bare_git_helper "$bare_repository"
  done
  for checkout in "$workspace_root"/tasks/*; do
    [ -e "$checkout/.git" ] || continue
    configure_git_helper "$checkout"
  done

  # The working branch is selected once, when the root checkout is created. A
  # restart keeps whatever branch and local commits the workspace has.
  if [ -d "$workspace_root/.git" ]; then
    git config --global --add safe.directory "$workspace_root"
  fi
  if [ "$root_checkout_created" = 1 ] && [ -n "${KUAYLE_WORKING_BRANCH:-}" ]; then
    if git -C "$workspace_root" ls-remote --exit-code --heads origin "$KUAYLE_WORKING_BRANCH" >/dev/null 2>&1; then
      git -C "$workspace_root" fetch origin "$KUAYLE_WORKING_BRANCH"
      git -C "$workspace_root" checkout -B "$KUAYLE_WORKING_BRANCH" FETCH_HEAD
    else
      git -C "$workspace_root" checkout "$KUAYLE_WORKING_BRANCH" 2>/dev/null || git -C "$workspace_root" checkout -b "$KUAYLE_WORKING_BRANCH"
    fi
  fi
  if [ -d "$workspace_root/.git" ] && [ -n "${KUAYLE_WORKING_BRANCH:-}" ]; then
    hooks="$workspace_root/.git/hooks"
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

  kuayle_default_workspace="$workspace_root/tasks"
  [ ! -d "$workspace_root/.git" ] || kuayle_default_workspace=$workspace_root
}
