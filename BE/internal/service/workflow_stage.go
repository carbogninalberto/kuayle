package service

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
)

func (w *WorkflowRunner) finishStage(ctx context.Context, task domain.AgentTask, graph domain.WorkflowGraph, profiles map[uuid.UUID]domain.AgentProfile, node domain.WorkflowNode, run repository.WorkflowRun) error {
	step, err := w.tasks.CurrentWorkflowStep(ctx, task)
	if err != nil {
		return err
	}
	// A changed worktree is a new attempt. Only an unchanged worktree may
	// continue using the input attempt's published PR.
	work := task
	unchanged := run.WorktreeChanged != nil && !*run.WorktreeChanged
	if step.StageFirstRunID != nil && *step.StageFirstRunID != task.RunID {
		original := task
		original.RunID = *step.StageFirstRunID
		first, err := w.tasks.WorkflowRun(ctx, original)
		if err != nil {
			return err
		}
		unchanged = first.WorktreeBefore != "" && first.WorktreeBefore == run.WorktreeAfter
	}
	if step.StageInputRunID != nil && unchanged {
		work.RunID = *step.StageInputRunID
	}
	fail := func(problem string) error {
		return w.AgentFailedBecause(ctx, task, problem)
	}
	last := strings.TrimSpace(run.Summary)
	if i := strings.LastIndexByte(last, '\n'); i >= 0 {
		last = strings.TrimSpace(last[i+1:])
	}
	// A plain handoff uses the built-in operation. Only a complete final
	// message can choose a different branch.
	if !run.FinalMessage || run.SummaryTruncated {
		last = ""
	}
	var apply *repository.WorkflowApply
	if id, chosen := strings.CutPrefix(last, "KUAYLE_STAGE_ROUTE "); chosen {
		outcome := domain.WorkflowAssistant + ":" + id
		if decision := graph.Decide(work.Cursor(), outcome, w.now(), profiles); decision.Stuck {
			return fail("The stage assistant selected an unavailable transition")
		}
		apply, err = w.plan(ctx, work, outcome, run.NotebookNote, false)
	} else if strings.HasPrefix(last, "KUAYLE_STAGE_") && last != "KUAYLE_STAGE_CONTINUE" {
		return fail("The stage assistant did not end its final message with a valid routing decision")
	} else if node.Type == domain.WorkflowNodeStart {
		apply, err = w.plan(ctx, work, domain.WorkflowElse, run.NotebookNote, false)
	} else {
		if node.StageReadsWorktree() && !unchanged {
			return fail("The stage assistant must leave the worktree unchanged to continue this stage")
		}
		entry, immediate, detail, enterErr := w.enterNative(ctx, work, graph, profiles, node, domain.WorkflowReasonPrefix+node.Label, w.now(), step.MergeWait())
		if enterErr != nil {
			return enterErr
		}
		if immediate != "" {
			apply, err = w.plan(ctx, work, immediate, detail, false)
		} else {
			apply = &repository.WorkflowApply{Sequence: task.WorkflowSequence, Version: task.WorkflowVersionID, Hold: true, Entry: entry, Outcome: node.Outcome, Detail: run.NotebookNote}
		}
	}
	if err != nil {
		return err
	}
	apply.StageFinished = true
	if work.RunID != task.RunID {
		apply.RestoreRun = &work.RunID
	}
	_, err = w.tasks.ApplyWorkflow(ctx, task, *apply)
	return err
}
