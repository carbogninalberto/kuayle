package service

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

func TestAssistantBulkTool(t *testing.T) {
	tool, ok := lookupAssistantTool("bulk_update_issues")
	require.True(t, ok)
	require.True(t, tool.approval)
	require.True(t, tool.write)
}

func TestAssistantBulkApprovalCloses25(t *testing.T) {
	f := newToolFixture(t)
	f.service.SetApprovals(NewAssistantApprovals(f.repo, nil))
	_, err := f.db.Exec(`INSERT INTO team_statuses(team_id,name,slug,category)
 SELECT id,'Done','done','completed' FROM teams WHERE workspace_id=$1`, f.ws)
	require.NoError(t, err)
	_, err = f.db.Exec(`INSERT INTO issues(workspace_id,team_id,number,identifier_text,title,creator_id,status_id)
 SELECT $1,t.id,n,'ENG-'||n,'Issue '||n,$2,s.id FROM teams t JOIN team_statuses s ON s.team_id=t.id CROSS JOIN generate_series(1,25) n WHERE t.workspace_id=$1 AND s.slug='backlog'`, f.ws, f.user)
	require.NoError(t, err)
	var changes []map[string]string
	for i := 1; i <= 25; i++ {
		changes = append(changes, map[string]string{"identifier": fmt.Sprintf("ENG-%d", i), "status": "done"})
	}
	args, err := json.Marshal(map[string]any{"changes": changes})
	require.NoError(t, err)
	a := f.attempt(t, "close these 25 issues")
	provider, _ := scriptedProvider(t, toolCallsStream("update_issue", `{"identifier":"ENG-1","priority":2}`, 9), toolCallStream("bulk_update_issues", string(args)), toolCallStream("update_issue", `{"identifier":"ENG-1","priority":3}`), finalAnswer)
	require.NoError(t, f.service.converse(f.ctx, a, provider.URL, "key", []chatCompletionMessage{{Role: "user", Content: "Close them"}}, func(string) error { return nil }))
	var count int
	require.NoError(t, f.db.Get(&count, `SELECT count(*) FROM conversation_tool_calls WHERE message_id=$1 AND status='failed' AND name='update_issue' AND error LIKE 'at most 10%'`, a.Message.ID))
	require.Equal(t, 1, count)
	var approvals []string
	require.NoError(t, f.db.Select(&approvals, `SELECT id FROM conversation_approvals WHERE conversation_id=$1 AND status='pending'`, a.Conversation.ID))
	require.Len(t, approvals, 1)
	closed := func() int {
		var n int
		require.NoError(t, f.db.Get(&n, `SELECT count(*) FROM issues WHERE workspace_id=$1 AND status='done'`, f.ws))
		return n
	}
	require.Zero(t, closed())
	result, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, uuid.MustParse(approvals[0]), true)
	require.NoError(t, err)
	require.Equal(t, "approved", result.Status)
	require.Equal(t, 25, closed())
}

func TestAssistantBulkValidatesBatchAndIndividualRows(t *testing.T) {
	rows := make([]map[string]any, 101)
	for i := range rows {
		rows[i] = map[string]any{"identifier": fmt.Sprintf("ENG-%d", i+1), "priority": 0}
	}
	for _, n := range []int{0, 100, 101} {
		data, err := json.Marshal(map[string]any{"changes": rows[:n]})
		require.NoError(t, err)
		payload, err := prepareBulkUpdate(string(data))
		if n == 100 {
			require.NoError(t, err)
			require.Len(t, payload.Changes, 100)
		} else {
			require.Error(t, err)
		}
	}
	_, err := prepareBulkUpdate(`{"changes":[{"identifier":"ENG-1","priority":0}],"results":[{"success":true}]}`)
	require.Error(t, err)
	tools := &AssistantTools{members: memberFake{domain.RoleMember}}
	for raw, reason := range map[string]string{
		`{"identifier":"ENG-1","title":"outside batch scope"}`:   "unknown argument title",
		`{"identifier":"ENG-1","priority":"urgent"}`:             "priority must be integer",
		`{"identifier":"ENG-1","priority":1,"assignee_id":null}`: "assignee_id must be string",
		`{"identifier":"ENG-1","assignee_id":"abc"}`:             "invalid assignee ID",
		`{"identifier":"ENG-1","label_ids":["abc"]}`:             "label_ids.0 must be a valid uuid",
		`{"identifier":"ENG-1","cycle_id":"abc"}`:                "invalid cycle or project ID",
		`{"identifier":"ENG-1"}`:                                 "at least 2 fields",
	} {
		result := tools.applyBulkChange(t.Context(), uuid.New(), uuid.New(), json.RawMessage(raw))
		require.Equal(t, "ENG-1", result.Identifier)
		require.False(t, result.Success)
		require.Contains(t, result.Error, reason)
	}
	tools.members = memberFake{domain.RoleGuest}
	result := tools.applyBulkChange(t.Context(), uuid.New(), uuid.New(), json.RawMessage(`{"identifier":"ENG-1","status":"done"}`))
	require.False(t, result.Success)
	require.Equal(t, noWorkspaceAccess, result.Error)
}

