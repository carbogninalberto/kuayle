package machine

import (
	"context"
	_ "embed"
	"errors"
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
	// Lease is the GitHub tip SyncCheckout saw when the agent had rewritten the
	// published commit. When set, the push is guarded by force-with-lease.
	Lease           string
	IssueIdentifier string
	Message         string
	Token           string
}

var (
	publishRepositoryPattern = regexp.MustCompile(`^[A-Za-z0-9-]+/[A-Za-z0-9._-]+$`)
	publishCommitPattern     = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)
)

// ErrPublishedWorkRemains is PublishCheckout's answer when the branch has
// nothing new to publish, but the task branch on GitHub still has changes
// against its base: work an earlier iteration published that the agent has
// since removed. Nothing is pushed, so that work stays on the branch and in
// its pull request (KUA-176).
var ErrPublishedWorkRemains = errors.New("the task branch on GitHub still holds work the agent removed")

// publishCheckoutScript opens the task checkout without loading anything the
// agent can configure. Agent runs write the checkout's git directory, so its
// config, hooks and info/attributes can name programs (filter, diff and merge
// drivers, hooks read from core.hooksPath or defined in config, fsmonitor,
// credential helpers, ssh and proxy commands, pagers, editors, signing
// programs, URL rewrites to the ext transport), and no command-line override
// covers them all. Git therefore never runs in that directory. $work/checkout is
// a new git directory with its own empty config and no hooks that borrows only
// the checkout's objects, refs and ignore patterns; g runs git there against
// the checkout's working tree and index. In-tree .gitattributes still apply,
// but the drivers they name are undefined, so content is committed and checked
// out as it is. System and global configuration are ignored and only the file
// and https transports are allowed.
//
// It expects $path, $branch and $step, and leaves $work (removed on exit),
// $gitdir (the checkout's own git directory, read as plain files only) and g.
const publishCheckoutScript = `export GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL=/dev/null GIT_TERMINAL_PROMPT=0
export GIT_ALLOW_PROTOCOL=file:https GIT_NO_LAZY_FETCH=1
work=$(mktemp -d "/tmp/kuayle-$step.XXXXXX")
trap 'rm -rf "$work"' EXIT
trap 'rm -rf "$work"; exit 130' INT
trap 'rm -rf "$work"; exit 143' TERM
cd "$work"
gitdir=$path/.git
if [ -f "$gitdir" ]; then
  link=$(head -n 1 "$gitdir")
  case $link in "gitdir: "*) link=${link#gitdir: } ;; *) echo "checkout is not a git repository" >&2; exit 1 ;; esac
  case $link in /*) gitdir=$link ;; *) gitdir=$path/$link ;; esac
fi
common=$gitdir
if [ -f "$gitdir/commondir" ]; then
  link=$(head -n 1 "$gitdir/commondir")
  case $link in /*) common=$link ;; *) common=$gitdir/$link ;; esac
fi
if [ ! -f "$gitdir/HEAD" ] || [ ! -d "$common/objects" ] || [ ! -d "$common/refs" ]; then echo "checkout is not a git repository" >&2; exit 1; fi
if [ -e "$common/reftable" ]; then echo "checkout uses an unsupported ref storage" >&2; exit 1; fi
# The object format is read from the checkout's config as data; nothing in that
# file is applied.
case $(git config --file "$common/config" --get extensions.objectformat </dev/null || true) in
  sha256) git init -q --bare --object-format=sha256 "$work/checkout" ;;
  *) git init -q --bare "$work/checkout" ;;
esac
rm -rf "$work/checkout/objects" "$work/checkout/refs" "$work/checkout/info/exclude"
mkdir -p "$work/checkout/info"
ln -s "$common/objects" "$work/checkout/objects"
ln -s "$common/refs" "$work/checkout/refs"
ln -s "$common/packed-refs" "$work/checkout/packed-refs"
ln -s "$common/shallow" "$work/checkout/shallow"
ln -s "$common/info/exclude" "$work/checkout/info/exclude"
printf 'ref: refs/heads/%s\n' "$branch" > "$work/checkout/HEAD"
# Housekeeping here would not see the repository's other worktrees.
git --git-dir="$work/checkout" config gc.auto 0
git --git-dir="$work/checkout" config maintenance.auto false
# A split index would leave its shared part in this directory, which is removed.
git --git-dir="$work/checkout" config core.splitIndex false
# Listing the refs of a repository the checkout names as an alternate would run
# git under that repository's configuration.
git --git-dir="$work/checkout" config core.alternateRefsCommand :
gi() { GIT_INDEX_FILE="$gitdir/index" git -C "$path" --git-dir="$work/checkout" --work-tree="$path" "$@"; }
g() { gi "$@" </dev/null; }
on_task_branch() { [ "$(head -n 1 "$gitdir/HEAD")" = "ref: refs/heads/$branch" ] || { echo "checkout is not on the task branch" >&2; exit 1; }; }
`

