package service

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// A real wake reads the other issue even before this issue has a pull request.
// Changing the board while the lead rests is not itself a reason to run.
func TestLeadWakesWithTheOtherIssuesOnItsBoard(t *testing.T) {
	w := leadAtReview(t, nil)
	root := w.task()
	peer := newPeerPipelineTask(t, w)
	w.exec(`INSERT INTO dev_machine_checkouts(id,workspace_id,machine_id,issue_id,github_repo_id,repository_full_name,base_branch,working_branch,workspace_path,status,scope_key)
 SELECT $2,workspace_id,machine_id,$3,github_repo_id,repository_full_name,base_branch,'kuayle/eng-2','/workspace/tasks/eng-2','ready',$4 FROM dev_machine_checkouts WHERE id=$1`,
		root.CheckoutID, peer.CheckoutID, peer.IssueID, peer.ID.String())
	w.exec(`UPDATE dev_machine_git_refs SET issue_id=$2 WHERE agent_run_id=$1`, root.RunID, root.IssueID)
	w.exec(`UPDATE dev_machine_agent_runs SET changed_files='["theme.go"]' WHERE id=$1`, root.RunID)
	// The peer has work in its checkout but hasn't published a PR.
	w.exec(`INSERT INTO dev_machine_agent_runs(workspace_id,machine_id,task_id,requested_by_user_id,provider_id,mode,prompt,command_argv,max_runtime_seconds,status,completed_at,changed_files)
 VALUES($1,$2,$3,$4,'codex','autonomous','Theme settings','["codex"]',60,'succeeded',now(),'["theme.go"]')`, w.f.ws, w.machine, peer.ID, w.f.user)
	hiddenLeadBoardTasks(t, w)

	w.agent = func(role string, _ int, _ domain.AgentTask) tickTurn {
		if role == domain.PipelineLeadRole {
			return tickTurn{hold: true}
		}
		return tickTurn{final: "Waiting for the theme design."}
	}
	// Let the peer's first writer finish and its lead receive the digest.
	w.drain()
	promptFor := func(id uuid.UUID) string {
		t.Helper()
		var prompt string
		require.NoError(t, w.f.db.Get(&prompt, `SELECT t.prompt FROM task_members m JOIN agent_tasks t ON t.id=m.member_task_id WHERE m.task_id=$1 AND m.role='lead'`, id))
		_, section, found := strings.Cut(prompt, "Repository board")
		require.True(t, found, "the lead's turn carries the board")
		_, fenced, found := strings.Cut(section, "~~~text\n")
		require.True(t, found)
		board, _, found := strings.Cut(fenced, "\n~~~\n")
		require.True(t, found)
		for _, hidden := range []string{"ENG-3", "EXT-4"} {
			require.NotContains(t, board, hidden)
		}
		return board
	}
	peerBoard := promptFor(peer.ID)
	for _, want := range []string{"ENG-1", "phase review", "merge position 1", "theme.go"} {
		require.Contains(t, peerBoard, want)
	}

	// A polling pass observes the new peer; ENG-1's reviewer is still working.
	w.exec(`UPDATE agent_tasks SET workflow_wake_at=now() WHERE id=$1`, root.ID)
	w.drain()
	require.Equal(t, 1, w.member(domain.PipelineLeadRole).Turns)
	_, err := w.f.tasks.Repo.PostRoomMessage(w.f.ctx, w.f.ws, root.ID, domain.TaskRoomPost{Kind: domain.RoomAsk, ToRoles: []string{domain.PipelineLeadRole}, Body: "What else is in progress?"})
	require.NoError(t, err)
	w.drain()
	require.Equal(t, 2, w.member(domain.PipelineLeadRole).Turns)
	rootBoard := promptFor(root.ID)
	for _, want := range []string{"ENG-2", "phase implement", "no pull request", "theme.go"} {
		require.Contains(t, rootBoard, want)
	}
}

// Both hidden tasks are active on acme/app. One belongs to another creator in
// this workspace; the other belongs to this creator in another workspace.
func hiddenLeadBoardTasks(t *testing.T, w *tickWorld) {
	t.Helper()
	root := w.task()
	otherUser, otherWS, otherTeam, otherMachine := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	w.exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Other owner','hash')`, otherUser, otherUser.String()+"@test.invalid")
	w.exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'admin')`, root.WorkspaceID, otherUser)
	w.exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Other workspace',$2,$3)`, otherWS, otherWS.String(), root.CreatorID)
	w.exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner')`, otherWS, root.CreatorID)
	w.exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'External','EXT')`, otherTeam, otherWS)
	w.exec(`INSERT INTO team_statuses(team_id,name,slug,category) VALUES($1,'Backlog','backlog','backlog')`, otherTeam)
	w.exec(`INSERT INTO dev_machines(id,workspace_id,created_by_user_id,routing_key,name,status,desired_status,machine_size,cpu_millis,memory_mb,disk_gb)
 VALUES($1,$2,$3,$4,'Other machine','running','running','small',1000,2048,20)`, otherMachine, otherWS, root.CreatorID, strings.ReplaceAll(otherMachine.String(), "-", ""))

	for i, scope := range []struct {
		id                      string
		ws, user, team, machine uuid.UUID
	}{
		{"ENG-3", root.WorkspaceID, otherUser, root.TeamID, w.machine},
		{"EXT-4", otherWS, root.CreatorID, otherTeam, otherMachine},
	} {
		issue := uuid.New()
		w.exec(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,creator_id,status_id)
 VALUES($1,$2,$3,$4,$5,'Private work',$6,(SELECT id FROM team_statuses WHERE team_id=$3 LIMIT 1))`,
			issue, scope.ws, scope.team, i+3, scope.id, scope.user)
		ctx := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: scope.ws, UserID: scope.user})
		chat, err := repository.NewConversationRepository(w.f.db).OpenIssue(ctx, scope.ws, issue)
		require.NoError(t, err)
		task, err := w.f.tasks.Repo.Create(ctx, scope.ws, chat.ID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Private work", Provider: "codex"})
		require.NoError(t, err)
		w.exec(`UPDATE agent_tasks SET state='awaiting_review',machine_id=$2 WHERE id=$1`, task.ID, scope.machine)
		w.exec(`INSERT INTO dev_machine_agent_runs(id,workspace_id,machine_id,task_id,requested_by_user_id,provider_id,mode,prompt,command_argv,max_runtime_seconds,status,completed_at)
 VALUES($1,$2,$3,$4,$5,'codex','autonomous','Private work','["codex"]',60,'succeeded',now())`, task.RunID, scope.ws, scope.machine, task.ID, scope.user)
		w.exec(`INSERT INTO dev_machine_git_refs(workspace_id,machine_id,agent_run_id,issue_id,ref_type,repository_full_name,ref_name,pull_request_number,url,source)
 VALUES($1,$2,$3,$4,'pull_request',$5,'private',$6,$7,'published')`, scope.ws, scope.machine, task.RunID, issue, tickRepository, i+20, fmt.Sprintf("https://github.com/%s/pull/%d", tickRepository, i+20))
		board, err := w.f.tasks.Board(ctx, scope.ws, tickRepository)
		require.NoError(t, err)
		require.Len(t, board, 1)
		require.Equal(t, scope.id, board[0].IssueIdentifier)
	}
}
