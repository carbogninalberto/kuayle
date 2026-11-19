package service

import (
	"context"
	"fmt"
	"slices"
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
	last := strings.TrimSpace(run.Summary)
	if i := strings.LastIndexByte(last, '\n'); i >= 0 {
		last = strings.TrimSpace(last[i+1:])
	}
	// A plain handoff uses the built-in operation. Only a complete final
	// message can choose a different branch.
	if !run.FinalMessage || run.SummaryTruncated {
		last = ""
	}
	outcome, warning := stageRoute(graph, node, last)
	var apply *repository.WorkflowApply
	if outcome != "" {
		apply, err = w.plan(ctx, work, outcome, run.NotebookNote, false)
	} else if node.Type == domain.WorkflowNodeStart {
		apply, err = w.plan(ctx, work, domain.WorkflowElse, run.NotebookNote, false)
	} else {
		if node.StageReadsWorktree() && problem != "" {
			return w.AgentFailedBecause(ctx, task, strings.TrimSpace(problem+" "+warning))
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
	if warning != "" {
		apply.Entry.Reason = truncateUTF8Bytes(apply.Entry.Reason+" "+warning, 2048)
	}
	apply.StageFinished = true
	if work.RunID != task.RunID {
		apply.RestoreRun = &work.RunID
	}
	_, err = w.tasks.ApplyWorkflow(ctx, task, *apply)
	return err
}

// An empty outcome runs the built-in stage, including its usual worktree checks.
func stageRoute(graph domain.WorkflowGraph, node domain.WorkflowNode, last string) (outcome, warning string) {
	if last == "KUAYLE_STAGE_CONTINUE" || !strings.HasPrefix(last, "KUAYLE_STAGE_") {
		return "", ""
	}
	options := graph.SelectableStageRoutes(node)
	if id, ok := strings.CutPrefix(last, "KUAYLE_STAGE_ROUTE "); ok && slices.Contains(options, id) {
		return domain.WorkflowAssistant + ":" + id, ""
	}
	selectable := strings.Join(options, ", ")
	if selectable == "" {
		selectable = "none"
	}
	return "", fmt.Sprintf("Warning: ignored stage assistant directive %q. Using KUAYLE_STAGE_CONTINUE. Selectable transitions: %s.", oneLine(last, 250), selectable)
}

// Only a stage that kept HEAD and the non-ignored tree may reuse the input
// attempt's PR. A build can add ignored output on any run.
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