const publishStageScript = `
# Staging compares a nested repository by running git inside it, under that
# repository's own configuration. Nested repositories are left out of that
# comparison and recorded afterwards at their current commit, which reads
# their HEAD without running anything in them.
nested() { g ls-files -s | grep '^160000 [0-9a-f]* 0' | cut -f 2- || true; }
stage_changes() {
  nested | gi update-index --skip-worktree --stdin
  g -c advice.addEmbeddedRepo=false add -A
  nested | gi update-index --no-skip-worktree --stdin
  g diff-files --ignore-submodules=dirty --name-only -z | gi update-index --add --remove -z --stdin
}
link_merge_state() {
  for state in MERGE_HEAD MERGE_MODE MERGE_MSG; do
    [ ! -f "$gitdir/$state" ] || ln -s "$gitdir/$state" "$work/checkout/$state"
  done
}
commit_changes() {
  g commit --no-verify -q -m "$message"
  rm -f "$gitdir/MERGE_HEAD" "$gitdir/MERGE_MODE" "$gitdir/MERGE_MSG"
}
`

// The commit step runs without any credential and through publishCheckoutScript,
// so no program named by the checkout's configuration, attributes or nested
// repositories runs; the agent's identity claims are replaced.
const publishCommitScript = `set -eu
path=$1 branch=$2 base=$3 step=commit
export GIT_AUTHOR_NAME="$4" GIT_AUTHOR_EMAIL="$5" GIT_COMMITTER_NAME="$4" GIT_COMMITTER_EMAIL="$5"
message=$(cat)
[ -n "$message" ] || { echo "missing commit message" >&2; exit 1; }
` + publishCheckoutScript + publishStageScript + `on_task_branch
# A merge the agent left uncommitted is concluded as it would be in the checkout.
link_merge_state
stage_changes
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
  commit_changes
fi
g rev-parse --verify HEAD
`

//go:embed publication_migrations.cjs
var publicationMigrationsScript string

