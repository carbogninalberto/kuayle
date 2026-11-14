package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
)

// The worktree scan tools (KUA-141) read a task's live checkout on its dev
// machine: the working tree, uncommitted changes included. They queue one
// read-only operation and wait briefly for the manager's result, so the
// recorded tool call shows exactly what was read.

const (
	worktreeScanPollInterval = 250 * time.Millisecond
	worktreeScanTimeout      = 20 * time.Second
	// linkedRepositoryHints bounds the repository names a scan target error lists.
	linkedRepositoryHints = 10
)

// worktreeScanStore queues read-only worktree scans and reads their results.
type worktreeScanStore interface {
	CreateWorktreeScan(ctx context.Context, ws, user uuid.UUID, target domain.WorktreeScanTarget, req domain.WorktreeScanRequest) (*domain.WorktreeScan, error)
	GetWorktreeScan(ctx context.Context, ws, id uuid.UUID) (*domain.WorktreeScan, error)
}

// SetWorktreeScans enables the live worktree tools and the source "task"
// target of repo_grep, repo_glob and repo_read_file.
func (t *AssistantTools) SetWorktreeScans(store worktreeScanStore) {
	t.worktreeScans = store
	if t.worktreeScanWait == 0 {
		t.worktreeScanWait = worktreeScanTimeout
	}
}

// linkedRepositoryLister lists a workspace's linked repositories for a
// retry hint; the Dev Machine repository implements it.
type linkedRepositoryLister interface {
	ListLinkedRepositories(ctx context.Context, ws uuid.UUID) ([]domain.GitHubRepoModel, error)
}

var assistantWorktreeToolCatalog = []assistantTool{
	{name: "get_task_worktree", worktree: true, description: "Read the git state of an issue's newest coding task on its dev machine: branch, HEAD commit, whether the worktree has uncommitted changes (with the changed paths), commits ahead and behind its upstream, and the last commit subject. Use it to check whether a failed or finished task's work is still there. Only the task's creator can read it.", parameters: `{"type":"object","properties":{"identifier":{"type":"string","maxLength":32}},"required":["identifier"],"additionalProperties":false}`},
	{name: "list_machine_worktrees", worktree: true, description: "List the task worktrees under /workspace/tasks on a dev machine: path, branch, commit, whether each has uncommitted changes and when it last changed. Give machine_id (one of your own machines) or identifier (the machine of an issue's newest coding task you created), not both. Read-only.", parameters: `{"type":"object","properties":{"machine_id":{"type":"string","maxLength":36},"identifier":{"type":"string","maxLength":32}},"additionalProperties":false}`},
}

// taskWorktreeTarget resolves the issue with the user's visibility, then its
// newest coding task, which only its creator may read. Missing and hidden
// issues look the same.
func (t *AssistantTools) taskWorktreeTarget(ctx context.Context, ws, user uuid.UUID, identifier string) (*domain.Issue, *repository.FollowUpTarget, error) {
	if t.work == nil {
		return nil, nil, toolFailure("task worktrees are not available; use the repository scans on the pushed code")
	}
	issue, err := t.issues.GetByIdentifier(ctx, ws, strings.ToUpper(strings.TrimSpace(identifier)))
	if err != nil || issue == nil {
		return nil, nil, toolFailure("issue not found")
	}
	target, err := t.work.FollowUpTarget(ctx, ws, issue.ID)
	if errors.Is(err, repository.ErrNoMergeTarget) {
		return nil, nil, toolFailure("this issue has no coding task")
	}
	if err != nil {
		return nil, nil, err
	}
	if target.CreatorID != user {
		return nil, nil, toolFailure("only the task's creator can read its machine")
	}
	return issue, target, nil
}

// refuseWorktreeScans refuses the live worktree target when the workspace
// switched its tools off. The conversation refuses a direct call by its own
// name; source task on repo_grep, repo_glob and repo_read_file needs the same
// switch checked here.
func (t *AssistantTools) refuseWorktreeScans(ctx context.Context, ws uuid.UUID) error {
	disabled, err := t.disabledTools(ctx, ws)
	if err != nil {
		return err
	}
	if disabled["get_task_worktree"] {
		return toolFailure("get_task_worktree is disabled in this workspace; a workspace owner or admin can turn it on in Settings → Assistant")
	}
	return nil
}

// runTaskScan queues a scan of a task's live worktree and waits for its result.
func (t *AssistantTools) runTaskScan(ctx context.Context, ws, user uuid.UUID, identifier string, req domain.WorktreeScanRequest) (map[string]any, error) {
	if err := t.refuseWorktreeScans(ctx, ws); err != nil {
		return nil, err
	}
	if t.worktreeScans == nil {
		return nil, toolFailure("live worktree scans are not available; use source github to read the pushed code")
	}
	_, target, err := t.taskWorktreeTarget(ctx, ws, user, identifier)
	if err != nil {
		return nil, err
	}
	return t.queueWorktreeScan(ctx, ws, user, domain.WorktreeScanTarget{TaskID: &target.TaskID}, req)
}

