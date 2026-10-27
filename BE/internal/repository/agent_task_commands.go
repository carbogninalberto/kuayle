package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// Resolve inside the accepted-intent transaction, after current issue/chat
// authorization. NULL inherits; [] deliberately stops inheritance. The selected
// command is copied into the queued event, so replays never consult new defaults.
func resolveAgentTaskCommand(ctx context.Context, tx *sqlx.Tx, ws, issue uuid.UUID, input []string) (*json.RawMessage, json.RawMessage, error) {
	if domain.ValidateTestCommand(input) != nil {
		return nil, nil, ErrConversationInvalid
	}
	if input != nil {
		encoded, _ := json.Marshal(input)
		raw := json.RawMessage(encoded)
		return &raw, raw, nil
	}
	var raw json.RawMessage
	err := tx.GetContext(ctx, &raw, `SELECT s.test_command FROM dev_machine_scope_settings s
 JOIN issues i ON i.id=$2 AND i.workspace_id=s.workspace_id
 WHERE s.workspace_id=$1 AND s.test_command IS NOT NULL
 AND (s.issue_id=i.id OR s.project_id=i.project_id OR s.team_id=i.team_id OR (s.issue_id IS NULL AND s.project_id IS NULL AND s.team_id IS NULL))
 ORDER BY CASE WHEN s.issue_id IS NOT NULL THEN 0 WHEN s.project_id IS NOT NULL THEN 1 WHEN s.team_id IS NOT NULL THEN 2 ELSE 3 END LIMIT 1`, ws, issue)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, json.RawMessage(`[]`), nil
	}
	if err != nil {
		return nil, nil, err
	}
	var command []string
	if json.Unmarshal(raw, &command) != nil || command == nil || domain.ValidateTestCommand(command) != nil {
		return nil, nil, ErrConversationInvalid
	}
	return nil, raw, nil
}

func sameAgentTaskCommand(accepted *json.RawMessage, input []string) bool {
	if accepted == nil {
		return input == nil
	}
	var command []string
	return json.Unmarshal(*accepted, &command) == nil && (command == nil) == (input == nil) && slices.Equal(command, input)
}

// resolveScopeDefault returns the nearest issue → project → team → workspace
// value of an agent default column, or "" when none is configured.
func resolveScopeDefault(ctx context.Context, q sqlx.QueryerContext, ws, issue uuid.UUID, column string) (string, error) {
	if column != "agent_provider" && column != "agent_instructions" {
		return "", ErrConversationInvalid
	}
	var value string
	err := sqlx.GetContext(ctx, q, &value, `SELECT s.`+column+` FROM dev_machine_scope_settings s
 JOIN issues i ON i.id=$2 AND i.workspace_id=s.workspace_id
 WHERE s.workspace_id=$1 AND s.`+column+` IS NOT NULL
 AND (s.issue_id=i.id OR s.project_id=i.project_id OR s.team_id=i.team_id OR (s.issue_id IS NULL AND s.project_id IS NULL AND s.team_id IS NULL))
 ORDER BY CASE WHEN s.issue_id IS NOT NULL THEN 0 WHEN s.project_id IS NOT NULL THEN 1 WHEN s.team_id IS NOT NULL THEN 2 ELSE 3 END LIMIT 1`, ws, issue)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return value, err
}

// DefaultProvider returns the preferred coding agent configured for an issue.
func (r *AgentTaskRepository) DefaultProvider(ctx context.Context, ws, issue uuid.UUID) (string, error) {
	return resolveScopeDefault(ctx, r.db, ws, issue, "agent_provider")
}
