#!/bin/sh
set -eu

repository=${1:-}
base_branch=${2:-}
working_branch=${3:-}
workspace_path=${4:-}

case "$repository" in
  [A-Za-z0-9-]*/[A-Za-z0-9._-]* ) ;;
  * ) echo "invalid repository" >&2; exit 1 ;;
esac
case "$workspace_path" in
  /workspace/tasks/* )
    case "$workspace_path" in *..*|*//*|/workspace/tasks/*/* ) echo "invalid workspace path" >&2; exit 1 ;; esac
    workspace_name=${workspace_path#/workspace/tasks/}
    case "$workspace_name" in ''|.|..|*[!A-Za-z0-9._-]* ) echo "invalid workspace path" >&2; exit 1 ;; esac
    ;;
  * ) echo "invalid workspace path" >&2; exit 1 ;;
esac
valid_ref() {
  ref=$1
  case "$ref" in ''|-*|.*|*..*|*//*|*@\{*|*~*|*^*|*:*|*\?*|*\**|*'['*|*'\'*|*/|*. ) return 1 ;; esac
  old_ifs=$IFS
  IFS=/
  for component in $ref; do
    case "$component" in ''|.|..|*.lock ) IFS=$old_ifs; return 1 ;; esac
    case "$component" in *[!A-Za-z0-9._-]* ) IFS=$old_ifs; return 1 ;; esac
  done
  IFS=$old_ifs
  return 0
}
valid_ref "$base_branch" && valid_ref "$working_branch" || { echo "invalid branch" >&2; exit 1; }

IFS= read -r github_token
[ -n "$github_token" ] || { echo "missing repository credential" >&2; exit 1; }
token_file=/run/kuayle-secrets/github-token
umask 077
printf '%s' "$github_token" > "$token_file"
unset github_token

askpass=/tmp/kuayle-checkout-askpass
cat > "$askpass" <<'SCRIPT'
#!/bin/sh
case "$1" in
  *Username*) printf '%s\n' x-access-token ;;
  *) cat /run/kuayle-secrets/github-token ;;
esac
SCRIPT
chmod 0700 "$askpass"
trap 'rm -f "$askpass"' EXIT INT TERM
export GIT_ASKPASS="$askpass" GIT_TERMINAL_PROMPT=0

repository_key=$(printf '%s' "$repository" | tr '/' '-')
bare_repository="/workspace/repos/${repository_key}.git"
repository_url="https://github.com/${repository}"

if [ ! -d "$bare_repository" ]; then
  git clone --bare "$repository_url" "$bare_repository"
  git --git-dir="$bare_repository" config remote.origin.fetch '+refs/heads/*:refs/remotes/origin/*'
else
  git --git-dir="$bare_repository" remote set-url origin "$repository_url"
fi

git --git-dir="$bare_repository" fetch --prune origin
git --git-dir="$bare_repository" worktree prune

if [ -e "$workspace_path" ]; then
  [ -e "$workspace_path/.git" ] || { echo "workspace path is not an existing checkout" >&2; exit 1; }
  ln -sfn "$workspace_path" /workspace/current
  exit 0
fi

mkdir -p "$(dirname "$workspace_path")"
if git --git-dir="$bare_repository" show-ref --verify --quiet "refs/remotes/origin/$working_branch"; then
  git --git-dir="$bare_repository" worktree add -B "$working_branch" "$workspace_path" "refs/remotes/origin/$working_branch"
else
  git --git-dir="$bare_repository" show-ref --verify --quiet "refs/remotes/origin/$base_branch"
  git --git-dir="$bare_repository" worktree add -b "$working_branch" "$workspace_path" "refs/remotes/origin/$base_branch"
fi

git -C "$workspace_path" config credential.helper '!f() { printf "%s\n" username=x-access-token; printf "password="; cat /run/kuayle-secrets/github-token; printf "\n"; }; f'
git config --global --add safe.directory "$workspace_path"
ln -sfn "$workspace_path" /workspace/current