// runWorktreeTool runs get_task_worktree and list_machine_worktrees.
func (t *AssistantTools) runWorktreeTool(ctx context.Context, ws, user uuid.UUID, name, arguments string) (any, *uuid.UUID, error) {
	if name == "get_task_worktree" {
		var input struct {
			Identifier string `json:"identifier"`
		}
		if err := decodeToolInput(arguments, &input); err != nil {
			return nil, nil, err
		}
		issue, target, err := t.taskWorktreeTarget(ctx, ws, user, input.Identifier)
		if err != nil {
			return nil, nil, err
		}
		result, err := t.queueWorktreeScan(ctx, ws, user, domain.WorktreeScanTarget{TaskID: &target.TaskID}, domain.WorktreeScanRequest{Kind: domain.WorktreeScanStatus})
		if err != nil {
			return nil, nil, err
		}
		result["identifier"], result["task_id"] = issue.Identifier, target.TaskID
		return result, &issue.ID, nil
	}
	var input struct {
		MachineID  string `json:"machine_id"`
		Identifier string `json:"identifier"`
	}
	if err := decodeToolInput(arguments, &input); err != nil {
		return nil, nil, err
	}
	machineID := strings.TrimSpace(input.MachineID)
	identifier := strings.ToUpper(strings.TrimSpace(input.Identifier))
	if machineID != "" && identifier != "" {
		return nil, nil, toolFailure("give machine_id or identifier, not both")
	}
	target := domain.WorktreeScanTarget{}
	var issueID *uuid.UUID
	switch {
	case machineID != "":
		id, err := uuid.Parse(machineID)
		if err != nil {
			return nil, nil, toolFailure("machine_id is not a machine id")
		}
		target.MachineID = &id
	case identifier != "":
		issue, task, err := t.taskWorktreeTarget(ctx, ws, user, identifier)
		if err != nil {
			return nil, nil, err
		}
		target.TaskID, issueID = &task.TaskID, &issue.ID
	default:
		return nil, nil, toolFailure("give machine_id or identifier: one of your machines, or an issue whose newest coding task you created")
	}
	result, err := t.queueWorktreeScan(ctx, ws, user, target, domain.WorktreeScanRequest{Kind: domain.WorktreeScanList})
	if err != nil {
		return nil, nil, err
	}
	return result, issueID, nil
}

// queueWorktreeScan queues one scan and polls its result every 250 ms for at
// most 20 s, or the test-shortened wait.
func (t *AssistantTools) queueWorktreeScan(ctx context.Context, ws, user uuid.UUID, target domain.WorktreeScanTarget, req domain.WorktreeScanRequest) (map[string]any, error) {
	if t.worktreeScans == nil {
		return nil, toolFailure("live worktree scans are not available; use source github to read the pushed code")
	}
	// As on the Dev Machine routes, the demo rule also covers reading a machine.
	if err := t.machineAccess.Require(user); err != nil {
		return nil, machineToolFailure(err)
	}
	scan, err := t.worktreeScans.CreateWorktreeScan(ctx, ws, user, target, req)
	if err != nil {
		return nil, worktreeScanFailure(err)
	}
	if scan == nil {
		return nil, toolFailure("the scan was not queued; try again")
	}
	scan, err = t.awaitWorktreeScan(ctx, ws, scan)
	if err != nil {
		return nil, err
	}
	if scan.Error != nil {
		return nil, toolFailure("the scan failed on the machine: %s", truncateRunes(*scan.Error, 400))
	}
	var result domain.WorktreeScanResult
	if len(scan.Result) > 0 && json.Unmarshal(scan.Result, &result) != nil {
		return nil, toolFailure("the scan returned an unreadable result; try again")
	}
	return worktreeScanResult(scan, result, req), nil
}

func worktreeScanFinished(scan *domain.WorktreeScan) bool {
	return scan.CompletedAt != nil || scan.Error != nil || len(scan.Result) > 0
}

