package machine

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/kuayle/kuayle-backend/internal/domain"
	log "github.com/sirupsen/logrus"
)

// An implement attempt declares the tests Kuayle reruns in
// <worktree>/.kuayle/validation.json (see the kuayle-agents skill). The file
// is untrusted: only a regular file, never through symlinks, is read, cut at
// one byte over the limit, and removed once taken so a later attempt must
// declare its own.
const validationFileTakeScript = `set -u
root=$1
[ -d "$root" ] && [ ! -L "$root" ] || exit 0
[ -d "$root/.kuayle" ] && [ ! -L "$root/.kuayle" ] || exit 0
f="$root/.kuayle/validation.json"
[ -e "$f" ] || [ -L "$f" ] || exit 0
if [ -L "$f" ] || [ ! -f "$f" ]; then rm -f "$f" 2>/dev/null; printf 'V invalid\n'; exit 0; fi
printf 'V %s\n' "$(head -c 65537 "$f" | base64 | tr -d '\n')"
rm -f "$f"
exit 0`

// validationFileRuntime is implemented by runtimes that can reach worktrees.
type validationFileRuntime interface {
	TakeValidationFile(ctx context.Context, services []domain.DevMachineService, checkout *domain.DevMachineCheckout) (*domain.AgentDeclaredValidation, error)
}

// parseValidationFileTake reads the take script's output: nil without a file.
func parseValidationFileTake(output string) *domain.AgentDeclaredValidation {
	line := strings.TrimSpace(output)
	if !strings.HasPrefix(line, "V") {
		return nil
	}
	value := strings.TrimSpace(strings.TrimPrefix(line, "V"))
	if value == "invalid" {
		return &domain.AgentDeclaredValidation{Error: "the file is not a regular file"}
	}
	data, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return &domain.AgentDeclaredValidation{Error: "the file could not be read"}
	}
	parsed := domain.ParseAgentValidationFile(data, len(data) > domain.AgentValidationFileMaxBytes)
	return &parsed
}

// TakeValidationFile takes the attempt's validation file as the developer
// container's user.
func (r *DockerRuntime) TakeValidationFile(ctx context.Context, services []domain.DevMachineService, checkout *domain.DevMachineCheckout) (*domain.AgentDeclaredValidation, error) {
	developer := developerContainerID(services)
	if developer == "" || checkout == nil || !strings.HasPrefix(checkout.WorkspacePath, taskWorktreeRoot+"/") {
		return nil, nil
	}
	output, err := r.execOutput(ctx, developer, []string{"/bin/sh", "-c", validationFileTakeScript, "kuayle-validation", checkout.WorkspacePath})
	if err != nil {
		return nil, err
	}
	return parseValidationFileTake(output), nil
}

// takeValidationFile records what a finished task attempt declared on run.
// A failure is logged; the attempt then simply declared nothing.
func (m *Manager) takeValidationFile(ctx context.Context, machine *domain.DevMachine, run *domain.DevMachineAgentRun, services []domain.DevMachineService, checkout *domain.DevMachineCheckout) {
	runtime, ok := m.runtime.(validationFileRuntime)
	if !ok || run.TaskID == nil || checkout == nil {
		return
	}
	declared, err := runtime.TakeValidationFile(ctx, services, checkout)
	if err != nil {
		log.WithFields(log.Fields{"machine_id": machine.ID, "agent_run_id": run.ID}).WithError(err).Warn("agent validation file could not be read")
		return
	}
	if declared == nil {
		return
	}
	data, err := json.Marshal(declared)
	if err != nil {
		return
	}
	raw := json.RawMessage(data)
	run.DeclaredValidation = &raw
}

var _ validationFileRuntime = (*DockerRuntime)(nil)
