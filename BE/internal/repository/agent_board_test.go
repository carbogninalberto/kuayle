package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// addBoardTask adds a task awaiting review for ctx's actor on a new issue of
// team, whose attempt changed files and, when pr > 0, published that pull
// request on repository at commit.
func addBoardTask(t *testing.T, db *sqlx.DB, repo *AgentTaskRepository, ctx context.Context, team, machine uuid.UUID, number int, repository string, pr int, commit, files string) domain.AgentTask {
	t.Helper()
	access, _ := domain.AccessFromContext(ctx)
	ws, issue := access.WorkspaceID, uuid.New()
	_, err := db.Exec(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,creator_id,status_id) VALUES($1,$2,$3,$4,$5,'Board',$6,(SELECT id FROM team_statuses WHERE team_id=$3 LIMIT 1))`,
		issue, ws, team, number, fmt.Sprintf("REC-%d", number), access.UserID)
	require.NoError(t, err)
	chat, err := NewConversationRepository(db).OpenIssue(ctx, ws, issue)
	require.NoError(t, err)
	task, err := repo.Create(ctx, ws, chat.ID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Board", Provider: "codex"})
	require.NoError(t, err)
	var repositoryID uuid.UUID
	err = db.Get(&repositoryID, `SELECT id FROM github_repos WHERE workspace_id=$1 AND full_name=$2`, ws, repository)
	if errors.Is(err, sql.ErrNoRows) {
		err = db.Get(&repositoryID, `INSERT INTO github_repos(installation_id,workspace_id,github_repo_id,full_name,default_branch)
 SELECT id,workspace_id,$3,$2,'main' FROM github_installations WHERE workspace_id=$1 LIMIT 1 RETURNING id`, ws, repository, 1000+number)
	}
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO dev_machine_checkouts(id,workspace_id,machine_id,issue_id,github_repo_id,repository_full_name,base_branch,working_branch,workspace_path,status,scope_key) VALUES($1,$2,$3,$4,$5,$6,'main',$7,$8,'ready',$9)`,
		task.CheckoutID, ws, machine, issue, repositoryID, repository, fmt.Sprintf("kuayle/rec-%d", number), fmt.Sprintf("/workspace/tasks/rec-%d", number), task.ID.String())
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO dev_machine_agent_runs(id,workspace_id,machine_id,task_id,checkout_id,requested_by_user_id,provider_id,mode,prompt,command_argv,max_runtime_seconds,status,completed_at,changed_files) VALUES($1,$2,$3,$4,$5,$6,'codex','autonomous','Board','["codex"]',60,'succeeded',now(),$7)`,
		task.RunID, ws, machine, task.ID, task.CheckoutID, access.UserID, files)
	require.NoError(t, err)
	if pr > 0 {
		_, err = db.Exec(`INSERT INTO dev_machine_git_refs(workspace_id,machine_id,agent_run_id,issue_id,ref_type,repository_full_name,ref_name,commit_sha,pull_request_number,url,source)
 VALUES($1,$2,$3,$4,'commit',$5,'kuayle/board',$6,NULL,NULL,'published'),($1,$2,$3,$4,'pull_request',$5,NULL,NULL,$7,$8,'published')`,
			ws, machine, task.RunID, issue, repository, commit, pr, fmt.Sprintf("https://github.com/%s/pull/%d", repository, pr))
		require.NoError(t, err)
	}
	_, err = db.Exec(`UPDATE agent_tasks SET state='awaiting_review',machine_id=$2 WHERE id=$1`, task.ID, machine)
	require.NoError(t, err)
	return *task
}

func TestBoardListsTheActorsActiveTasksOfOneRepository(t *testing.T) {
	db, repo, ctx, task := reviewableTask(t)
	ws := task.WorkspaceID
	var machine uuid.UUID
	require.NoError(t, db.Get(&machine, `SELECT machine_id FROM agent_tasks WHERE id=$1`, task.ID))
	migration := "BE/migrations/000201_board.up.sql"
	_, err := db.Exec(`UPDATE dev_machine_agent_runs SET changed_files=$2 WHERE id=$1`, task.RunID, `["BE/internal/a.go","`+migration+`"]`)
	require.NoError(t, err)
	// CI passed on the published commit, and a person approved the merge
	// Kuayle is carrying out at the task's ship approval.
	_, err = repo.RecordSignal(context.Background(), ws, "fixture/repo", 12, ci("1", "success", publishedCommit))
	require.NoError(t, err)
	approved := time.Date(2026, 11, 24, 9, 0, 0, 0, time.UTC)
	wait := fmt.Sprintf(`{"approved":%q,"head":%q,"updates":0,"since":%q,"deadline":%q}`, publishedCommit, publishedCommit, approved.Format(time.RFC3339Nano), approved.Add(time.Hour).Format(time.RFC3339Nano))
	_, err = db.Exec(`INSERT INTO agent_task_workflow_steps(task_id,sequence,node_id,node_type,visit,reason,merge) VALUES($1,1,'ship','approval',1,'Ready to ship',$2)`, task.ID, wait)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE agent_tasks SET workflow_sequence=1 WHERE id=$1`, task.ID)
	require.NoError(t, err)

	second := addBoardTask(t, db, repo, ctx, task.TeamID, machine, 2, "fixture/repo", 13, strings.Repeat("b", 40), `["`+migration+`"]`)
	// A later attempt that recorded no files does not hide the branch's.
	_, err = db.Exec(`INSERT INTO dev_machine_agent_runs(workspace_id,machine_id,task_id,checkout_id,requested_by_user_id,provider_id,mode,prompt,command_argv,max_runtime_seconds,status,completed_at,created_at) VALUES($1,$2,$3,$4,$5,'codex','autonomous','Board','["codex"]',60,'failed',now(),now()+interval '1 minute')`,
		ws, machine, second.ID, second.CheckoutID, task.CreatorID)
	require.NoError(t, err)
	_, err = repo.RecordSignal(context.Background(), ws, "fixture/repo", 13, ci("2", "failure", strings.Repeat("b", 40)))
	require.NoError(t, err)
	addBoardTask(t, db, repo, ctx, task.TeamID, machine, 3, "other/repo", 14, strings.Repeat("c", 40), `["`+migration+`"]`)
	finished := addBoardTask(t, db, repo, ctx, task.TeamID, machine, 4, "fixture/repo", 15, strings.Repeat("d", 40), `["`+migration+`"]`)
	_, err = db.Exec(`UPDATE agent_tasks SET state='completed' WHERE id=$1`, finished.ID)
	require.NoError(t, err)
	unpublished := addBoardTask(t, db, repo, ctx, task.TeamID, machine, 5, "fixture/repo", 0, "", `[]`)

	board, err := repo.Board(ctx, ws, "Fixture/Repo")
	require.NoError(t, err)
	byIssue := map[string]domain.AgentBoardTask{}
	for _, entry := range board {
		byIssue[entry.IssueIdentifier] = entry
	}
	require.ElementsMatch(t, []string{"REC-1", "REC-2", "REC-5"}, keysOf(byIssue), "only the active tasks of that repository, matched without case")

	first := byIssue["REC-1"]
	require.Equal(t, task.ID, first.TaskID)
	require.Equal(t, "awaiting_review", first.State)
	require.Equal(t, "fixture/repo", first.Repository)
	require.Equal(t, 12, first.PullRequest)
	require.Equal(t, publishedCommit, first.Head)
	require.Equal(t, "main", first.Base)
	require.Equal(t, []string{"BE/internal/a.go", migration}, first.ChangedFiles)
	require.Equal(t, "passing", first.CI)
	require.NotNil(t, first.ApprovedAt)
	require.True(t, approved.Equal(*first.ApprovedAt), first.ApprovedAt)

	other := byIssue["REC-2"]
	require.Equal(t, second.ID, other.TaskID)
	require.Equal(t, 13, other.PullRequest)
	require.Equal(t, []string{migration}, other.ChangedFiles)
	require.Equal(t, "failing", other.CI)
	require.Nil(t, other.ApprovedAt)

	draft := byIssue["REC-5"]
	require.Equal(t, unpublished.ID, draft.TaskID)
	require.Zero(t, draft.PullRequest)
	require.Empty(t, draft.Head)
	require.Empty(t, draft.CI)

	// GitHub reporting a newer head (an update of the branch) replaces the
	// published commit, and CI is read for that head.
	updated := strings.Repeat("e", 40)
	_, err = db.Exec(`INSERT INTO pull_request_heads(workspace_id,repository_full_name,pull_request_number,head_sha,observed_at) VALUES($1,'fixture/repo',12,$2,now()+interval '1 minute')`, ws, updated)
	require.NoError(t, err)
	// A merge whose attempt failed waits for a person again: not approved.
	_, err = db.Exec(`UPDATE agent_task_workflow_steps SET merge=merge||'{"failure":"The merge was refused"}' WHERE task_id=$1`, task.ID)
	require.NoError(t, err)
	board, err = repo.Board(ctx, ws, "fixture/repo")
	require.NoError(t, err)
	index := slices.IndexFunc(board, func(entry domain.AgentBoardTask) bool { return entry.TaskID == task.ID })
	require.GreaterOrEqual(t, index, 0)
	require.Equal(t, updated, board[index].Head)
	require.Empty(t, board[index].CI, "no suite has reported on the new head")
	require.Nil(t, board[index].ApprovedAt)
}

