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

configure_codex_deepseek() {
  if [ -z "${DEEPSEEK_API_KEY:-}" ] || [ -z "${KUAYLE_DEEPSEEK_MODEL:-}" ]; then
    return 0
  fi

  codex_config="$HOME/.codex/config.toml"
  if [ -e "$codex_config" ] || [ -L "$codex_config" ]; then
    return 0
  fi

  model=$KUAYLE_DEEPSEEK_MODEL
  case "$model" in
    deepseek-flash|deepseek-v4-pro ) ;;
    * ) echo "invalid DeepSeek model: ${model}" >&2; return 1 ;;
  esac
  thinking=${KUAYLE_DEEPSEEK_THINKING:-high}
  case "$thinking" in
    off ) reasoning_effort=none ;;
    low|high|max ) reasoning_effort=$thinking ;;
    * ) echo "invalid DeepSeek thinking level: ${thinking}" >&2; return 1 ;;
  esac

  mkdir -p "$HOME/.codex"
  if ! (umask 077; set -C; : > "$codex_config") 2>/dev/null; then
    return 0
  fi
  # Codex's /model picker otherwise lists OpenAI models that DeepSeek rejects. A failed
  # generation only loses the picker list; the configured model still works.
  catalog_line=
  catalog_path="$HOME/.codex/model-catalogs/deepseek.json"
  catalog_generator=${KUAYLE_CODEX_CATALOG_GENERATOR:-/usr/local/lib/kuayle/codex-deepseek-catalog.cjs}
  if mkdir -p "${catalog_path%/*}" && node "$catalog_generator" "$catalog_path" 2>/dev/null; then
    catalog_line="model_catalog_json = \"${catalog_path}\""
  fi
  cat > "$codex_config" <<EOF
model = "${model}"
model_provider = "deepseek"
model_reasoning_effort = "${reasoning_effort}"
${catalog_line}

[model_providers.deepseek]
name = "DeepSeek"
base_url = "https://api.deepseek.com"
env_key = "DEEPSEEK_API_KEY"
wire_api = "responses"
EOF
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

prepare_developer_workspace() {
  install_git_credential_helper
  git config --global --unset-all core.askPass >/dev/null 2>&1 || true
  git config --global --unset-all credential.helper >/dev/null 2>&1 || true
  git config --global credential.helper "$credential_helper"
  export GIT_TERMINAL_PROMPT=0

  if [ ! -d "$workspace_root/.git" ] && [ -n "${KUAYLE_REPO_URL:-}" ]; then
    git clone --branch "${KUAYLE_BASE_BRANCH:-main}" --single-branch "$KUAYLE_REPO_URL" "$workspace_root"
  fi

  mkdir -p "$workspace_root/repos" "$workspace_root/tasks"

  if [ -d "$workspace_root/.git" ]; then
    configure_git_helper "$workspace_root"
  fi
  for bare_repository in "$workspace_root"/repos/*.git; do
    [ -d "$bare_repository" ] || continue
    configure_bare_git_helper "$bare_repository"
  done
  for checkout in "$workspace_root"/tasks/*; do
    [ -e "$checkout/.git" ] || continue
    configure_git_helper "$checkout"
  done

  if [ -d "$workspace_root/.git" ] && [ -n "${KUAYLE_WORKING_BRANCH:-}" ]; then
    git -C "$workspace_root" config --global --add safe.directory "$workspace_root"
    if git -C "$workspace_root" ls-remote --exit-code --heads origin "$KUAYLE_WORKING_BRANCH" >/dev/null 2>&1; then
      git -C "$workspace_root" fetch origin "$KUAYLE_WORKING_BRANCH"
      git -C "$workspace_root" checkout -B "$KUAYLE_WORKING_BRANCH" FETCH_HEAD
    else
      git -C "$workspace_root" checkout "$KUAYLE_WORKING_BRANCH" 2>/dev/null || git -C "$workspace_root" checkout -b "$KUAYLE_WORKING_BRANCH"
    fi
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