// The push step copies only the recorded commit into a new temporary repository
// with no hooks or inherited configuration, then pushes it. It prints none and
// pushes nothing when the commit has nothing to publish because GitHub's base
// already has its work, also when that work was squashed. It prints remains
// instead when GitHub's task branch still differs from the base, so it holds work
// an earlier iteration published that the agent has since removed. With the lease
// of a rewrite the sync accepted, the push is usually a fast-forward, because the
// commit step rebuilt the changes on the published commit. It is a real force
// only when the published commit is missing from the checkout, and then every
// commit it drops must be Kuayle's own. The lease is checked against GitHub's
// tip at push time, so a commit that landed in between is never discarded. The
// commit is read through publishCheckoutScript, so the agent-writable
// checkout's configuration is never loaded, with or without the token.
var publishPushScript = `set -eu
repository=$1 branch=$2 path=$3 commit=$4 base=$5 email=$6 lease=${7:-} step=publish
umask 077
` + publishCheckoutScript + `git init -q --bare "$work/repo"
git --git-dir="$work/repo" fetch -q --no-tags --no-recurse-submodules "$work/checkout" "+refs/heads/$branch:refs/kuayle/publish" </dev/null
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
# carry no co-author trailers. The range is every commit not reachable from the
# base and task-branch tips fetched from GitHub, so a forced push's rewritten
# commits are checked as well, not only the commits beyond them. The commit step
# trusted the checkout's remote-tracking refs to keep base and task-branch
# commits; here they come from GitHub itself.
remote fetch -q --no-tags --no-recurse-submodules "$url" "+refs/heads/$base:refs/kuayle/remote/base"
# A branch that cannot be read (a network or HTTP error, a refused credential) is
# not a branch that does not exist: the publication stops instead of counting it
# as no branch and reporting that there is nothing to publish.
heads=$(remote ls-remote --heads "$url" "refs/heads/$branch") || { echo "could not read the task branch on GitHub" >&2; exit 1; }
if printf '%s\n' "$heads" | cut -f 2 | grep -qxF "refs/heads/$branch"; then
  remote fetch -q --no-tags --no-recurse-submodules "$url" "+refs/heads/$branch:refs/kuayle/remote/branch"
fi
# unchanged <commit> succeeds when merging the commit into the base as it is on
# GitHub leaves the base's tree as it is, so the commit has nothing to publish:
# its work is already in the base, also when it was squashed (a diff from the
# merge base would still show it), with or without the base merged back. A
# conflict or any other error counts as a change, so the push goes ahead.
# Git before 2.38 has no merge-tree --write-tree and exits with a usage error
# (129); the commit is then compared with its merge base with the base, which
# misses squashed work.
unchanged() {
  status=0
  tree=$(git --git-dir="$work/repo" merge-tree --write-tree --no-messages refs/kuayle/remote/base "$1" </dev/null 2>/dev/null) || status=$?
  case $status in
    0) [ "$tree" = "$(git --git-dir="$work/repo" rev-parse --verify 'refs/kuayle/remote/base^{tree}' </dev/null)" ] ;;
    129) git --git-dir="$work/repo" diff --quiet --no-ext-diff --no-textconv "refs/kuayle/remote/base...$1" </dev/null 2>/dev/null ;;
    *) return 1 ;;
  esac
}
if unchanged "$commit"; then
  # Nothing new to publish. When GitHub's task branch still differs from the
  # base, it holds work an earlier iteration published (in its pull request,
  # if one is open) that the agent has since removed. Not pushing leaves that
  # work published, so the script says so and the task is blocked instead of
  # ended (KUA-176).
  if git --git-dir="$work/repo" rev-parse -q --verify refs/kuayle/remote/branch >/dev/null </dev/null && ! unchanged refs/kuayle/remote/branch; then
    echo remains
  else
    echo none
  fi
  exit 0
fi
authors=$(git --git-dir="$work/repo" log --format='%ae%n%ce' "$commit" --not --glob='refs/kuayle/remote/*')
messages=$(git --git-dir="$work/repo" log --format=%B "$commit" --not --glob='refs/kuayle/remote/*')
if printf '%s\n' "$authors" | grep -vxF -- "$email" | grep -q . || printf '%s\n' "$messages" | grep -qi '^co-authored-by:'; then
  echo "publication adds commits by another author" >&2; exit 1
fi
# Compare against the freshly fetched base before publishing. The helper
# works on Git objects alone and never receives the repository credential.
renumbered=
if [ -n "$(git --git-dir="$work/repo" ls-tree -d refs/kuayle/remote/base -- BE/migrations)" ] &&
   [ -n "$(git --git-dir="$work/repo" ls-tree -d "$commit" -- BE/migrations)" ]; then
  if ! command -v node >/dev/null 2>&1; then
    printf 'blocked\nPublication needs Node.js in the developer container to check BE/migrations against the latest base. Use a developer image with Node.js, then retry publication.\n'
    exit 0
  fi
  renumbered=$(GIT_AUTHOR_NAME="${8:-Kuayle Agent}" GIT_COMMITTER_NAME="${8:-Kuayle Agent}" \
    GIT_AUTHOR_EMAIL="$email" GIT_COMMITTER_EMAIL="$email" env -u NODE_OPTIONS -u NODE_PATH \
    node - "$work/repo" refs/kuayle/remote/base "$commit" "${9:-}" <<'KUAYLE_MIGRATIONS'
` + publicationMigrationsScript + `KUAYLE_MIGRATIONS
)
  case $renumbered in
    blocked*) printf '%s\n' "$renumbered"; exit 0 ;;
    "") ;;
    *) commit=$renumbered ;;
  esac
fi
# The bare repository has no remote-tracking ref, so the expected tip is named
# explicitly. It is the tip the sync saw; if the branch moved since then, GitHub
# refuses the push instead of losing that commit. A force can also replace
# commits that are on the branch today: the commit step usually rebuilt on the
# published commit, so this is a fast-forward, but when it could not (the
# published commit is missing from the checkout) it rebuilt from the merge base
# and this push really drops commits. Every dropped commit must then be Kuayle's
# own, or the push loses someone else's work; the lease commit is present here
# because the branch fetched above carries it, and when GitHub no longer has it
# the expected tip cannot match anyway.
if [ -n "$lease" ]; then
  if git --git-dir="$work/repo" cat-file -e "$lease^{commit}" 2>/dev/null &&
    ! git --git-dir="$work/repo" merge-base --is-ancestor "$lease" "$commit"; then
    dropped=$(git --git-dir="$work/repo" log --format='%ae%n%ce' "$lease" --not "$commit" refs/kuayle/remote/base)
    if printf '%s\n' "$dropped" | grep -vxF -- "$email" | grep -q .; then
      echo "the push would drop commits someone else pushed to the task branch; nothing was force-pushed" >&2; exit 1
    fi
  fi
  remote push -q --force-with-lease="refs/heads/$branch:$lease" "$url" "$commit:refs/heads/$branch"
else
  remote push -q "$url" "$commit:refs/heads/$branch"
fi
[ -z "$renumbered" ] || printf 'renumbered %s\n' "$commit"
`

