package repository

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// A tree or show scan names the commit it reads and, for show, a clean path
// and a line range no longer than a page.
func TestTreeAndShowScanRequestsNameACommitAndACleanPath(t *testing.T) {
	commit := strings.Repeat("a", 40)
	for _, valid := range []domain.WorktreeScanRequest{
		{Kind: domain.WorktreeScanTree, Ref: "HEAD"},
		{Kind: domain.WorktreeScanTree, Ref: commit},
		{Kind: domain.WorktreeScanShow, Ref: commit, Path: "src/app.go", StartLine: 1, EndLine: domain.TaskFileLinesMax},
	} {
		require.NoError(t, ValidateWorktreeScanRequest(valid), valid)
	}
	for _, invalid := range []domain.WorktreeScanRequest{
		{Kind: domain.WorktreeScanTree},
		{Kind: domain.WorktreeScanTree, Ref: "main"},
		{Kind: domain.WorktreeScanTree, Ref: "HEAD~1"},
		{Kind: domain.WorktreeScanShow, Ref: commit, Path: "../outside", StartLine: 1, EndLine: 1},
		{Kind: domain.WorktreeScanShow, Ref: commit, Path: "./src/app.go", StartLine: 1, EndLine: 1},
		{Kind: domain.WorktreeScanShow, Ref: commit, Path: "/etc/passwd", StartLine: 1, EndLine: 1},
		{Kind: domain.WorktreeScanShow, Ref: commit, Path: "src/app.go", StartLine: 0, EndLine: 1},
		{Kind: domain.WorktreeScanShow, Ref: commit, Path: "src/app.go", StartLine: 1, EndLine: domain.TaskFileLinesMax + 1},
		{Kind: domain.WorktreeScanShow, Ref: "HEAD:src/app.go", Path: "src/app.go", StartLine: 1, EndLine: 1},
	} {
		require.ErrorIs(t, ValidateWorktreeScanRequest(invalid), ErrConversationInvalid, invalid)
	}
}

