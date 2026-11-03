package machine

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/kuayle/kuayle-backend/internal/domain"
)

// PublishRequest carries manager-held publication authority. The token is never
// written into the checkout, environment or git configuration of the machine.
type PublishRequest struct {
	AuthorName  string
	AuthorEmail string
	// The last commit Kuayle published on this branch; commits up to it are kept.
	PublishedCommit string
	Message         string
	Token           string
}

var (
	publishRepositoryPattern = regexp.MustCompile(`^[A-Za-z0-9-]+/[A-Za-z0-9._-]+$`)
	publishCommitPattern     = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)
)

// The commit step runs without any credential. Repository-local hooks, fsmonitor
// and signing programs are disabled; the agent's identity claims are replaced.
const publishCommitScript = `set -eu
path=$1 branch=$2 base=$3
export GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL=/dev/null GIT_TERMINAL_PROMPT=0
export GIT_AUTHOR_NAME="$4" GIT_AUTHOR_EMAIL="$5" GIT_COMMITTER_NAME="$4" GIT_COMMITTER_EMAIL="$5"
g() { git -C "$path" -c core.hooksPath=/dev/null -c core.fsmonitor=false -c commit.gpgSign=false "$@" </dev/null; }
message=$(cat)
[ -n "$message" ] || { echo "missing commit message" >&2; exit 1; }
[ "$(g symbolic-ref --short HEAD)" = "$branch" ] || { echo "checkout is not on the task branch" >&2; exit 1; }
g add -A
# Commits made since the last publication by anyone but the configured author,
# or carrying co-author trailers, are folded into one commit by that author.
# The floor is the commit Kuayle recorded as published ($6), never a marker in
# the agent-writable checkout, so commits already on GitHub are never rewritten.
# Kuayle's own commits are kept, so a retry republishes the same commit.
# A history rewritten below the published commit is rebuilt on top of it with
# the checkout's current tree, so the push stays a fast-forward; a newer base
# it was rebased onto becomes a merge parent.
floor=${6:-} recorded= rebuilt=
# The checkout's own head, kept so a rebase onto a newer base still records
# that base when it is rebuilt on the published commit.
head=$(g rev-parse --verify HEAD)
if [ -n "$floor" ] && g cat-file -e "$floor^{commit}" 2>/dev/null; then
  recorded=1
  g merge-base --is-ancestor "$floor" HEAD || { g reset -q --soft "$floor"; rebuilt=1; }
else
  floor=$(g merge-base HEAD "refs/remotes/origin/$base")
fi
# Commits people pushed to the task branch on GitHub (fetched into the
# checkout) are kept as well, and base-branch commits merged into the task are
# never judged or folded. Those refs are agent-writable, so the push step
# re-checks every commit it adds against the branches fetched from GitHub.
remote="refs/remotes/origin/$branch"
if g rev-parse -q --verify "$remote^{commit}" >/dev/null && g merge-base --is-ancestor "$remote" HEAD &&
  { [ -z "$recorded" ] || g merge-base --is-ancestor "$floor" "$remote"; }; then
  floor=$(g rev-parse --verify "$remote^{commit}")
fi
tip=$(g rev-parse --verify "refs/remotes/origin/$base^{commit}")
foreign=$(g log --format='%ae%n%ce' "$floor..HEAD" --not "$tip" | grep -vxF -- "$5" | head -n 1 || true)
if [ -n "$rebuilt" ] || [ -n "$foreign" ] || g log --format=%B "$floor..HEAD" --not "$tip" | grep -qi '^co-authored-by:'; then
  # A merge of the base branch stays a merge, so the pull request is neither
  # shown behind its base nor re-adds the base's commits.
  merged=$tip
  g merge-base --is-ancestor "$tip" "$head" || merged=$(g merge-base "$head" "$tip" || true)
  if [ -n "$merged" ] && ! g merge-base --is-ancestor "$merged" "$floor"; then
    folded=$(g commit-tree --no-gpg-sign -p "$floor" -p "$merged" -m "$message" "$(g write-tree)")
    g reset -q --soft "$folded"
  else
    g reset -q --soft "$floor"
  fi
fi
if ! g diff --cached --quiet; then
  g commit --no-verify -q -m "$message"
fi
if [ -z "$(g rev-list -n 1 "refs/remotes/origin/$base..HEAD")" ]; then echo none; exit 0; fi
g rev-parse --verify HEAD
`