func TestAssistantBulkReportsPartialFailure(t *testing.T) {
	f := newToolFixture(t)
	f.service.SetApprovals(NewAssistantApprovals(f.repo, nil))
	f.service.tools.SetIssueLabels(repository.NewLabelRepository(f.db))
	project, cycle, label := uuid.New(), uuid.New(), uuid.New()
	var team uuid.UUID
	require.NoError(t, f.db.Get(&team, `SELECT id FROM teams WHERE workspace_id=$1`, f.ws))
	_, err := f.db.Exec(`INSERT INTO projects(id,workspace_id,name) VALUES($1,$2,'Test')`, project, f.ws)
	require.NoError(t, err)
	_, err = f.db.Exec(`INSERT INTO cycles(id,team_id,name,number,status) VALUES($1,$2,'Test',1,'active')`, cycle, team)
	require.NoError(t, err)
	_, err = f.db.Exec(`INSERT INTO labels(id,workspace_id,name,color) VALUES($1,$2,'Test','#112233')`, label, f.ws)
	require.NoError(t, err)
	_, err = f.db.Exec(`INSERT INTO issues(workspace_id,team_id,number,identifier_text,title,creator_id,status_id)
 SELECT $1,$2,n,'ENG-'||n,'Issue '||n,$3,s.id FROM team_statuses s CROSS JOIN generate_series(1,3) n WHERE s.team_id=$2 AND s.slug='backlog'`, f.ws, team, f.user)
	require.NoError(t, err)
	args := fmt.Sprintf(`{"changes":[{"identifier":"ENG-1","priority":1},{"identifier":"ENG-404","priority":2},{"identifier":"ENG-2","priority":9},{"identifier":"ENG-3","project_id":%q,"cycle_id":%q,"label_ids":[%q],"assignee_id":%q}]}`, project, cycle, label, f.user)
	a := f.attempt(t, "Change these issues")
	provider, _ := scriptedProvider(t, toolCallStream(bulkUpdateAction, args), finalAnswer)
	require.NoError(t, f.service.converse(f.ctx, a, provider.URL, "key", []chatCompletionMessage{{Role: "user", Content: "Update"}}, func(string) error { return nil }))
	var id uuid.UUID
	require.NoError(t, f.db.Get(&id, `SELECT id FROM conversation_approvals WHERE conversation_id=$1`, a.Conversation.ID))
	result, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, id, true)
	require.NoError(t, err)
	require.Equal(t, "failed", result.Status)
	require.Equal(t, "2 of 4 issue updates failed", *result.Error)
	var payload bulkUpdatePayload
	require.NoError(t, json.Unmarshal(result.Payload, &payload))
	require.Len(t, payload.Results, 4)
	require.Contains(t, payload.Results[2].Error, "priority must be at most 4")
	require.Equal(t, []bulkIssueResult{{Identifier: "ENG-1", Success: true}, {Identifier: "ENG-404", Error: "issue not found"}, {Identifier: "ENG-2", Error: payload.Results[2].Error}, {Identifier: "ENG-3", Success: true}}, payload.Results)
	var updated int
	require.NoError(t, f.db.Get(&updated, `SELECT count(*) FROM issues i JOIN issue_labels il ON il.issue_id=i.id
 WHERE i.workspace_id=$1 AND i.identifier_text='ENG-3' AND i.project_id=$2 AND i.cycle_id=$3 AND il.label_id=$4 AND i.assignee_id=$5`, f.ws, project, cycle, label, f.user))
	require.Equal(t, 1, updated)
}