// PublishCheckout commits pending task changes and pushes the task branch. It
// returns the pushed commit, or "" when the push step found nothing to publish
// against the base as it is on GitHub (for example work that was already
// merged). Nothing is pushed then. It returns ErrPublishedWorkRemains when the
// commit has nothing new to publish but the task branch on GitHub still has
// changes against the base, because it holds work an earlier iteration published
// that the agent has since removed: nothing is pushed then either, so that work
// stays on the branch and in its pull request.
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
	if request.Lease != "" && !publishCommitPattern.MatchString(request.Lease) {
		return "", fmt.Errorf("invalid publication lease")
	}
	output, err := r.execInputOutput(ctx, developerContainer, []string{"sh", "-c", publishCommitScript, "kuayle-publish-commit",
		checkout.WorkspacePath, checkout.WorkingBranch, checkout.BaseBranch, request.AuthorName, request.AuthorEmail, request.PublishedCommit}, request.Message)
	if err != nil {
		return "", err
	}
	commit := strings.TrimSpace(output)
	if !publishCommitPattern.MatchString(commit) {
		return "", fmt.Errorf("publication returned an invalid commit")
	}
	pushed, err := r.execInputOutput(ctx, developerContainer, []string{"sh", "-c", publishPushScript, "kuayle-publish-push",
		checkout.RepositoryFullName, checkout.WorkingBranch, checkout.WorkspacePath, commit, checkout.BaseBranch, request.AuthorEmail, request.Lease, request.AuthorName, request.IssueIdentifier}, request.Token+"\n")
	if err != nil {
		return "", err
	}
	return pushedCommit(pushed, commit)
}