// The push step copies only the recorded commit into a new temporary repository
// with no hooks or inherited configuration, then pushes it non-forcefully. The
// agent-writable checkout's configuration never runs while the token exists.
const publishPushScript = `set -eu
repository=$1 branch=$2 path=$3 commit=$4 base=$5 email=$6
export GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL=/dev/null GIT_TERMINAL_PROMPT=0
umask 077
work=$(mktemp -d /tmp/kuayle-publish.XXXXXX)
trap 'rm -rf "$work"' EXIT
trap 'rm -rf "$work"; exit 130' INT
trap 'rm -rf "$work"; exit 143' TERM
git init -q --bare "$work/repo"
git --git-dir="$work/repo" fetch -q --no-tags "$path" "+refs/heads/$branch:refs/kuayle/publish" </dev/null
[ "$(git --git-dir="$work/repo" rev-parse --verify refs/kuayle/publish)" = "$commit" ] || { echo "task branch moved before publishing" >&2; exit 1; }
IFS= read -r token
[ -n "$token" ] || { echo "missing repository credential" >&2; exit 1; }
printf '%s' "$token" > "$work/token"
unset token
cat > "$work/askpass" <<'SCRIPT'
#!/bin/sh
case "$1" in
  *Username*) printf '%s\n' x-access-token ;;
  *) cat "$KUAYLE_PUBLISH_TOKEN_FILE" ;;
esac
SCRIPT
chmod 0700 "$work/askpass"
remote() { GIT_ASKPASS="$work/askpass" KUAYLE_PUBLISH_TOKEN_FILE="$work/token" git --git-dir="$work/repo" -c credential.helper= -c core.hooksPath=/dev/null "$@" </dev/null; }
url="https://github.com/$repository"
# Every commit this push adds to GitHub must be by the configured author and
# carry no co-author trailers. The commit step trusted the checkout's
# remote-tracking refs to keep base and task-branch commits; here they come from
# GitHub itself.
remote fetch -q --no-tags "$url" "+refs/heads/$base:refs/kuayle/remote/base"
if remote ls-remote --exit-code --heads "$url" "refs/heads/$branch" >/dev/null; then
  remote fetch -q --no-tags "$url" "+refs/heads/$branch:refs/kuayle/remote/branch"
fi
authors=$(git --git-dir="$work/repo" log --format='%ae%n%ce' "$commit" --not --glob='refs/kuayle/remote/*')
messages=$(git --git-dir="$work/repo" log --format=%B "$commit" --not --glob='refs/kuayle/remote/*')
if printf '%s\n' "$authors" | grep -vxF -- "$email" | grep -q . || printf '%s\n' "$messages" | grep -qi '^co-authored-by:'; then
  echo "publication adds commits by another author" >&2; exit 1
fi
remote push -q "$url" "$commit:refs/heads/$branch"
`

// PublishCheckout commits pending task changes and pushes the task branch. It
// returns the pushed commit, or "" when the branch has no changes to publish.
func (r *DockerRuntime) PublishCheckout(ctx context.Context, _ *domain.DevMachine, services []domain.DevMachineService, checkout *domain.DevMachineCheckout, request PublishRequest) (string, error) {
	developerContainer := developerContainerID(services)
	if developerContainer == "" {
		return "", fmt.Errorf("developer container is unavailable")
	}
	if checkout == nil || !publishRepositoryPattern.MatchString(checkout.RepositoryFullName) {
		return "", fmt.Errorf("invalid publication repository")
	}
	if request.Token == "" {
		return "", fmt.Errorf("repository credential is unavailable")
	}
	if request.PublishedCommit != "" && !publishCommitPattern.MatchString(request.PublishedCommit) {
		return "", fmt.Errorf("invalid published commit")
	}
	output, err := r.execInputOutput(ctx, developerContainer, []string{"sh", "-c", publishCommitScript, "kuayle-publish-commit",
		checkout.WorkspacePath, checkout.WorkingBranch, checkout.BaseBranch, request.AuthorName, request.AuthorEmail, request.PublishedCommit}, request.Message)
	if err != nil {
		return "", err
	}
	commit := strings.TrimSpace(output)
	if commit == "none" {
		return "", nil
	}
	if !publishCommitPattern.MatchString(commit) {
		return "", fmt.Errorf("publication returned an invalid commit")
	}
	if _, err = r.execInputOutput(ctx, developerContainer, []string{"sh", "-c", publishPushScript, "kuayle-publish-push",
		checkout.RepositoryFullName, checkout.WorkingBranch, checkout.WorkspacePath, commit, checkout.BaseBranch, request.AuthorEmail}, request.Token+"\n"); err != nil {
		return "", err
	}
	return commit, nil
}

