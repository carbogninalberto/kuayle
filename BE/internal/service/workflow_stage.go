package service

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
)

func (w *WorkflowRunner) finishStage(ctx context.Context, task domain.AgentTask, graph domain.WorkflowGraph, profiles map[uuid.UUID]domain.AgentProfile, node domain.WorkflowNode, run *repository.WorkflowRun) error {
	step, err := w.tasks.CurrentWorkflowStep(ctx, task)
	if err != nil {
		return err
	}
	// A retry is compared to the start of the whole stage visit.
	first := run
	if step.StageFirstRunID != nil && *step.StageFirstRunID != task.RunID {
		original := task
		original.RunID = *step.StageFirstRunID
		first, err = w.tasks.WorkflowRun(ctx, original)
		if err != nil {
			return err
		}
	}
	problem := stageWorktreeProblem(first, run)
	work := task
	if step.StageInputRunID != nil && problem == "" {
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
		if node.StageReadsWorktree() && problem != "" {
			return fail(problem)
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

// Only a stage that kept HEAD and the non-ignored tree may reuse the input
// attempt's validation and PR. A build can add ignored output on any run.
func stageWorktreeProblem(first, last *repository.WorkflowRun) string {
	beforeHead, beforeTree, beforeOK := domain.WorktreeContent(first.WorktreeBefore)
	afterHead, afterTree, afterOK := domain.WorktreeContent(last.WorktreeAfter)
	if !beforeOK || !afterOK {
		// Older runs may have only the stricter, whole-fingerprint comparison.
		if first == last && (first.WorktreeBefore == "" || last.WorktreeAfter == "") && last.WorktreeChanged != nil {
			if !*last.WorktreeChanged {
				return ""
			}
			if len(last.WorktreeFiles) > 0 {
				return "Kuayle could not verify the stage's worktree. The run recorded changes to " + changedFileSummary(last.WorktreeFiles)
			}
		}
		return "Kuayle could not verify that the stage assistant left HEAD and the non-ignored files unchanged"
	}
	if beforeTree == afterTree {
		if beforeHead != afterHead {
			return "The stage assistant changed HEAD, so the built-in stage cannot use the input attempt"
		}
		return ""
	}
	problem := "The stage assistant changed non-ignored files"
	// File lists are per-run. A retry may have only added build output
	// after the first run's edits, or made further edits of its own.
	if _, tree, ok := domain.WorktreeContent(first.WorktreeAfter); ok && tree == afterTree && len(first.WorktreeFiles) > 0 {
		return problem + ": " + changedFileSummary(first.WorktreeFiles)
	}
	if _, tree, ok := domain.WorktreeContent(last.WorktreeBefore); ok && tree != afterTree && len(last.WorktreeFiles) > 0 {
		return problem + ". Files changed on the final run: " + changedFileSummary(last.WorktreeFiles)
	}
	return problem
}