func TestBoardHasTheVisibilityOfTheTaskQueue(t *testing.T) {
	db, repo, ctx, task := reviewableTask(t)
	ws := task.WorkspaceID
	listed := func(ctx context.Context, ws uuid.UUID) ([]uuid.UUID, error) {
		board, err := repo.Board(ctx, ws, "fixture/repo")
		var ids []uuid.UUID
		for _, entry := range board {
			ids = append(ids, entry.TaskID)
		}
		return ids, err
	}
	ids, err := listed(ctx, ws)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{task.ID}, ids, "the creator sees their task")

	// Another member of the workspace does not see someone else's task.
	member := uuid.New()
	_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Board member','hash')`, member, member.String()+"@test.invalid")
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'admin')`, ws, member)
	require.NoError(t, err)
	ids, err = listed(domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: member}), ws)
	require.NoError(t, err)
	require.Empty(t, ids, "tasks are private to their creator, even from an admin")

	// Someone outside the workspace is refused.
	_, err = listed(domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: uuid.New()}), ws)
	require.ErrorIs(t, err, ErrConversationNotFound)

	// Another workspace: access to it reads nothing of this one, and access
	// to this one cannot be used for it.
	elsewhere := uuid.New()
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Elsewhere',$2,$3)`, elsewhere, "elsewhere-"+elsewhere.String(), member)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner')`, elsewhere, task.CreatorID)
	require.NoError(t, err)
	ids, err = listed(domain.WithAccess(context.Background(), domain.Access{WorkspaceID: elsewhere, UserID: task.CreatorID}), elsewhere)
	require.NoError(t, err)
	require.Empty(t, ids)
	_, err = listed(ctx, elsewhere)
	require.ErrorIs(t, err, ErrConversationNotFound)

	// The team becomes private and the creator, now an ordinary member, is
	// not in it (the guard against private teams in a workspace with
	// machines is not what is tested here).
	_, err = db.Exec(`ALTER TABLE teams DISABLE TRIGGER USER`)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE workspace_members SET role='member' WHERE workspace_id=$1 AND user_id=$2`, ws, task.CreatorID)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE teams SET is_private=true WHERE id=$1`, task.TeamID)
	require.NoError(t, err)
	ids, err = listed(ctx, ws)
	require.NoError(t, err)
	require.Empty(t, ids, "a private team's task is hidden from who is not in the team")
	_, err = db.Exec(`INSERT INTO team_members(team_id,user_id) VALUES($1,$2)`, task.TeamID, task.CreatorID)
	require.NoError(t, err)
	ids, err = listed(ctx, ws)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{task.ID}, ids, "and listed again for a member of it")
}

func keysOf[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	return keys
}