// The sync step brings the checkout up to the task branch on GitHub before it is
// published: commits others pushed (a maintainer merging the base branch into
// the pull request) are fetched and fast-forwarded to, never overwritten. The
// credential is used only in a fresh bare repository; the checkout, which the
// agent can configure, fetches from that repository without a credential.
const publishSyncScript = `set -eu
repository=$1 branch=$2 path=$3
export GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL=/dev/null GIT_TERMINAL_PROMPT=0
umask 077
work=$(mktemp -d /tmp/kuayle-sync.XXXXXX)
trap 'rm -rf "$work"' EXIT
trap 'rm -rf "$work"; exit 130' INT
trap 'rm -rf "$work"; exit 143' TERM
IFS= read -r token
[ -n "$token" ] || { echo "missing repository credential" >&2; exit 1; }
printf '%s' "$token" > "$work/token"
unset token
cat > "$work/askpass" <<'SCRIPT'
#!/bin/sh
case "$1" in
  *Username*) printf '%s\n' x-access-token ;;
  *) cat "$KUAYLE_PUBLISH_TOKEN_FILE" ;;
esac
SCRIPT
chmod 0700 "$work/askpass"
git init -q --bare "$work/repo"
remote() { GIT_ASKPASS="$work/askpass" KUAYLE_PUBLISH_TOKEN_FILE="$work/token" git --git-dir="$work/repo" -c credential.helper= -c core.hooksPath=/dev/null "$@" </dev/null; }
url="https://github.com/$repository"
# A branch that is not on GitHub yet has nothing to bring in.
remote ls-remote --exit-code --heads "$url" "refs/heads/$branch" >/dev/null || exit 0
remote fetch -q --no-tags "$url" "+refs/heads/$branch:refs/kuayle/branch"
g() { git -C "$path" -c core.hooksPath=/dev/null -c core.fsmonitor=false "$@" </dev/null; }
[ "$(g symbolic-ref --short HEAD)" = "$branch" ] || { echo "checkout is not on the task branch" >&2; exit 1; }
g fetch -q --no-tags "$work/repo" "+refs/kuayle/branch:refs/remotes/origin/$branch"
tip=$(g rev-parse --verify "refs/remotes/origin/$branch^{commit}")
g merge-base --is-ancestor "$tip" HEAD && exit 0
if g merge-base --is-ancestor HEAD "$tip"; then
  g merge -q --ff-only "$tip" || { echo "could not fast-forward the checkout to commits pushed to GitHub; resolve it by hand, nothing was force-pushed" >&2; exit 1; }
  exit 0
fi
echo "the task branch on GitHub has commits the checkout does not have and the checkout has commits GitHub does not have; it cannot be fast-forwarded and nothing was force-pushed" >&2
exit 1
`

// SyncCheckout fast-forwards the task checkout to the task branch on GitHub. It
// fails, leaving the checkout alone, when the histories diverged.
func (r *DockerRuntime) SyncCheckout(ctx context.Context, _ *domain.DevMachine, services []domain.DevMachineService, checkout *domain.DevMachineCheckout, token string) error {
	developerContainer := developerContainerID(services)
	if developerContainer == "" {
		return fmt.Errorf("developer container is unavailable")
	}
	if checkout == nil || !publishRepositoryPattern.MatchString(checkout.RepositoryFullName) {
		return fmt.Errorf("invalid publication repository")
	}
	if token == "" {
		return fmt.Errorf("repository credential is unavailable")
	}
	_, err := r.execInputOutput(ctx, developerContainer, []string{"sh", "-c", publishSyncScript, "kuayle-publish-sync",
		checkout.RepositoryFullName, checkout.WorkingBranch, checkout.WorkspacePath}, token+"\n")
	return err
}
