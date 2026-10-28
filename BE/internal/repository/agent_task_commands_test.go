package repository

import (
	"encoding/json"
	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestAgentTaskCommandsSnapshotAndReplay(t *testing.T) {
	db, repo, ctx, initial, fixture := taskRecoveryFixture(t)
	ws, chat := initial.WorkspaceID, *initial.ConversationID
	setting := &domain.DevMachineScopeSetting{WorkspaceID: ws, TestCommand: rawTaskCommand(`["go","test","./..."]`)}
	require.NoError(t, fixture.repository.UpsertScopeSetting(ctx, setting))
	stored, err := fixture.repository.GetScopeSetting(ctx, ws, nil, nil, nil)
	require.NoError(t, err)
	require.JSONEq(t, string(*setting.TestCommand), string(*stored.TestCommand))
	input := domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Implement", Provider: "codex"}
	task, err := repo.Create(ctx, ws, chat, input)
	require.NoError(t, err)
	require.JSONEq(t, string(*setting.TestCommand), string(task.ValidationCommand))
	require.Empty(t, task.TestCommand)
	original := append(json.RawMessage(nil), task.ValidationCommand...)
	setting.TestCommand = rawTaskCommand(`["npm","test"]`)
	require.NoError(t, fixture.repository.UpsertScopeSetting(ctx, setting))
	replay, err := repo.Create(ctx, ws, chat, input)
	require.NoError(t, err)
	require.JSONEq(t, string(original), string(replay.ValidationCommand))
	changed := input
	changed.TestCommand = []string{}
	_, err = repo.ReplayCreate(ctx, ws, chat, changed)
	require.ErrorIs(t, err, ErrConversationConflict, "inherit and explicit no check are different intents")
	changed.RequestID = uuid.New()
	disabled, err := repo.Create(ctx, ws, chat, changed)
	require.NoError(t, err)
	require.JSONEq(t, `[]`, string(disabled.ValidationCommand))
	require.JSONEq(t, `[]`, string(*disabled.TestCommand))
	changed.RequestID = uuid.New()
	changed.TestCommand = []string{"npm", "run", "check"}
	override, err := repo.Create(ctx, ws, chat, changed)
	require.NoError(t, err)
	require.JSONEq(t, `["npm","run","check"]`, string(override.ValidationCommand))
	_, err = db.Exec(`UPDATE agent_tasks SET state='blocked' WHERE id=$1`, task.ID)
	require.NoError(t, err)
	retryInput := domain.AgentTaskRetryRequest{RequestID: uuid.New(), Generation: 1, Prompt: "Try again"}
	retry, err := repo.Retry(ctx, ws, task.ID, retryInput)
	require.NoError(t, err)
	require.JSONEq(t, string(*setting.TestCommand), string(retry.ValidationCommand), "new intent resolves current defaults")
	setting.TestCommand = rawTaskCommand(`["make","test"]`)
	require.NoError(t, fixture.repository.UpsertScopeSetting(ctx, setting))
	retryReplay, err := repo.ReplayRetry(ctx, ws, task.ID, retryInput)
	require.NoError(t, err)
	require.JSONEq(t, string(retry.ValidationCommand), string(retryReplay.ValidationCommand))
	retryInput.TestCommand = []string{}
	_, err = repo.ReplayRetry(ctx, ws, task.ID, retryInput)
	require.ErrorIs(t, err, ErrConversationConflict)
	// Old create receipts remain stable after retry, including their command.
	replay, err = repo.ReplayCreate(ctx, ws, chat, input)
	require.NoError(t, err)
	require.JSONEq(t, string(original), string(replay.ValidationCommand))
	changed.RequestID = uuid.New()
	changed.TestCommand = []string{"bad\x00command"}
	_, err = repo.Create(ctx, ws, chat, changed)
	require.ErrorIs(t, err, ErrConversationInvalid)
}

func rawTaskCommand(value string) *json.RawMessage { raw := json.RawMessage(value); return &raw }

func TestAgentTaskCommandInheritanceOrderAndExplicitNone(t *testing.T) {
	db, repo, ctx, initial, fixture := taskRecoveryFixture(t)
	project := uuid.New()
	_, err := db.Exec(`INSERT INTO projects(id,workspace_id,name) VALUES($1,$2,'Test commands')`, project, initial.WorkspaceID)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE issues SET project_id=$2 WHERE id=$1`, initial.IssueID, project)
	require.NoError(t, err)
	settings := []*domain.DevMachineScopeSetting{
		{WorkspaceID: initial.WorkspaceID, TestCommand: rawTaskCommand(`["workspace"]`)},
		{WorkspaceID: initial.WorkspaceID, TeamID: &initial.TeamID, TestCommand: rawTaskCommand(`["team"]`)},
		{WorkspaceID: initial.WorkspaceID, ProjectID: &project, TestCommand: rawTaskCommand(`["project"]`)},
		{WorkspaceID: initial.WorkspaceID, IssueID: &initial.IssueID, TestCommand: rawTaskCommand(`["issue"]`)},
	}
	for _, setting := range settings {
		require.NoError(t, fixture.repository.UpsertScopeSetting(ctx, setting))
	}
	create := func() *domain.AgentTask {
		task, err := repo.Create(ctx, initial.WorkspaceID, *initial.ConversationID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Implement", Provider: "codex"})
		require.NoError(t, err)
		return task
	}
	for i := len(settings) - 1; i >= 0; i-- {
		require.JSONEq(t, string(*settings[i].TestCommand), string(create().ValidationCommand))
		if i == 3 {
			settings[i].TestCommand = rawTaskCommand(`[]`)
			require.NoError(t, fixture.repository.UpsertScopeSetting(ctx, settings[i]))
			require.JSONEq(t, `[]`, string(create().ValidationCommand))
		}
		settings[i].TestCommand = nil
		require.NoError(t, fixture.repository.UpsertScopeSetting(ctx, settings[i]))
		stored, err := fixture.repository.GetScopeSetting(ctx, initial.WorkspaceID, settings[i].TeamID, settings[i].ProjectID, settings[i].IssueID)
		require.NoError(t, err)
		require.Nil(t, stored.TestCommand)
	}
	require.JSONEq(t, `[]`, string(create().ValidationCommand))
	// Corrupt/legacy internal configuration cannot become an executable command.
	_, err = db.Exec(`UPDATE dev_machine_scope_settings SET test_command='[1]' WHERE id=$1`, settings[0].ID)
	require.NoError(t, err)
	_, err = repo.Create(ctx, initial.WorkspaceID, *initial.ConversationID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Implement", Provider: "codex"})
	require.ErrorIs(t, err, ErrConversationInvalid)
}
