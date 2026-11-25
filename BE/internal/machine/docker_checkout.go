package machine

import (
	"context"
	"fmt"

	"github.com/kuayle/kuayle-backend/internal/domain"
)

// checkoutScript prepares a checkout, /workspace/tasks/<name>: a worktree of
// the machine's cache of the repository, /workspace/repos/<owner>/<repo>.git,
// which is made on first use. Preparing an existing checkout fetches again
// and keeps its work.
//
// Agent runs can write everything under /workspace, the configuration and
// hooks of the caches every worktree shares included, so git never runs in a
// cache or a worktree (KUA-299). As in publication, git runs in
// publishCheckoutScript's isolated git directory, which borrows the cache's
// objects, refs and worktree records and has its own empty configuration. The
// installation token (stdin) is used for one fetch of GitHub's branches from
// the fixed URL and removed before anything else runs: no URL rewrite, proxy,
// credential helper, hook, fsmonitor or filter an agent configured applies
// while it is at hand, and none runs afterwards either. What Kuayle records
// in the cache's configuration (origin, upstreams, the token-less credential
// helper people's IDE and terminals use) is written as data.
const checkoutScript = `set -eu
repository=$1 base=$2 branch=$3 path=$4 step=checkout
workspace_root=${KUAYLE_WORKSPACE_ROOT:-/workspace}
home_dir=${HOME:-/home/kuayle}
credential_helper=${KUAYLE_GIT_CREDENTIAL_HELPER:-$home_dir/.kuayle-git-credential}
if [ "${KUAYLE_CHECKOUT_ALLOW_ROOT_OVERRIDE:-0}" != "1" ]; then
  [ "$workspace_root" = /workspace ] || { echo "invalid workspace root" >&2; exit 1; }
fi
case "$workspace_root" in
  /* ) ;;
  * ) echo "invalid workspace root" >&2; exit 1 ;;
esac
case "$workspace_root" in /|*/|*..*|*//* ) echo "invalid workspace root" >&2; exit 1 ;; esac
case "$credential_helper" in /* ) ;; * ) echo "invalid credential helper path" >&2; exit 1 ;; esac
case "$repository" in
  [A-Za-z0-9-]*/[A-Za-z0-9._-]* ) ;;
  * ) echo "invalid repository" >&2; exit 1 ;;
esac
owner=${repository%%/*}
name=${repository#*/}
case "$owner" in ''|.*|*[!A-Za-z0-9-]* ) echo "invalid repository" >&2; exit 1 ;; esac
case "$name" in ''|.|..|*/*|*[!A-Za-z0-9._-]* ) echo "invalid repository" >&2; exit 1 ;; esac
tasks=$workspace_root/tasks
case "$path" in
  "$tasks"/* )
    case "$path" in *..*|*//*|"$tasks"/*/* ) echo "invalid workspace path" >&2; exit 1 ;; esac
    task=${path#"$tasks"/}
    case "$task" in ''|.|..|*[!A-Za-z0-9._-]* ) echo "invalid workspace path" >&2; exit 1 ;; esac
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
valid_ref "$base" && valid_ref "$branch" || { echo "invalid branch" >&2; exit 1; }
IFS= read -r token
[ -n "$token" ] || { echo "missing repository credential" >&2; exit 1; }
bare=$workspace_root/repos/$owner/$name.git
url=https://github.com/$repository

# People's IDE and terminals, never Kuayle, use this helper: it reads the
# GITHUB_TOKEN of their own environment and stores nothing.
mkdir -p "${credential_helper%/*}"
(umask 077; cat > "$credential_helper" <<'SCRIPT'
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
)
chmod 0700 "$credential_helper"
git config --global --add safe.directory "$path" </dev/null
` + isolatedGitEnvironmentScript + `if [ -e "$path" ]; then
  [ -e "$path/.git" ] || { echo "workspace path is not an existing checkout" >&2; exit 1; }
` + checkoutGitDirScript + `  [ "$(git config --file "$common/config" --get remote.origin.url </dev/null || true)" = "$url" ] || { echo "workspace path holds a checkout of another repository" >&2; exit 1; }
  existing=1
else
  if [ ! -d "$bare" ]; then
    mkdir -p "${bare%/*}"
    git -c init.defaultBranch="$base" init -q --bare "$bare" </dev/null
  fi
  gitdir=$bare common=$bare existing=
fi
` + isolatedGitDirScript + `record() { git config --file "$common/config" "$@" </dev/null; }
record --replace-all remote.origin.url "$url"
record --replace-all remote.origin.fetch '+refs/heads/*:refs/remotes/origin/*'
record --unset-all core.askPass || true
record --replace-all credential.helper "$credential_helper"

(umask 077; printf '%s' "$token" > "$work/token")
unset token
cat > "$work/askpass" <<'SCRIPT'
#!/bin/sh
case "$1" in
  *Username*) printf '%s\n' x-access-token ;;
  *) cat "$KUAYLE_CHECKOUT_TOKEN_FILE" ;;
esac
SCRIPT
chmod 0700 "$work/askpass"
GIT_ASKPASS="$work/askpass" KUAYLE_CHECKOUT_TOKEN_FILE="$work/token" \
  git --git-dir="$work/checkout" -c credential.helper= -c core.hooksPath=/dev/null \
  fetch -q --prune --no-recurse-submodules "$url" '+refs/heads/*:refs/remotes/origin/*' </dev/null
rm -f "$work/token" "$work/askpass"

if [ -z "$existing" ]; then
  # The worktree records are borrowed too, so git sees the cache's other
  # worktrees and records this one in the cache.
  mkdir -p "$common/worktrees"
  ln -s "$common/worktrees" "$work/checkout/worktrees"
  w() { git --git-dir="$work/checkout" -c core.hooksPath=/dev/null "$@" </dev/null; }
  w worktree prune
  mkdir -p "${path%/*}"
  # Start at the fetched tip when the local branch can fast-forward. Keep
  # local commits when the branch has diverged or the remote ref disappeared.
  local_ref=refs/heads/$branch remote_ref=refs/remotes/origin/$branch upstream=
  if w show-ref --verify --quiet "$remote_ref" &&
      { ! w show-ref --verify --quiet "$local_ref" || w merge-base --is-ancestor "$local_ref" "$remote_ref"; }; then
    w worktree add -q --no-checkout -B "$branch" "$path" "$remote_ref"
    upstream=$branch
  elif w show-ref --verify --quiet "$local_ref"; then
    w worktree add -q --no-checkout "$path" "$branch"
  else
    w show-ref --verify --quiet "refs/remotes/origin/$base" || { echo "base branch $base is not on GitHub" >&2; exit 1; }
    w worktree add -q --no-checkout -b "$branch" "$path" "refs/remotes/origin/$base"
    upstream=$base
  fi
  if [ -n "$upstream" ]; then
    record "branch.$branch.remote" origin
    record "branch.$branch.merge" "refs/heads/$upstream"
  fi
  # The worktree's .git names its record in the cache, not in $work.
  link=$(head -n 1 "$path/.git")
  gitdir=$common/worktrees/${link##*/}
  [ "$link" = "gitdir: $gitdir" ] || printf 'gitdir: %s\n' "$gitdir" > "$path/.git"
  # The files and the index are written as git worktree add would, through g.
  g reset -q --hard --no-recurse-submodules
fi
ln -sfn "$path" "$workspace_root/current"
`

// PrepareCheckout makes or refreshes a checkout in the developer container
// with the repository's installation token, which reaches only GitHub.
func (r *DockerRuntime) PrepareCheckout(ctx context.Context, _ *domain.DevMachine, services []domain.DevMachineService, checkout *domain.DevMachineCheckout, token string) error {
	developerContainer := developerContainerID(services)
	if developerContainer == "" {
		return fmt.Errorf("developer container is unavailable")
	}
	if token == "" {
		return fmt.Errorf("repository credential is unavailable")
	}
	return r.execWithInput(ctx, developerContainer, []string{"sh", "-c", checkoutScript, "kuayle-checkout",
		checkout.RepositoryFullName, checkout.BaseBranch, checkout.WorkingBranch, checkout.WorkspacePath}, token+"\n")
}
