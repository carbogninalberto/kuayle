package machine

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// A planning step must leave its worktree as it found it. Kuayle decides that
// itself, the same way for every harness: it fingerprints the worktree before
// the agent starts and after it ends, and never asks the agent.

// worktreeStateScript prints "<HEAD commit> <tree>" for the worktree $1. The
// tree is written from a throwaway index holding HEAD plus everything in the
// worktree that git does not ignore, so uncommitted and untracked changes
// count and the checkout's real index is untouched. Kuayle's own files under
// .kuayle/ are excluded from the repository (excludeKuayleScript).
const worktreeStateScript = `set -eu
index=$(mktemp)
trap 'rm -f "$index"' EXIT
export GIT_INDEX_FILE="$index"
head=$(git -C "$1" rev-parse --verify HEAD)
git -C "$1" read-tree HEAD
git -C "$1" add -A
tree=$(git -C "$1" write-tree)
printf '%s %s\n' "$head" "$tree"`

var worktreeStatePattern = regexp.MustCompile(`^[0-9a-f]{40,64} [0-9a-f]{40,64}$`)

// maxWorktreeChangedFiles bounds the changed paths kept on a run.
const maxWorktreeChangedFiles = 100

// parseWorktreeState returns the fingerprint in a script's output, or "".
func parseWorktreeState(output string) string {
	lines := nonEmptyLines(output)
	if len(lines) == 0 {
		return ""
	}
	state := strings.TrimSpace(lines[len(lines)-1])
	if !worktreeStatePattern.MatchString(state) {
		return ""
	}
	return state
}

// worktreeChange compares two fingerprints: whether anything changed (a new
// commit counts even when the files are the same) and the trees to diff.
func worktreeChange(before, after string) (changed bool, fromTree, toTree string) {
	_, fromTree, _ = strings.Cut(before, " ")
	_, toTree, _ = strings.Cut(after, " ")
	return before != after, fromTree, toTree
}

// worktreeRuntime is implemented by runtimes that can fingerprint a worktree.
type worktreeRuntime interface {
	// WorktreeState fingerprints the checkout's worktree.
	WorktreeState(ctx context.Context, machine *domain.DevMachine, services []domain.DevMachineService, checkout *domain.DevMachineCheckout) (string, error)
	// WorktreeChangedFiles lists the paths that differ between two trees.
	WorktreeChangedFiles(ctx context.Context, services []domain.DevMachineService, checkout *domain.DevMachineCheckout, fromTree, toTree string) ([]string, error)
}

// worktreeStore keeps the fingerprint taken before a run started, so a
// manager that restarts while the agent runs still compares against it.
type worktreeStore interface {
	RecordWorktreeBefore(ctx context.Context, runID uuid.UUID, state string) (string, error)
}

func checkoutWorktreePath(checkout *domain.DevMachineCheckout) string {
	if checkout != nil {
		return checkout.WorkspacePath
	}
	return "/workspace"
}

func (r *DockerRuntime) WorktreeState(ctx context.Context, _ *domain.DevMachine, services []domain.DevMachineService, checkout *domain.DevMachineCheckout) (string, error) {
	developer := developerContainerID(services)
	if developer == "" {
		return "", fmt.Errorf("developer container is unavailable")
	}
	output, err := r.execOutput(ctx, developer, []string{"/bin/sh", "-c", worktreeStateScript, "kuayle-worktree", checkoutWorktreePath(checkout)})
	if err != nil {
		return "", err
	}
	state := parseWorktreeState(output)
	if state == "" {
		return "", fmt.Errorf("worktree state is unavailable")
	}
	return state, nil
}

func (r *DockerRuntime) WorktreeChangedFiles(ctx context.Context, services []domain.DevMachineService, checkout *domain.DevMachineCheckout, fromTree, toTree string) ([]string, error) {
	developer := developerContainerID(services)
	if developer == "" {
		return nil, fmt.Errorf("developer container is unavailable")
	}
	output, err := r.execOutput(ctx, developer, []string{"git", "-C", checkoutWorktreePath(checkout), "diff", "--name-only", "--no-renames", fromTree, toTree})
	if err != nil {
		return nil, err
	}
	return nonEmptyLines(output), nil
}

var _ worktreeRuntime = (*DockerRuntime)(nil)

// worktreeBefore fingerprints an unattended run's worktree before its agent
// starts; "" when it cannot. The first fingerprint of a run is the one kept.
func (m *Manager) worktreeBefore(ctx context.Context, machine *domain.DevMachine, run *domain.DevMachineAgentRun, services []domain.DevMachineService, checkout *domain.DevMachineCheckout) string {
	runtime, ok := m.runtime.(worktreeRuntime)
	if !ok || run.Mode == "interactive" {
		return ""
	}
	state, err := runtime.WorktreeState(ctx, machine, services, checkout)
	if err != nil {
		state = ""
	}
	store, ok := m.store.(worktreeStore)
	if !ok {
		return state
	}
	// An earlier start of this run (before a restart) already recorded it.
	kept, err := store.RecordWorktreeBefore(ctx, run.ID, state)
	if err != nil {
		return ""
	}
	return kept
}

// recordWorktreeChange sets on the run whether its agent changed the
// worktree and which files, or leaves it unknown.
func (m *Manager) recordWorktreeChange(ctx context.Context, machine *domain.DevMachine, run *domain.DevMachineAgentRun, services []domain.DevMachineService, checkout *domain.DevMachineCheckout, before string) {
	run.WorktreeChanged, run.WorktreeChangedFiles = nil, []byte(`[]`)
	runtime, ok := m.runtime.(worktreeRuntime)
	if !ok || before == "" {
		return
	}
	after, err := runtime.WorktreeState(ctx, machine, services, checkout)
	if err != nil {
		return
	}
	changed, fromTree, toTree := worktreeChange(before, after)
	run.WorktreeChanged = &changed
	if !changed || fromTree == toTree {
		return
	}
	files, err := runtime.WorktreeChangedFiles(ctx, services, checkout, fromTree, toTree)
	if err != nil {
		return
	}
	if len(files) > maxWorktreeChangedFiles {
		files = files[:maxWorktreeChangedFiles]
	}
	if encoded, err := json.Marshal(files); err == nil {
		run.WorktreeChangedFiles = encoded
	}
}