func TestAssistantBulkLargeApprovalCanBeDenied(t *testing.T) {
	f := newToolFixture(t)
	f.service.SetApprovals(NewAssistantApprovals(f.repo, nil))
	changes := make([]map[string]any, 100)
	for i := range changes {
		changes[i] = map[string]any{"identifier": fmt.Sprintf("ENG-%d", i+1), "status": "done", "priority": 1, "assignee_id": f.user, "cycle_id": uuid.New(), "project_id": uuid.New(), "label_ids": []uuid.UUID{uuid.New()}}
	}
	data, err := json.Marshal(map[string]any{"changes": changes})
	require.NoError(t, err)
	require.Greater(t, len(data), assistantMaxToolInputBytes)
	a := f.attempt(t, "Update 100 issues")
	provider, _ := scriptedProvider(t, toolCallStream(bulkUpdateAction, string(data)), finalAnswer)
	require.NoError(t, f.service.converse(f.ctx, a, provider.URL, "key", []chatCompletionMessage{{Role: "user", Content: "Update"}}, func(string) error { return nil }))
	var id uuid.UUID
	require.NoError(t, f.db.Get(&id, `SELECT id FROM conversation_approvals WHERE conversation_id=$1 AND jsonb_array_length(payload->'changes')=100`, a.Conversation.ID))
	result, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, id, false)
	require.NoError(t, err)
	require.Equal(t, "denied", result.Status)
	var payload bulkUpdatePayload
	require.NoError(t, json.Unmarshal(result.Payload, &payload))
	require.Len(t, payload.Changes, 100)
	require.Empty(t, payload.Results)
}

func TestAssistantBulkRetryPreservesCompletedAndInterruptedRows(t *testing.T) {
	f := newToolFixture(t)
	f.service.SetApprovals(NewAssistantApprovals(f.repo, nil))
	_, err := f.db.Exec(`INSERT INTO issues(workspace_id,team_id,number,identifier_text,title,creator_id,priority,status_id)
 SELECT $1,t.id,n,'ENG-'||n,'Issue '||n,$2,4,s.id FROM teams t JOIN team_statuses s ON s.team_id=t.id CROSS JOIN generate_series(1,3) n WHERE t.workspace_id=$1`, f.ws, f.user)
	require.NoError(t, err)
	a := f.attempt(t, "Update priorities")
	provider, _ := scriptedProvider(t, toolCallStream(bulkUpdateAction, `{"changes":[{"identifier":"ENG-1","priority":1},{"identifier":"ENG-2","priority":1},{"identifier":"ENG-3","priority":1}]}`), finalAnswer)
	require.NoError(t, f.service.converse(f.ctx, a, provider.URL, "key", []chatCompletionMessage{{Role: "user", Content: "Update"}}, func(string) error { return nil }))
	var id uuid.UUID
	require.NoError(t, f.db.Get(&id, `UPDATE conversation_approvals SET status='approving',payload=payload||'{"results":[{"identifier":"ENG-1","success":true}],"in_flight":true}'::jsonb WHERE conversation_id=$1 RETURNING id`, a.Conversation.ID))
	result, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, id, true)
	require.NoError(t, err)
	var payload bulkUpdatePayload
	require.NoError(t, json.Unmarshal(result.Payload, &payload))
	require.Len(t, payload.Results, 3)
	require.Equal(t, "ENG-2", payload.Results[1].Identifier)
	require.Contains(t, payload.Results[1].Error, "interrupted")
	require.True(t, payload.Results[2].Success)
	var priorities []int
	require.NoError(t, f.db.Select(&priorities, `SELECT priority FROM issues WHERE workspace_id=$1 ORDER BY number`, f.ws))
	require.Equal(t, []int{4, 4, 1}, priorities)
}
