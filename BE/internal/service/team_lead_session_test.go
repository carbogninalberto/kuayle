package service

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// newPeerPipelineTask starts ENG-2 with the same workflow and owner as ENG-1.
func newPeerPipelineTask(t *testing.T, w *tickWorld) domain.AgentTask {
	t.Helper()
	f := w.f
	issue := uuid.New()
	w.exec(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,creator_id,status_id)
 SELECT $2,workspace_id,team_id,2,'ENG-2','Theme settings',creator_id,status_id FROM issues WHERE id=$1`, f.issue, issue)
	chat, err := repository.NewConversationRepository(f.db).OpenIssue(f.ctx, f.ws, issue)
	require.NoError(t, err)
	var workflow string
	require.NoError(t, f.db.Get(&workflow, `SELECT workflow_id FROM workflow_versions WHERE id=$1`, w.task().WorkflowVersionID))
	task, err := f.tasks.Create(f.ctx, f.ws, chat.ID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Add theme settings", Provider: "codex", WorkflowID: &workflow})
	require.NoError(t, err)
	return *task
}

func TestLeadResumesItsOwnSessionAcrossIssues(t *testing.T) {
	w := newTickWorld(t, withLead)
	roots := []uuid.UUID{w.root, newPeerPipelineTask(t, w).ID}
	w.agent = func(role string, _ int, _ domain.AgentTask) tickTurn {
		if role == domain.PipelineLeadRole {
			return tickTurn{final: "Read the room."}
		}
		return tickTurn{hold: true}
	}
	start := func(root uuid.UUID) domain.TaskMember {
		t.Helper()
		member, err := w.f.tasks.Repo.StartMemberTurn(w.f.ctx, root, domain.PipelineLeadRole, false)
		require.NoError(t, err)
		require.NotNil(t, member.MemberTaskID)
		return *member
	}
	leads := []domain.TaskMember{start(roots[0]), start(roots[1])}
	require.Equal(t, leads[0].ProfileID, leads[1].ProfileID)
	require.NotEqual(t, *leads[0].MemberTaskID, *leads[1].MemberTaskID)
	w.drain()

	// Launch both first turns on the same harness and machine. Claude names
	// each new session at launch, so its real ID is available for turn two.
	sessions := make([]string, len(leads))
	for i, lead := range leads {
		run := w.relaunch(*lead.MemberTaskID, "claude-code", "")
		require.NotNil(t, run.NativeSessionID)
		sessions[i] = *run.NativeSessionID
		w.exec(`UPDATE dev_machine_agent_runs SET native_session_id=$2,provider_id='claude-code' WHERE id=$1`, run.ID, sessions[i])
	}
	require.NotEqual(t, sessions[0], sessions[1])

	for i, root := range roots {
		next := start(root)
		require.Equal(t, leads[i].MemberTaskID, next.MemberTaskID)
		require.Equal(t, 2, next.Turns)
		run := w.relaunch(*next.MemberTaskID, "claude-code", "")
		var argv []string
		require.NoError(t, json.Unmarshal(run.CommandArgv, &argv))
		require.Subset(t, argv, []string{"--resume", sessions[i]})
		require.NotContains(t, argv, sessions[1-i])
	}
}