// pushedCommit reads the push step's stdout: none when the commit had nothing
// to publish, remains when GitHub's task branch still holds removed work, and
// nothing when the original commit was pushed. A renumbering reports the
// commit actually pushed or a reason the agent must repair the checkout.
func pushedCommit(output, commit string) (string, error) {
	output = strings.TrimSpace(output)
	switch output {
	case "none":
		return "", nil
	case "remains":
		return "", ErrPublishedWorkRemains
	}
	if detail, ok := strings.CutPrefix(output, "blocked\n"); ok {
		return "", &checkoutSyncBlockedError{detail: detail}
	}
	if head, ok := strings.CutPrefix(output, "renumbered "); ok {
		if !publishCommitPattern.MatchString(head) {
			return "", fmt.Errorf("publication returned an invalid renumbered commit")
		}
		return head, nil
	}
	return commit, nil
}

// The sync step refreshes the base tracking ref before an agent visit or
// publication, and brings the checkout up to the task branch on GitHub:
// commits others pushed (a maintainer merging the base branch into
// the pull request) are fetched and fast-forwarded to, never overwritten. If
// pending work prevents a fast-forward, it is committed and then merged with
// the fetched tip. A conflict leaves that commit and the merge in the checkout
// for the next attempt to resolve. When the histories diverged because the
// agent rewrote the commits Kuayle published
// itself, GitHub's tip is still exactly the commit Kuayle recorded as published
// ($4). Nobody else pushed then, so the checkout is left alone and the tip is
// printed as the lease for a guarded force push. Any other divergence is
// refused. The credential is used only in a fresh bare repository. The checkout
// is updated from that repository through publishCheckoutScript, after the
// credential is gone, so no program named by its configuration or attributes
// runs.
const publishSyncScript = `set -eu
repository=$1 branch=$2 path=$3 published=$4 base=$8 step=sync
export GIT_AUTHOR_NAME="$5" GIT_AUTHOR_EMAIL="$6" GIT_COMMITTER_NAME="$5" GIT_COMMITTER_EMAIL="$6"
message=$7
umask 077
` + publishCheckoutScript + publishStageScript + `on_task_branch
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
set -- "+refs/heads/$base:refs/kuayle/base"
# The base is fetched even before the task's first push.
if remote ls-remote --exit-code --heads "$url" "refs/heads/$branch" >/dev/null; then
  set -- "$@" "+refs/heads/$branch:refs/kuayle/branch"
  fetched=1
else
  status=$?
  [ "$status" -eq 2 ] || exit "$status"
  fetched=
fi
remote fetch -q --no-tags --no-recurse-submodules "$url" "$@"
rm -f "$work/token" "$work/askpass"
g fetch -q --no-tags --no-recurse-submodules "$work/repo" "+refs/kuayle/base:refs/remotes/origin/$base"
block_checkout() { printf 'blocked\n%s\n' "$1"; exit 0; }
check_conflicts() {
  files=$(g -c core.quotePath=false diff --cached --name-only --diff-filter=U)
  [ -z "$files" ] || { printf 'conflicts\n%s\n' "$files"; exit 0; }
}
check_conflicts
[ -n "$fetched" ] || exit 0
g fetch -q --no-tags --no-recurse-submodules "$work/repo" "+refs/kuayle/branch:refs/remotes/origin/$branch"
tip=$(g rev-parse --verify "refs/remotes/origin/$branch^{commit}")
if [ -f "$gitdir/MERGE_HEAD" ] && [ "$(cat "$gitdir/MERGE_HEAD")" = "$tip" ]; then
  # The agent staged the resolutions. A merge that keeps the checkpoint's
  # tree still needs to record the GitHub tip as a parent.
  link_merge_state
  stage_changes
  commit_changes
  exit 0
fi
g merge-base --is-ancestor "$tip" HEAD && exit 0
if g merge-base --is-ancestor HEAD "$tip"; then
  [ ! -f "$gitdir/MERGE_HEAD" ] || block_checkout "Finish the in-progress merge before bringing in commits from the GitHub task branch."
  stage_changes
  if g diff --cached --quiet; then
    g merge -q --ff-only --no-overwrite-ignore "$tip" >"$work/merge-output" 2>&1 ||
      block_checkout "Could not fast-forward to the GitHub task branch. $(cat "$work/merge-output")"
    exit 0
  fi
  # Keep a durable copy before the merge touches the working tree.
  g commit --no-verify -q -m "$message"
  if ! g merge -q --no-ff --no-overwrite-ignore -m "$message" "$tip" >"$work/merge-output" 2>&1; then
    if [ -f "$work/checkout/MERGE_HEAD" ]; then
      # Keep the standard merge state after the isolated git directory goes away.
      for state in ORIG_HEAD MERGE_MODE MERGE_MSG MERGE_HEAD; do
        mv -f "$work/checkout/$state" "$gitdir/"
      done
    fi
    check_conflicts
    block_checkout "Could not merge the GitHub task branch into the saved changes. $(cat "$work/merge-output")"
  fi
  exit 0
fi
if [ -n "$published" ] && [ "$tip" = "$published" ]; then
  # GitHub's tip is the commit Kuayle published last and nobody else pushed, so
  # the divergence is the agent's own rewrite: leave the checkout for the commit
  # step and report the expected tip for the push's lease.
  echo "lease $tip"
  exit 0
fi
block_checkout "The task branch on GitHub and the checkout have diverged. Bring the GitHub branch's commits into the checkout before publishing."
`

