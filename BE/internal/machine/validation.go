package machine

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	log "github.com/sirupsen/logrus"
)

// validateAgent uses the developer environment, never a model invocation. The
// original credential snapshot is used only for output redaction, not injection.
// It runs both the attempt's independent validation and manual checks
// (run_check); the store applies each one's own authority.
func (m *Manager) validateAgent(ctx context.Context, machine *domain.DevMachine, op *domain.DevMachineOperation) error {
	step, err := m.store.GetValidation(ctx, *op)
	if err != nil {
		return err
	}
	if step.Status == domain.DevMachineAgentRunStepStatusSucceeded || step.Status == domain.DevMachineAgentRunStepStatusFailed || step.Status == domain.DevMachineAgentRunStepStatusCancelled || step.Status == domain.DevMachineAgentRunStepStatusSkipped {
		return nil
	}
	run, err := m.store.GetAgentRunInternal(ctx, step.AgentRunID)
	if err != nil {
		return err
	}
	if run == nil || run.TaskID == nil || run.CheckoutID == nil || run.EncryptedOutputSecrets == nil {
		return &terminalOperationError{code: "validation_unavailable", message: "Validation requires the original checkout and redaction snapshot"}
	}
	secrets, err := m.decodeAgentRunSecrets(*run.EncryptedOutputSecrets)
	if err != nil {
		return err
	}
	checkout, err := m.store.GetCheckoutInternal(ctx, *run.CheckoutID)
	if err != nil {
		return err
	}
	if checkout == nil || checkout.Status != "ready" || checkout.MachineID != machine.ID || checkout.WorkspaceID != machine.WorkspaceID || checkout.ScopeKey != run.TaskID.String() {
		return &terminalOperationError{code: "validation_unavailable", message: "Task checkout is unavailable"}
	}
	services, err := m.store.ListServices(ctx, machine.WorkspaceID, machine.ID)
	if err != nil {
		return err
	}
	image := ""
	for _, service := range services {
		if service.ServiceType == "ide" || service.ServiceType == "terminal" {
			image = service.ImageRef
			break
		}
	}
	if image == "" {
		return &terminalOperationError{code: "validation_unavailable", message: "Developer environment is unavailable"}
	}
	key := checkContainerKey(run, step)
	service := &domain.DevMachineService{ID: step.ID, WorkspaceID: machine.WorkspaceID, MachineID: machine.ID, AgentRunID: &run.ID, ServiceType: "agent", ServiceKey: key, ContainerName: "kuayle-" + machine.RoutingKey + "-" + key, ImageRef: image, InternalHost: key, InternalPort: 8080, Status: "pending", HealthStatus: "unknown"}
	if err = m.store.CreateRuntimeService(ctx, service); err != nil {
		return err
	}
	if service.ID != step.ID {
		return repository.ErrAgentTaskConflict
	}
	// A restart cannot reset the check's timeout indefinitely.
	started := step.CreatedAt
	if step.StartedAt != nil {
		started = *step.StartedAt
	}
	deadline := started.Add(time.Duration(run.MaxRuntimeSeconds) * time.Second)
	if machine.ExpiresAt.Before(deadline) {
		deadline = machine.ExpiresAt
	}
	runCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	output := newAgentOutputBuffer(secrets.Redactions, func(stream string, sequence int64, source, content string, truncated bool) error {
		writeCtx, done := context.WithTimeout(runCtx, 5*time.Second)
		defer done()
		return m.store.CreateValidationOutput(writeCtx, *op, &domain.DevMachineLogChunk{WorkspaceID: machine.WorkspaceID, MachineID: machine.ID, AgentRunID: &run.ID, ServiceID: &step.ID, Stream: stream, Sequence: sequence, SourceKey: source, Content: content, Truncated: truncated})
	})
	observer := &AgentObserver{Authorize: func() error { _, err := m.store.GetValidation(runCtx, *op); return err }, Started: func(id string) error { return m.store.StartValidation(runCtx, *op, id) }, Output: output.WriteDocker}
	execution, execErr := m.runtime.RunValidation(runCtx, machine, run, step, checkout, image, observer)
	if step.ComposeFile != "" {
		// Services live only for this check; the check container stays for replay.
		defer func() {
			cleanupCtx, done := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
			defer done()
			if err := m.runtime.RemoveCheckServices(cleanupCtx, machine, run, step); err != nil {
				log.WithField("agent_run_id", run.ID).WithError(err).Warn("check service cleanup failed")
			}
		}()
	}
	var composeErr *ComposeError
	if errors.As(execErr, &composeErr) {
		// A rejected or unhealthy stack is the project's configuration, so the
		// reason is shown with the check instead of being retried.
		if err := output.Write("stderr", []byte("Kuayle: "+composeErr.Error()+"\n")); err == nil {
			_ = output.Finish()
		}
		return &terminalOperationError{code: "compose_rejected", message: "Compose file rejected: " + composeErr.Reason}
	}
	if memoryLimit := m.recordRunMemoryLimit(ctx, machine, run, "validation", execution); memoryLimit != "" && execErr == nil {
		writeKuayleNote(output, run.ID, memoryLimit)
	}
	if execErr == nil {
		execErr = output.Finish()
	}
	if execErr != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			cleanupCtx, done := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer done()
			if step.StepType == manualCheckStepType {
				// Only this check's container stops; the attempt's are kept.
				if err = m.runtime.RemoveCheck(cleanupCtx, machine, run, step); err != nil {
					return fmt.Errorf("validation_cleanup_failed: %w", err)
				}
				return &terminalOperationError{code: "validation_timeout", message: "The check exceeded its time limit"}
			}
			if err = m.runtime.CancelAgent(cleanupCtx, run); err != nil {
				return fmt.Errorf("validation_cleanup_failed: %w", err)
			}
			return &terminalOperationError{code: "validation_timeout", message: "Independent validation exceeded its time limit"}
		}
		// The runtime stops a process whose output/authorization failed. Keep
		// its deterministic container for replay; deleting it would rerun the
		// command on an operation retry. Cancellation owns physical removal.
		return execErr
	}
	if execution == nil {
		return errors.New("validation execution returned no result")
	}
	// QA artifacts are best effort: they never change the check's outcome.
	if artifacts, err := m.runtime.CollectValidationArtifacts(ctx, machine, run, step, checkout); err != nil {
		log.WithField("agent_run_id", run.ID).WithError(err).Warn("validation artifact collection failed")
	} else if len(artifacts) > 0 {
		if err := m.store.SaveValidationArtifacts(ctx, *op, artifacts); err != nil {
			return err
		}
	}
	if err = m.store.CompleteValidation(ctx, *op, execution.ExitCode); err != nil {
		return err
	}
	// A recorded manual check is never replayed, so its container can go.
	if err = m.runtime.RemoveCheck(ctx, machine, run, step); err != nil {
		log.WithField("agent_run_id", run.ID).WithError(err).Warn("manual check cleanup failed")
	}
	return nil
}

// manualCheckFailure is the summary shown for a manual check that could not
// run: a terminal reason is the project's own configuration or a limit and
// is safe to show; other infrastructure errors stay generic.
func manualCheckFailure(err error) string {
	var terminal *terminalOperationError
	if errors.As(err, &terminal) {
		return terminal.message
	}
	return "The check could not finish"
}