func (t *AssistantTools) awaitWorktreeScan(ctx context.Context, ws uuid.UUID, scan *domain.WorktreeScan) (*domain.WorktreeScan, error) {
	if worktreeScanFinished(scan) {
		return scan, nil
	}
	wait := t.worktreeScanWait
	if wait <= 0 {
		wait = worktreeScanTimeout
	}
	deadline := time.Now().Add(wait)
	for {
		current, err := t.worktreeScans.GetWorktreeScan(ctx, ws, scan.ID)
		if err != nil {
			return nil, worktreeScanFailure(err)
		}
		if current != nil {
			scan = current
			if worktreeScanFinished(scan) {
				return scan, nil
			}
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, toolFailure("the machine did not answer within 20 s; it may be busy — try again")
		}
		timer := time.NewTimer(min(remaining, worktreeScanPollInterval))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

// worktreeScanFailure names why a scan could not run and what to do next.
func worktreeScanFailure(err error) error {
	switch {
	case errors.Is(err, repository.ErrWorktreeUnavailable):
		return toolFailure("the task's machine is not running or its checkout is gone. Next step: start the machine, or search the pushed code with source github")
	case errors.Is(err, repository.ErrConversationNotFound), errors.Is(err, repository.ErrAgentTaskUnauthorized):
		return toolFailure("only the task's creator can read its machine")
	case errors.Is(err, repository.ErrConversationInvalid):
		return toolFailure("the scan request is invalid for this task; check the path or pattern and try again")
	case errors.Is(err, repository.ErrWorktreeScanBusy):
		return toolFailure("too many worktree scans are already running on this machine; try again in a moment")
	}
	return err
}

// worktreeScanResult is the model-facing result: the checkout's identity, the
// kind's fields and the same byte budget as the GitHub scans.
func worktreeScanResult(scan *domain.WorktreeScan, result domain.WorktreeScanResult, req domain.WorktreeScanRequest) map[string]any {
	kind := req.Kind
	out := map[string]any{
		"machine": scan.MachineName, "repository": scan.Repository,
		"branch": result.Branch, "commit": result.Commit, "dirty": result.Dirty,
		"path": scan.WorkspacePath, "truncated": result.Truncated,
	}
	switch kind {
	case domain.WorktreeScanGrep:
		out["source"] = "task"
		out["matches"], out["truncated"] = trimWorktreeItems(result.Matches, result.Truncated, out)
	case domain.WorktreeScanGlob:
		out["source"] = "task"
		out["files"], out["truncated"] = trimWorktreeItems(result.Files, result.Truncated, out)
	case domain.WorktreeScanRead:
		out["source"] = "task"
		out["file"] = req.Path
		content, start, end, truncated := trimWorktreeContent(result.Content, result.StartLine, result.EndLine, result.Truncated)
		out["content"], out["start_line"], out["end_line"], out["total_lines"], out["truncated"] = content, start, end, result.TotalLines, truncated
	case domain.WorktreeScanStatus:
		out["changed"], out["truncated"] = trimWorktreeItems(result.Changed, result.Truncated, out)
		out["ahead"], out["behind"], out["last_subject"] = result.Ahead, result.Behind, result.LastSubject
	case domain.WorktreeScanList:
		out["worktrees"], out["truncated"] = trimWorktreeItems(result.Worktrees, result.Truncated, out)
	}
	return out
}

// trimWorktreeItems keeps list entries while the whole result stays within
// scanResultBytes, counting the header already in out.
func trimWorktreeItems[T any](items []T, truncated bool, out map[string]any) ([]T, bool) {
	header, _ := json.Marshal(out)
	budget := jsonBudget{limit: max(scanResultBytes-len(header)-1, 0)}
	kept := make([]T, 0, len(items))
	for _, item := range items {
		if !budget.admit(item) {
			return kept, true
		}
		kept = append(kept, item)
	}
	return kept, truncated
}

// trimWorktreeContent bounds a read to scanReadBytes and reports the last
// complete line the returned content reaches. A cut in the middle of one huge
// line keeps the readable prefix, marked with an ellipsis, and reports only
// the line it started on.
func trimWorktreeContent(content string, start, end int, truncated bool) (string, int, int, bool) {
	if len(content) <= scanReadBytes {
		return content, start, end, truncated
	}
	cut := content[:scanReadBytes]
	if index := strings.LastIndexByte(cut, '\n'); index >= 0 {
		cut = cut[:index]
		last := start + strings.Count(cut, "\n")
		if end > 0 {
			last = min(last, end)
		}
		return cut, start, last, true
	}
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut + " …", start, start, true
}

// taskSourceRequested reports a scan call whose source is the live worktree.
func taskSourceRequested(arguments string) bool {
	var probe struct {
		Source string `json:"source"`
	}
	return json.Unmarshal([]byte(arguments), &probe) == nil && strings.EqualFold(strings.TrimSpace(probe.Source), "task")
}

// linkedRepositoryHint names up to ten linked repositories so the model can
// retry a scan with a valid one. Only owners and admins may scan a repository
// by name, so only they are shown the names.
func (t *AssistantTools) linkedRepositoryHint(ctx context.Context, ws uuid.UUID, role string) string {
	if role != domain.RoleOwner && role != domain.RoleAdmin {
		return ""
	}
	lister, ok := t.scanRepos.(linkedRepositoryLister)
	if !ok {
		return ""
	}
	repos, err := lister.ListLinkedRepositories(ctx, ws)
	if err != nil || len(repos) == 0 {
		return ""
	}
	names := make([]string, 0, min(len(repos), linkedRepositoryHints))
	for _, repo := range repos {
		if len(names) == linkedRepositoryHints {
			break
		}
		names = append(names, repo.FullName)
	}
	return " Linked repositories: " + strings.Join(names, ", ") + "."
}
