package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
)

// Pipeline workflows are experimental (KUA-252): a server saves them, and
// starts tasks on them, only with WORKFLOW_PIPELINES_ENABLED on. Their tick
// (team_tick.go) runs those tasks; the graph engine never runs a pipeline.
var ErrPipelinesDisabled = errors.New("pipeline workflows are turned off on this server")

// SetPipelines turns the experimental pipeline workflows on or off.
func (s *AgentTaskService) SetPipelines(enabled bool) { s.pipelines = enabled }

// PipelinesEnabled reports the server's switch; a nil service has it off.
func (s *AgentTaskService) PipelinesEnabled() bool { return s != nil && s.pipelines }

// pipelineRefusal says why raw, when it is a pipeline, cannot be saved or
// start a task; nil for a graph.
func (s *AgentTaskService) pipelineRefusal(raw []byte) error {
	if domain.WorkflowKind(raw) == domain.WorkflowKindPipeline && !s.PipelinesEnabled() {
		return ErrPipelinesDisabled
	}
	return nil
}

// startablePipeline checks a pipeline a task starts on, as runnableWorkflow
// checks a graph: valid with the workspace's active profiles, and every role
// and the lead allowed by the workspace policy. It returns the writer's
// profile, whose harness and model the first turn runs.
func (s *AgentTaskService) startablePipeline(ctx context.Context, ws uuid.UUID, version *domain.WorkflowVersion) (*domain.Pipeline, domain.AgentProfile, error) {
	pipeline, err := domain.ParsePipeline(version.Graph)
	if err != nil {
		return nil, domain.AgentProfile{}, ErrWorkflowUnusable
	}
	profiles, err := s.definitions.ActiveProfiles(ctx, ws)
	if err != nil {
		return nil, domain.AgentProfile{}, err
	}
	if len(domain.ValidatePipeline(*pipeline, profiles)) > 0 {
		return nil, domain.AgentProfile{}, ErrWorkflowUnusable
	}
	for _, member := range pipeline.Members() {
		if err = s.profilePolicy(ctx, ws, profiles[member.ProfileID]); err != nil {
			return nil, domain.AgentProfile{}, err
		}
	}
	writer, _ := pipeline.Writer()
	return pipeline, profiles[writer.ProfileID], nil
}

// pipelineIssues is every problem of a pipeline (domain.ValidatePipeline).
func (s *WorkflowService) pipelineIssues(ctx context.Context, ws uuid.UUID, raw json.RawMessage) ([]domain.WorkflowIssue, error) {
	pipeline, err := domain.ParsePipeline(raw)
	if err != nil {
		var invalid *domain.WorkflowInvalidError
		if errors.As(err, &invalid) {
			return invalid.Issues, nil
		}
		return nil, err
	}
	profiles, err := s.repo.ActiveProfiles(ctx, ws)
	if err != nil {
		return nil, err
	}
	return domain.ValidatePipeline(*pipeline, profiles), nil
}

// pipeline is the pipeline a task runs and the profiles of the workspace
// (archived ones too, for the roles that pinned them).
func (w *WorkflowRunner) pipeline(ctx context.Context, task domain.AgentTask) (*domain.Pipeline, map[uuid.UUID]domain.AgentProfile, error) {
	if !task.Pipeline || task.WorkflowVersionID == nil {
		return nil, nil, ErrInvalidOperation
	}
	w.mu.Lock()
	p := w.pipelines[*task.WorkflowVersionID]
	w.mu.Unlock()
	if p == nil {
		version, err := w.definitions.Version(ctx, task.WorkspaceID, *task.WorkflowVersionID)
		if err != nil {
			return nil, nil, err
		}
		if p, err = domain.ParsePipeline(version.Graph); err != nil {
			return nil, nil, err
		}
		w.mu.Lock()
		w.pipelines[*task.WorkflowVersionID] = p
		w.mu.Unlock()
	}
	profiles, err := w.definitions.AllProfiles(ctx, task.WorkspaceID)
	return p, profiles, err
}

// decidePipeline applies a person's decision on a pipeline task: approving
// the head where a phase waits for the owner's approval, or continuing or
// stopping where the tick paused. It reads the task as it is locked for the
// decision, so a pass that ran meanwhile is neither lost nor overwritten.
func (s *AgentTaskService) decidePipeline(ctx context.Context, ws, id uuid.UUID, sequence int, decision string) (*domain.AgentTask, error) {
	access, ok := domain.AccessFromContext(ctx)
	if !ok || s.workflow == nil {
		return nil, ErrWorkflowDecision
	}
	decided, err := s.Repo.DecidePipeline(ctx, ws, id, func(task domain.AgentTask) (*repository.PipelineApply, error) {
		if !task.Pipeline || task.WorkflowSequence != sequence || task.WorkflowStatus == nil || task.WorkflowPausedAt != nil {
			return nil, ErrWorkflowDecision
		}
		p, _, err := s.workflow.pipeline(ctx, task)
		if err != nil {
			return nil, err
		}
		st := readTickState(task)
		now := s.workflow.now()
		apply := &repository.PipelineApply{Sequence: sequence, Phase: deref(task.WorkflowNode), Entry: repository.WorkflowEntry{Kind: repository.WorkflowEnterWait, WakeAt: &now}}
		phase := p.Phases[max(slices.IndexFunc(p.Phases, func(phase domain.PipelinePhase) bool { return phase.ID == apply.Phase }), 0)]
		switch {
		case *task.WorkflowStatus == domain.WorkflowStatusWaiting && decision == WorkflowDecisionApprove && task.State == "awaiting_review":
			var head string
			_ = json.Unmarshal(st.Facts["head_sha"], &head)
			if head == "" || !slices.ContainsFunc(phase.Checks, func(c domain.PipelineCheck) bool { return c.Fact == domain.PipelineFactApproved }) {
				return nil, ErrWorkflowDecision
			}
			// A new approval starts a new merge.
			st.Approved, st.Merge = &tickApproval{Head: head, By: access.UserID}, nil
			apply.Entry.Reason = fmt.Sprintf("%s%s: approved %s; Kuayle reads the facts", domain.WorkflowReasonPrefix, phase.Label, shortSHA(head))
			apply.Notes = []domain.NotebookNote{{Event: fmt.Sprintf("A person approved merging %s at %s", shortSHA(head), phase.Label)}}
		case *task.WorkflowStatus == domain.WorkflowStatusPaused && decision == WorkflowDecisionContinue:
			// Every role may be asked again, the stall and the waits count
			// anew and the merge starts again; a budget that is used up
			// grants another of the same size.
			st.Stall, st.Wait, st.Asked, st.Merge = tickStall{}, tickWait{}, nil, nil
			t := &tickPass{task: task, p: *p, now: now}
			_, apply.ExtendBudget = t.exhausted(1)
			apply.Entry.Reason = domain.WorkflowReasonPrefix + "Continued by a person; Kuayle reads the facts"
			apply.Notes = []domain.NotebookNote{{Event: fmt.Sprintf("A person chose %q at %s", decision, phase.Label)}}
		case *task.WorkflowStatus == domain.WorkflowStatusPaused && decision == WorkflowDecisionStop:
			apply.Entry = repository.WorkflowEntry{Kind: repository.WorkflowEnterFail, Reason: domain.WorkflowReasonPrefix + "stopped by a person where it paused"}
		default:
			return nil, ErrWorkflowDecision
		}
		if apply.State, err = json.Marshal(st); err != nil {
			return nil, err
		}
		return apply, nil
	})
	if errors.Is(err, repository.ErrAgentTaskConflict) {
		return nil, ErrWorkflowDecision
	}
	return decided, err
}
