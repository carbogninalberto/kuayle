package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// toolCallsStream requests count parallel calls of one tool in a single round.
func toolCallsStream(name, arguments string, count int) string {
	var b strings.Builder
	for i := range count {
		fmt.Fprintf(&b, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":%d,\"id\":\"call_%d\",\"function\":{\"name\":%q,\"arguments\":%q}}]}}]}\n\n", i, i, name, arguments)
	}
	b.WriteString("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n")
	return b.String()
}

func TestAssistantReplyRunsUpToFourHundredToolCalls(t *testing.T) {
	require.Equal(t, 400, assistantMaxToolCalls)
	require.Equal(t, 10, assistantMaxChangesPerReply)
	require.GreaterOrEqual(t, assistantMaxToolRounds, assistantMaxToolCalls, "every call can run in its own round")
	f := newToolFixture(t)
	a := f.attempt(t, "Search everything")
	rounds := (assistantMaxToolCalls + assistantMaxRoundToolCalls - 1) / assistantMaxRoundToolCalls
	responses := []string{}
	for range rounds {
		responses = append(responses, toolCallsStream("search_issues", `{"query":"dark"}`, assistantMaxRoundToolCalls))
	}
	provider, requests := scriptedProvider(t, append(responses, finalAnswer)...)
	require.NoError(t, f.service.converse(f.ctx, a, provider.URL, "key", []chatCompletionMessage{{Role: "user", Content: "Search"}}, func(string) error { return nil }))
	require.Len(t, *requests, rounds+1)
	require.NotEmpty(t, (*requests)[rounds-1]["tools"])
	require.Nil(t, (*requests)[rounds]["tools"], "once the call budget is spent the model must answer")
	var recorded int
	require.NoError(t, f.db.Get(&recorded, `SELECT count(*) FROM conversation_tool_calls WHERE message_id=$1`, a.Message.ID))
	require.Equal(t, assistantMaxToolCalls, recorded, "calls beyond the limit are refused without running")
	history := (*requests)[rounds]["messages"].([]any)
	last := history[len(history)-1].(map[string]any)
	require.Contains(t, last["content"], "tool call limit reached")
	body, err := json.Marshal((*requests)[rounds]["messages"])
	require.NoError(t, err)
	require.Less(t, len(body), assistantContextBytes+assistantMaxToolCalls*512, "the context sent per round stays bounded")
}

func TestAssistantReplyMakesAtMostTenIssueChanges(t *testing.T) {
	f := newToolFixture(t)
	var team uuid.UUID
	require.NoError(t, f.db.Get(&team, `SELECT id FROM teams WHERE workspace_id=$1`, f.ws))
	_, err := f.db.Exec(`INSERT INTO issues(workspace_id,team_id,number,identifier_text,title,creator_id,status_id) VALUES($1,$2,1,'ENG-1','Old title',$3,(SELECT id FROM team_statuses WHERE team_id=$2 LIMIT 1))`, f.ws, team, f.user)
	require.NoError(t, err)
	a := f.attempt(t, "Bump the priority a lot")
	provider, requests := scriptedProvider(t, toolCallsStream("update_issue", `{"identifier":"ENG-1","priority":2}`, assistantMaxChangesPerReply+1), finalAnswer)
	require.NoError(t, f.service.converse(f.ctx, a, provider.URL, "key", []chatCompletionMessage{{Role: "user", Content: "Update"}}, func(string) error { return nil }))
	var completed, failed int
	require.NoError(t, f.db.Get(&completed, `SELECT count(*) FROM conversation_tool_calls WHERE message_id=$1 AND status='completed'`, a.Message.ID))
	require.NoError(t, f.db.Get(&failed, `SELECT count(*) FROM conversation_tool_calls WHERE message_id=$1 AND status='failed' AND error LIKE 'at most 10 issue changes%'`, a.Message.ID))
	require.Equal(t, assistantMaxChangesPerReply, completed)
	require.Equal(t, 1, failed)
	history := (*requests)[1]["messages"].([]any)
	require.Contains(t, history[len(history)-1].(map[string]any)["content"], "at most 10 issue changes can be made per reply")
}