// Where a task's files are read is its creator's to ask, like every task
// read: another member (even an admin), the creator through another
// workspace and a request without access find nothing. The head is the
// published commit, or GitHub's head observed after the publication; a ref
// is either a commit Kuayle published or one an attempt recorded.
func TestTaskFileSourceIsTheCreatorsAndNamesThePublishedHead(t *testing.T) {
	db, repo, ctx, task := reviewableTask(t)
	_, err := db.Exec(`UPDATE dev_machine_agent_runs SET changed_files='["src/app.go","README.md"]' WHERE id=$1`, task.RunID)
	require.NoError(t, err)
	agentCommit := strings.Repeat("e", 40)
	_, err = db.Exec(`INSERT INTO dev_machine_git_refs(workspace_id,machine_id,agent_run_id,issue_id,ref_type,repository_full_name,ref_name,commit_sha)
 SELECT workspace_id,machine_id,id,$2,'commit','fixture/repo','kuayle/rec-1',$3 FROM dev_machine_agent_runs WHERE id=$1`, task.RunID, task.IssueID, agentCommit)
	require.NoError(t, err)

	source, err := repo.TaskFileSource(ctx, task.WorkspaceID, task.ID, "")
	require.NoError(t, err)
	require.Equal(t, "fixture/repo", source.Repository)
	require.Equal(t, publishedCommit, source.Head)
	require.Equal(t, []string{"src/app.go", "README.md"}, source.Changed)
	require.False(t, source.RefPublished || source.RefRecorded)

	source, err = repo.TaskFileSource(ctx, task.WorkspaceID, task.ID, publishedCommit)
	require.NoError(t, err)
	require.True(t, source.RefPublished)
	require.True(t, source.RefRecorded)
	source, err = repo.TaskFileSource(ctx, task.WorkspaceID, task.ID, agentCommit)
	require.NoError(t, err)
	require.False(t, source.RefPublished)
	require.True(t, source.RefRecorded)
	source, err = repo.TaskFileSource(ctx, task.WorkspaceID, task.ID, strings.Repeat("f", 40))
	require.NoError(t, err)
	require.False(t, source.RefPublished || source.RefRecorded, "a commit of no attempt of the task")

	// A maintainer's push GitHub reported after the publication is the head.
	moved := strings.Repeat("9", 40)
	_, err = db.Exec(`INSERT INTO pull_request_heads(workspace_id,repository_full_name,pull_request_number,head_sha,observed_at) VALUES($1,'fixture/repo',12,$2,$3)`, task.WorkspaceID, moved, time.Now().Add(time.Minute))
	require.NoError(t, err)
	source, err = repo.TaskFileSource(ctx, task.WorkspaceID, task.ID, "")
	require.NoError(t, err)
	require.Equal(t, moved, source.Head)

	member, outsider := uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{member, outsider} {
		_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Files','x')`, id, id.String()+"@example.test")
		require.NoError(t, err)
	}
	_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'admin')`, task.WorkspaceID, member)
	require.NoError(t, err)
	foreign := uuid.New()
	for name, denied := range map[string]struct {
		ctx context.Context
		ws  uuid.UUID
	}{
		"another member, even an admin":             {domain.WithAccess(context.Background(), domain.Access{WorkspaceID: task.WorkspaceID, UserID: member}), task.WorkspaceID},
		"a person outside the workspace":            {domain.WithAccess(context.Background(), domain.Access{WorkspaceID: task.WorkspaceID, UserID: outsider}), task.WorkspaceID},
		"the creator through another workspace":     {domain.WithAccess(context.Background(), domain.Access{WorkspaceID: foreign, UserID: task.CreatorID}), foreign},
		"the creator naming another workspace's id": {ctx, foreign},
		"no access at all":                          {context.Background(), task.WorkspaceID},
	} {
		_, err = repo.TaskFileSource(denied.ctx, denied.ws, task.ID, "")
		require.ErrorIs(t, err, ErrConversationNotFound, name)
	}
}

// Before anything is published there is no head: the files are read on the
// task's machine, in the repository of its checkout.
func TestTaskFileSourceOfAnUnpublishedTaskHasNoHead(t *testing.T) {
	_, repo, ctx, task, _ := publishingTask(t)
	source, err := repo.TaskFileSource(ctx, task.WorkspaceID, task.ID, "")
	require.NoError(t, err)
	require.Equal(t, "fixture/repo", source.Repository)
	require.Empty(t, source.Head)
	require.Empty(t, source.Changed)
}

// A tree scan's result may take up to 4 MiB (migration 210), enough for a
// listing of 30,000 paths; every other kind keeps 64 KiB.
func TestWorktreeScanResultsFitTheirKindsBound(t *testing.T) {
	db, _, ctx, task, _ := publishingTask(t)
	store := NewDevMachineRepository(db)
	scan := func(req domain.WorktreeScanRequest) uuid.UUID {
		t.Helper()
		created, err := store.CreateWorktreeScan(ctx, task.WorkspaceID, task.CreatorID, domain.WorktreeScanTarget{TaskID: &task.ID}, req)
		require.NoError(t, err)
		return created.ID
	}
	files := make([]string, domain.TaskFilesMax)
	for i := range files {
		files[i] = "src/a-typical-directory/and-a-typical-file-name-" + strings.Repeat("x", 60) + "-" + strconv.Itoa(i) + ".go"
	}
	listing, err := json.Marshal(domain.WorktreeScanResult{Commit: strings.Repeat("a", 40), Files: files})
	require.NoError(t, err)
	require.Greater(t, len(listing), 3<<20, "30,000 paths of about 120 bytes")
	tree := scan(domain.WorktreeScanRequest{Kind: domain.WorktreeScanTree, Ref: "HEAD", Limit: domain.TaskFilesMax})
	require.NoError(t, store.CompleteWorktreeScan(ctx, tree, listing, nil))
	stored, err := store.GetWorktreeScan(ctx, task.WorkspaceID, tree)
	require.NoError(t, err)
	var result domain.WorktreeScanResult
	require.NoError(t, json.Unmarshal(stored.Result, &result))
	require.Len(t, result.Files, domain.TaskFilesMax)

	tooLarge, err := json.Marshal(domain.WorktreeScanResult{Content: strings.Repeat("x", domain.WorktreeTreeResultMax)})
	require.NoError(t, err)
	require.Error(t, store.CompleteWorktreeScan(ctx, scan(domain.WorktreeScanRequest{Kind: domain.WorktreeScanTree, Ref: "HEAD"}), tooLarge, nil))
	page, err := json.Marshal(domain.WorktreeScanResult{Content: strings.Repeat("x", 100<<10)})
	require.NoError(t, err)
	show := scan(domain.WorktreeScanRequest{Kind: domain.WorktreeScanShow, Ref: "HEAD", Path: "a.txt", StartLine: 1, EndLine: 10})
	require.Error(t, store.CompleteWorktreeScan(ctx, show, page, nil), "a page keeps the 64 KiB bound")
	small, err := json.Marshal(domain.WorktreeScanResult{Content: "a"})
	require.NoError(t, err)
	require.NoError(t, store.CompleteWorktreeScan(ctx, show, small, nil))
}

// The number of files a task's listing holds is the workspace's Dev Machine
// policy setting: 30,000 unless an admin set another, from 100 to 30,000,
// and another workspace's setting never applies.
func TestTaskFileSourceCarriesItsWorkspacesFileLimit(t *testing.T) {
	db, repo, ctx, task, _ := publishingTask(t)
	source, err := repo.TaskFileSource(ctx, task.WorkspaceID, task.ID, "")
	require.NoError(t, err)
	require.Equal(t, domain.TaskFilesMax, source.FilesMax, "no policy row: the default")

	machines := NewDevMachineRepository(db)
	policy := func(ws uuid.UUID, limit int) *domain.DevMachineWorkspacePolicy {
		return &domain.DevMachineWorkspacePolicy{WorkspaceID: ws, Enabled: true, MaxConcurrentMachines: 5, MaxMachinesPerUser: 2, MaxDailyAgentRuns: 25, MaxDiskGB: 100,
			AllowedProviders: json.RawMessage(`["codex"]`), AllowedRepositories: json.RawMessage(`[]`), TaskFilesMax: limit}
	}
	other := uuid.New()
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Other files',$2,$3)`, other, other.String(), task.CreatorID)
	require.NoError(t, err)
	require.NoError(t, machines.UpsertPolicy(ctx, policy(other, 100)))
	source, err = repo.TaskFileSource(ctx, task.WorkspaceID, task.ID, "")
	require.NoError(t, err)
	require.Equal(t, domain.TaskFilesMax, source.FilesMax, "another workspace's setting does not apply")

	require.NoError(t, machines.UpsertPolicy(ctx, policy(task.WorkspaceID, 0)))
	stored, err := machines.GetPolicy(ctx, task.WorkspaceID)
	require.NoError(t, err)
	require.Equal(t, domain.TaskFilesMax, stored.TaskFilesMax, "a policy built without it stores the default")
	require.NoError(t, machines.UpsertPolicy(ctx, policy(task.WorkspaceID, 2500)))
	stored, err = machines.GetPolicy(ctx, task.WorkspaceID)
	require.NoError(t, err)
	require.Equal(t, 2500, stored.TaskFilesMax)
	source, err = repo.TaskFileSource(ctx, task.WorkspaceID, task.ID, "")
	require.NoError(t, err)
	require.Equal(t, 2500, source.FilesMax)
	other100, err := machines.GetPolicy(ctx, other)
	require.NoError(t, err)
	require.Equal(t, 100, other100.TaskFilesMax)

	for _, invalid := range []int{99, domain.TaskFilesMax + 1} {
		_, err = db.Exec(`UPDATE dev_machine_workspace_policies SET task_files_max=$2 WHERE workspace_id=$1`, task.WorkspaceID, invalid)
		require.Error(t, err, invalid)
	}
}