// SyncCheckout brings commits pushed to the task branch on GitHub into the
// checkout while preserving pending changes.
// When the checkout rewrote the commits Kuayle published and GitHub's tip is
// still the commit Kuayle recorded as published, nobody else pushed: it returns
// that tip as the lease for a guarded force push instead of changing the
// checkout.
func (r *DockerRuntime) SyncCheckout(ctx context.Context, _ *domain.DevMachine, services []domain.DevMachineService, checkout *domain.DevMachineCheckout, request PublishRequest) (string, error) {
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
	output, err := r.execInputOutput(ctx, developerContainer, []string{"sh", "-c", publishSyncScript, "kuayle-publish-sync",
		checkout.RepositoryFullName, checkout.WorkingBranch, checkout.WorkspacePath, request.PublishedCommit,
		request.AuthorName, request.AuthorEmail, request.Message, checkout.BaseBranch}, request.Token+"\n")
	if err != nil {
		return "", err
	}
	return parseSyncLease(output)
}

// A checkout block requires changes to the worktree before it can be retried.
type checkoutSyncBlockedError struct{ detail string }

func (e *checkoutSyncBlockedError) Error() string { return e.detail }

// parseSyncLease reads the sync step's stdout: nothing when no lease is needed,
// the lease of an accepted rewrite, or the reason the checkout is blocked.
func parseSyncLease(output string) (string, error) {
	output = strings.TrimSpace(output)
	if output == "" {
		return "", nil
	}
	if files, ok := strings.CutPrefix(output, "conflicts\n"); ok {
		return "", &checkoutSyncBlockedError{detail: "Merge conflicts in " + strings.ReplaceAll(files, "\n", ", ")}
	}
	if detail, ok := strings.CutPrefix(output, "blocked\n"); ok {
		return "", &checkoutSyncBlockedError{detail: detail}
	}
	lease, ok := strings.CutPrefix(output, "lease ")
	if !ok || !publishCommitPattern.MatchString(lease) {
		return "", fmt.Errorf("checkout sync returned an unexpected result")
	}
	return lease, nil
}