func TestAssistantListAgentWorkReportsWorkspaceTasksWithTheUsersAccess(t *testing.T) {
	f := newToolFixture(t)
	url, review := "https://github.com/acme/app/pull/7", "changes_requested"
	work := &mergeWorkFake{recent: []repository.AgentWorkItem{{Identifier: "ENG-7", Title: strings.Repeat("t", 400), IssueAgentWork: repository.IssueAgentWork{TaskID: uuid.New(), State: "awaiting_review", Reason: strings.Repeat("r", 3000), Provider: "codex", Attempt: 2, PullRequestURL: &url, Review: &review}}}}
	f.service.tools.SetAgentWork(work)
	found := false
	for _, spec := range f.service.tools.specs() {
		found = found || spec.Function.Name == "list_agent_work"
	}
	require.True(t, found, "list_agent_work is offered once agent work is wired")
	output, issue, err := f.service.tools.run(f.ctx, f.ws, f.user, "list_agent_work", `{"state":"awaiting_review","limit":5}`)
	require.NoError(t, err)
	require.Nil(t, issue)
	require.Equal(t, []string{"awaiting_review/5"}, work.asked)
	data, err := json.Marshal(output)
	require.NoError(t, err)
	require.Contains(t, string(data), `"identifier":"ENG-7"`)
	require.Contains(t, string(data), `"review":"changes_requested"`)
	require.Contains(t, string(data), `"pull_request_url":"https://github.com/acme/app/pull/7"`)
	require.NotContains(t, string(data), strings.Repeat("r", 600), "long reasons are shortened")

	_, _, err = f.service.tools.run(f.ctx, f.ws, f.user, "list_agent_work", `{}`)
	require.NoError(t, err)
	require.Equal(t, "/50", work.asked[1], "the default lists the 50 most recent tasks")
	for arguments, message := range map[string]string{
		`{"state":"sleeping"}`: "invalid state",
		`{"limit":51}`:         "limit must be 1 to 50",
		`{"creator_id":"x"}`:   "invalid arguments",
	} {
		_, _, err = f.service.tools.run(f.ctx, f.ws, f.user, "list_agent_work", arguments)
		require.EqualError(t, err, message, arguments)
	}

	many := make([]repository.AgentWorkItem, 50)
	for i := range many {
		many[i] = work.recent[0]
		many[i].Identifier = fmt.Sprintf("ENG-%d", i+1)
	}
	work.recent = many
	output, _, err = f.service.tools.run(f.ctx, f.ws, f.user, "list_agent_work", `{}`)
	require.NoError(t, err)
	data, err = json.Marshal(output)
	require.NoError(t, err)
	require.LessOrEqual(t, len(data), assistantToolOutputBytes, "a full list fits the tool result bound")
	require.Contains(t, string(data), `"truncated":true`)

	_, err = f.db.Exec(`UPDATE workspace_members SET role='guest' WHERE workspace_id=$1 AND user_id=$2`, f.ws, f.user)
	require.NoError(t, err)
	_, _, err = f.service.tools.run(f.ctx, f.ws, f.user, "list_agent_work", `{}`)
	require.EqualError(t, err, "you no longer have access to this workspace")
}

