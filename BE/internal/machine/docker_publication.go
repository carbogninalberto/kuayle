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
	Message     string
	Token       string
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
if [ -n "$(g status --porcelain --untracked-files=all)" ]; then
  g add -A
  g commit --no-verify -q -m "$message"
fi
if [ -z "$(g rev-list -n 1 "refs/remotes/origin/$base..HEAD")" ]; then echo none; exit 0; fi
g rev-parse --verify HEAD
`

// The push step copies only the recorded commit into a new temporary repository
// with no hooks or inherited configuration, then pushes it non-forcefully. The
// agent-writable checkout's configuration never runs while the token exists.
const publishPushScript = `set -eu
repository=$1 branch=$2 path=$3 commit=$4
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
GIT_ASKPASS="$work/askpass" KUAYLE_PUBLISH_TOKEN_FILE="$work/token" git --git-dir="$work/repo" -c credential.helper= -c core.hooksPath=/dev/null push -q "https://github.com/$repository" "$commit:refs/heads/$branch" </dev/null
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
	output, err := r.execInputOutput(ctx, developerContainer, []string{"sh", "-c", publishCommitScript, "kuayle-publish-commit",
		checkout.WorkspacePath, checkout.WorkingBranch, checkout.BaseBranch, request.AuthorName, request.AuthorEmail}, request.Message)
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
		checkout.RepositoryFullName, checkout.WorkingBranch, checkout.WorkspacePath, commit}, request.Token+"\n"); err != nil {
		return "", err
	}
	return commit, nil
}
