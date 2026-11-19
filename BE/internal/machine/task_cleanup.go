package machine

import (
	"context"
	"fmt"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	log "github.com/sirupsen/logrus"
)

func (m *Manager) cleanupTask(ctx context.Context, machine *domain.DevMachine, services []domain.DevMachineService, op *domain.DevMachineOperation) error {
	checkout, runs, err := m.store.BeginTaskResourceCleanup(ctx, *op, time.Now())
	if err != nil {
		return err
	}
	if checkout == nil {
		return &terminalOperationError{code: "task_cleanup_deferred", message: "Task resources are no longer eligible for cleanup"}
	}
	cleanupCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	return m.runtime.CleanupTaskResources(cleanupCtx, machine, services, checkout, *op.AgentTaskID, runs)
}

// The checkout and the bare repository must be opened at their machine-local
// paths. Git releases this worktree's registration without pruning its siblings
// or deleting the branch. Refuse symlinked parents and unregistered checkouts.
const taskWorkspaceCleanupScript = `set -eu
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1
root=$1 owner=$2 repo=$3 name=$4
shift 4
worktree=$root/tasks/$name
bare=$root/repos/$owner/$repo.git
tmp=$root/.kuayle/tmp
for dir in "$root/tasks" "$root/repos" "$root/repos/$owner" "$bare" "$worktree" "$root/.kuayle" "$tmp"; do
  [ ! -L "$dir" ] || { echo "unexpected symlink at $dir" >&2; exit 1; }
done
registered=
if [ -d "$bare" ]; then
  worktrees=$(git --git-dir="$bare" worktree list --porcelain)
  if printf '%s\n' "$worktrees" | grep -Fxq "worktree $worktree"; then
    registered=yes
  fi
fi
if [ -n "$registered" ]; then
  git --git-dir="$bare" worktree remove --force -- "$worktree"
  printf '%s\n' "$worktree"
elif [ -e "$worktree" ]; then
  echo "task worktree is not registered in its repository" >&2
  exit 1
fi
if [ -L "$root/current" ] && [ "$(readlink "$root/current")" = "$worktree" ]; then
  rm -- "$root/current"
fi
for key do
  dir=$tmp/$key
  if [ -L "$dir" ]; then
    rm -- "$dir"
  elif [ -e "$dir" ]; then
    # Go module directories in the run cache are read-only.
    chmod -R u+w -- "$dir" 2>/dev/null || true
    rm -rf -- "$dir"
  else
    continue
  fi
  printf '%s\n' "$dir"
done`

// The daemon's local socket is reachable inside its own container. Use exact
// ownership labels so an issue-level project name cannot match another task.
const taskVolumeCleanupScript = `set -eu
unset DOCKER_CONTEXT DOCKER_TLS_VERIFY DOCKER_CERT_PATH DOCKER_TLS
export DOCKER_HOST="unix://${XDG_RUNTIME_DIR:-/run/user/$(id -u)}/docker.sock"
compose=$(docker volume ls -q --filter "label=com.docker.compose.project=$1")
explicit=$(docker volume ls -q --filter "label=com.kuayle.task-id=$2")
volumes=$(printf '%s\n%s\n' "$compose" "$explicit" | sort -u)
failed=
for volume in $volumes; do
  if result=$(docker volume rm -- "$volume" 2>&1); then
    printf '%s\n' "$volume"
  else
    case "$result" in
      *": no such volume") ;;
      *": volume is in use"|*": volume is in use - "*) printf 'in use %s\n' "$volume" ;;
      *) printf '%s\n' "$result" >&2; failed=yes ;;
    esac
  fi
done
[ -z "$failed" ]`

var (
	taskCleanupRepoOwner = regexp.MustCompile(`^[A-Za-z0-9-]+$`)
	taskCleanupRepoName  = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	taskCleanupTreeName  = regexp.MustCompile(`^[a-z0-9._-]+$`)
)

// Compose drops dots and strips leading punctuation from directory names.
// Keep the task UUID suffix so worktrees on the same machine stay isolated.
func taskComposeProject(workspacePath string) string {
	return strings.TrimLeft(strings.ReplaceAll(path.Base(workspacePath), ".", ""), "_-")
}

func (r *DockerRuntime) CleanupTaskResources(ctx context.Context, machine *domain.DevMachine, services []domain.DevMachineService, checkout *domain.DevMachineCheckout, taskID uuid.UUID, runs []uuid.UUID) error {
	owner, repo, _ := strings.Cut(checkout.RepositoryFullName, "/")
	name := path.Base(checkout.WorkspacePath)
	if !taskCleanupRepoOwner.MatchString(owner) || !taskCleanupRepoName.MatchString(repo) || repo == "." || repo == ".." ||
		checkout.MachineID != machine.ID || checkout.WorkspaceID != machine.WorkspaceID || checkout.ScopeKey != taskID.String() ||
		!taskCleanupTreeName.MatchString(name) || strings.Contains(name, "..") ||
		!strings.HasSuffix(name, "-"+taskID.String()) || checkout.WorkspacePath != "/workspace/tasks/"+name {
		return &terminalOperationError{code: "task_cleanup_invalid_checkout", message: "Checkout paths do not match the task"}
	}
	image := ""
	for _, service := range services {
		if isDeveloperService(service.ServiceType) {
			image = service.ImageRef
			break
		}
	}
	if image == "" {
		return fmt.Errorf("developer image is unavailable for task cleanup")
	}
	args := []string{"/bin/sh", "-c", taskWorkspaceCleanupScript, "kuayle-task-cleanup", "/workspace", owner, repo, name}
	for _, id := range runs {
		args = append(args, "agent-"+id.String())
	}
	output, err := r.runTaskWorkspaceHelper(ctx, machine, image, taskID, "task-cleanup", args)
	fields := log.WithFields(log.Fields{"task_id": taskID, "machine_id": checkout.MachineID, "workspace_id": checkout.WorkspaceID})
	if removed := nonEmptyLines(output); len(removed) > 0 {
		fields.WithField("paths", removed).Info("removed task workspace paths")
	}
	if err != nil {
		return fmt.Errorf("remove task workspace files: %w", err)
	}
	if docker := dockerContainerID(services); docker != "" {
		if err := ctx.Err(); err != nil {
			return err
		}
		// A cancelled exec stream does not stop the daemon-side process. Let
		// the bounded command exit before the manager releases the task lease.
		volumeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 40*time.Second)
		defer cancel()
		output, err = r.execOutput(volumeCtx, docker, []string{"timeout", "-k", "5", "30", "/bin/sh", "-c", taskVolumeCleanupScript, "kuayle-task-volumes", taskComposeProject(checkout.WorkspacePath), taskID.String()})
		if err != nil {
			return fmt.Errorf("remove task Docker volumes: %w", err)
		}
		var removed, inUse []string
		for _, volume := range nonEmptyLines(output) {
			if name, ok := strings.CutPrefix(volume, "in use "); ok {
				inUse = append(inUse, name)
			} else {
				removed = append(removed, volume)
			}
		}
		if len(removed) > 0 {
			fields.WithField("volumes", removed).Info("removed task Docker volumes")
		}
		if len(inUse) > 0 {
			fields.WithField("volumes", inUse).Info("skipped task Docker volumes still in use")
		}
	} else if hasDockerService(services) {
		return fmt.Errorf("machine Docker daemon is unavailable for task cleanup")
	}
	return nil
}