func TestAssistantLinkRepositoryUsesTheDevelopmentSettingRules(t *testing.T) {
	f := newToolFixture(t)
	tools := f.service.tools
	tool, ok := lookupAssistantTool("link_repository")
	require.True(t, ok)
	require.True(t, tool.write, "linking counts against the reply's issue changes")
	_, _, err := tools.run(f.ctx, f.ws, f.user, "link_repository", `{"identifier":"ENG-1","repository":"acme/app"}`)
	require.EqualError(t, err, "unknown tool", "unavailable until repositories are wired")
	tools.SetRepositories(repository.NewDevMachineRepository(f.db))

	var team uuid.UUID
	require.NoError(t, f.db.Get(&team, `SELECT id FROM teams WHERE workspace_id=$1`, f.ws))
	project, installation, app, secret, inactive := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	_, err = f.db.Exec(`INSERT INTO projects(id,workspace_id,name) VALUES($1,$2,'Web')`, project, f.ws)
	require.NoError(t, err)
	var issue uuid.UUID
	require.NoError(t, f.db.Get(&issue, `INSERT INTO issues(workspace_id,team_id,number,identifier_text,title,creator_id,status_id,project_id) VALUES($1,$2,1,'ENG-1','Dark mode',$3,(SELECT id FROM team_statuses WHERE team_id=$2 LIMIT 1),$4) RETURNING id`, f.ws, team, f.user, project))
	_, err = f.db.Exec(`INSERT INTO issues(workspace_id,team_id,number,identifier_text,title,creator_id,status_id) VALUES($1,$2,2,'ENG-2','No project',$3,(SELECT id FROM team_statuses WHERE team_id=$2 LIMIT 1))`, f.ws, team, f.user)
	require.NoError(t, err)
	_, err = f.db.Exec(`INSERT INTO github_installations(id,workspace_id,installation_id,account_login,account_type,installed_by) VALUES($1,$2,1,'acme','Organization',$3)`, installation, f.ws, f.user)
	require.NoError(t, err)
	_, err = f.db.Exec(`INSERT INTO github_repos(id,installation_id,workspace_id,github_repo_id,full_name,default_branch,is_active) VALUES($1,$4,$5,1,'acme/app','main',TRUE),($2,$4,$5,2,'acme/secret','main',TRUE),($3,$4,$5,3,'acme/old','main',FALSE)`, app, secret, inactive, installation, f.ws)
	require.NoError(t, err)
	_, err = f.db.Exec(`INSERT INTO dev_machine_workspace_policies(workspace_id,enabled,allowed_repositories) VALUES($1,TRUE,'["acme/app","acme/old"]')`, f.ws)
	require.NoError(t, err)
	_, err = f.db.Exec(`INSERT INTO dev_machine_scope_settings(workspace_id,issue_id,test_command) VALUES($1,$2,'["go","test","./..."]')`, f.ws, issue)
	require.NoError(t, err)
	link := func(arguments string) (any, *uuid.UUID, error) {
		return tools.run(f.ctx, f.ws, f.user, "link_repository", arguments)
	}

	output, ref, err := link(`{"identifier":"eng-1","repository":"Acme/App"}`)
	require.NoError(t, err)
	require.Equal(t, issue, *ref)
	require.Equal(t, map[string]any{"identifier": "ENG-1", "scope": "issue", "repository": "acme/app", "base_branch": "main"}, output)
	var setting struct {
		Repo   uuid.UUID `db:"github_repo_id"`
		Branch string    `db:"base_branch"`
		Test   string    `db:"test_command"`
	}
	require.NoError(t, f.db.Get(&setting, `SELECT github_repo_id,base_branch,test_command::text FROM dev_machine_scope_settings WHERE issue_id=$1`, issue))
	require.Equal(t, app, setting.Repo)
	require.Equal(t, "main", setting.Branch)
	require.JSONEq(t, `["go","test","./..."]`, setting.Test, "other development settings are kept")
	_, _, err = link(`{"identifier":"ENG-1","repository":"acme/app","base_branch":"release/1.2"}`)
	require.NoError(t, err)
	require.NoError(t, f.db.Get(&setting, `SELECT github_repo_id,base_branch,test_command::text FROM dev_machine_scope_settings WHERE issue_id=$1`, issue))
	require.Equal(t, "release/1.2", setting.Branch)

	for arguments, message := range map[string]string{
		`{"identifier":"ENG-1","repository":"acme/secret"}`:                   `repository "acme/secret" is not allowed by the workspace's Dev Machine policy`,
		`{"identifier":"ENG-1","repository":"acme/old"}`:                      `repository "acme/old" is not linked to this workspace's GitHub installation`,
		`{"identifier":"ENG-1","repository":"other/app"}`:                     `repository "other/app" is not linked to this workspace's GitHub installation`,
		`{"identifier":"ENG-1","repository":"acme/app","base_branch":"../x"}`: "invalid base branch",
		`{"identifier":"ENG-404","repository":"acme/app"}`:                    "issue not found",
		`{"identifier":"ENG-1","repository":"acme/app","scope":"project"}`:    "only workspace owners and admins can set a project's repository",
		`{"identifier":"ENG-1","repository":"acme/app","scope":"team"}`:       "invalid arguments",
		`{"identifier":"ENG-1","repository":"acme/app","github_repo_id":"x"}`: "invalid arguments",
	} {
		_, _, err := link(arguments)
		require.EqualError(t, err, message, arguments)
	}

	_, err = f.db.Exec(`UPDATE workspace_members SET role='admin' WHERE workspace_id=$1 AND user_id=$2`, f.ws, f.user)
	require.NoError(t, err)
	_, _, err = link(`{"identifier":"ENG-2","repository":"acme/app","scope":"project"}`)
	require.EqualError(t, err, "this issue has no project")
	output, _, err = link(`{"identifier":"ENG-1","repository":"acme/app","scope":"project"}`)
	require.NoError(t, err)
	require.Equal(t, "project", output.(map[string]any)["scope"])
	require.NoError(t, f.db.Get(&setting.Repo, `SELECT github_repo_id FROM dev_machine_scope_settings WHERE project_id=$1`, project))
	require.Equal(t, app, setting.Repo)

	_, err = f.db.Exec(`UPDATE workspace_members SET role='guest' WHERE workspace_id=$1 AND user_id=$2`, f.ws, f.user)
	require.NoError(t, err)
	_, _, err = link(`{"identifier":"ENG-1","repository":"acme/app"}`)
	require.EqualError(t, err, "you no longer have access to this workspace")
}
