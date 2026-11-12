package machine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	log "github.com/sirupsen/logrus"
)

// agentImageIdentity is implemented by runtimes that can resolve an image
// reference to what the daemon holds: its local ID and repository digest.
type agentImageIdentity interface {
	ImageIdentity(ctx context.Context, reference string) (id, digest string, err error)
}

// agentCLIVersionPackages maps a harness to the npm package the shared agent
// CLI refresh reports the installed version of (agent_clis.go).
var agentCLIVersionPackages = map[string]string{
	"claude-code": "@anthropic-ai/claude-code",
	"codex":       "@openai/codex",
	"opencode":    "opencode-ai",
	"qwen-code":   "@qwen-code/qwen-code",
}

// harnessVersion is the version the manager already knows the run will start:
// the shared CLI refresh's report, never a guess. Unknown versions stay empty.
func (m *Manager) harnessVersion(provider string) (version, source string) {
	pkg, ok := agentCLIVersionPackages[provider]
	if !ok {
		return "", ""
	}
	version = m.AgentCLIVersions()[pkg]
	if version == "" {
		return "", ""
	}
	return version, "shared agent CLI refresh"
}

// runEnvironment records what a run ran on. execution is nil before the
// container starts; when it is known its actual limits and peak process count
// replace the machine's planned values.
func (m *Manager) runEnvironment(ctx context.Context, machine *domain.DevMachine, run *domain.DevMachineAgentRun, provider *domain.DevMachineAgentProvider, execution *AgentExecution) domain.AgentRunEnvironment {
	// Keep what the start recorded, such as the image identity, and refresh
	// it: the image may be gone by the time the run ends.
	var environment domain.AgentRunEnvironment
	_ = json.Unmarshal(run.Environment, &environment)
	environment.Harness = run.ProviderID
	environment.Model = run.Model
	environment.Effort = run.Thinking
	environment.CPUMillis = int64(machine.CPUMillis)
	environment.MemoryBytes = int64(machine.MemoryMB) * 1024 * 1024
	environment.PidsLimit = int64(machine.PidsLimit)
	environment.TimeLimitSeconds = run.MaxRuntimeSeconds
	environment.WorktreeReadOnly = run.WorktreeReadOnly
	if provider != nil {
		environment.ImageRef = provider.ImageRef
	}
	var argv []string
	if json.Unmarshal(run.CommandArgv, &argv) == nil && len(argv) > 0 {
		environment.HarnessCommand = argv[0]
	}
	if run.NativeSessionID != nil {
		environment.ResumedSession = *run.NativeSessionID
	}
	// The version is recorded once, by the start, and the end keeps it.
	if identifier, ok := m.runtime.(agentImageIdentity); ok && environment.ImageRef != "" && environment.ImageID == "" && environment.ImageDigest == "" {
		if id, digest, err := identifier.ImageIdentity(ctx, environment.ImageRef); err == nil {
			environment.ImageID, environment.ImageDigest = id, digest
		}
	}
	if execution != nil {
		environment.ContainerID = execution.ContainerID
		environment.PidsPeak = execution.PidsPeak
		if execution.CPUMillis > 0 {
			environment.CPUMillis = execution.CPUMillis
		}
		if execution.MemoryLimitBytes > 0 {
			environment.MemoryBytes = execution.MemoryLimitBytes
		}
		if execution.PidsLimit > 0 {
			environment.PidsLimit = execution.PidsLimit
		}
	}
	return environment
}

// recordRunEnvironment keeps what the run was given when it starts. A trace
// that cannot be written never stops the run.
func (m *Manager) recordRunEnvironment(ctx context.Context, run *domain.DevMachineAgentRun, environment domain.AgentRunEnvironment) {
	data, err := json.Marshal(environment)
	if err != nil {
		return
	}
	run.Environment = domain.NullJSON(data)
	if err := m.store.RecordAgentRunEnvironment(ctx, run.ID, run.Environment); err != nil {
		log.WithField("agent_run_id", run.ID).WithError(err).Warn("the run's environment could not be recorded")
	}
}

// runFailureEvidence is what the manager knows about a run that did not
// succeed. All text is redacted: stderr comes from the output buffer.
func runFailureEvidence(status string, timeLimitSeconds int, execution *AgentExecution, result agent.RunResult, executionErr error, memoryLimit, usageLimit, cancelledBy, stderr string) domain.RunFailureEvidence {
	evidence := domain.RunFailureEvidence{
		Status:           status,
		TimeLimitSeconds: timeLimitSeconds,
		OOMKilled:        execution != nil && execution.OOMKilled,
		MemoryLimit:      memoryLimit,
		UsageLimit:       usageLimit,
		HarnessError:     result.FailureReason,
		NoFinalMessage:   result.NoFinalMessage,
		Stderr:           stderr,
		CancelledBy:      cancelledBy,
	}
	if execution != nil {
		code := execution.ExitCode
		evidence.ExitCode = &code
		evidence.PidsLimit = execution.PidsLimit
		evidence.PidsPeak = execution.PidsPeak
	}
	if executionErr != nil {
		message := safeError(executionErr)
		if strings.HasPrefix(message, "sign_in_required") {
			evidence.HarnessError = message
		} else if errors.Is(executionErr, repository.ErrAgentTaskUnauthorized) {
			// A policy check when a step starts refused the run: the policy's.
			evidence.PolicyRefused = true
		} else {
			evidence.ExecutionError = message
		}
	}
	return evidence
}

// applyRunFailure stores the class, rule and cause on a run that did not
// succeed, and makes the cause its error message when the run had none more
// specific.
func applyRunFailure(run *domain.DevMachineAgentRun, failure domain.RunFailure, preserveMessage bool) {
	class, rule, cause := string(failure.Class), failure.Rule, failure.Cause
	run.FailureClass, run.FailureRule, run.FailureCause = &class, &rule, &cause
	if !preserveMessage || run.ErrorMessage == nil || strings.TrimSpace(*run.ErrorMessage) == "" {
		message := cause
		run.ErrorMessage = &message
	}
}
