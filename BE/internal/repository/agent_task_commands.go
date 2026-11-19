package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

// resolveScopeDefault returns the nearest issue → project → team → workspace
// value of an agent default column, or "" when none is configured.
func resolveScopeDefault(ctx context.Context, q sqlx.QueryerContext, ws, issue uuid.UUID, column string) (string, error) {
	if column != "agent_provider" && column != "agent_instructions" && column != "compose_file" && column != "pull_request_template" {
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

// resolvePreview returns the nearest scope's preview command and port.
func resolvePreview(ctx context.Context, q sqlx.QueryerContext, ws, issue uuid.UUID) (json.RawMessage, *int, error) {
	var command json.RawMessage
	var port *int
	err := q.QueryRowxContext(ctx, `SELECT s.preview_command,s.preview_port FROM dev_machine_scope_settings s
 JOIN issues i ON i.id=$2 AND i.workspace_id=s.workspace_id
 WHERE s.workspace_id=$1 AND s.preview_command IS NOT NULL
 AND (s.issue_id=i.id OR s.project_id=i.project_id OR s.team_id=i.team_id OR (s.issue_id IS NULL AND s.project_id IS NULL AND s.team_id IS NULL))
 ORDER BY CASE WHEN s.issue_id IS NOT NULL THEN 0 WHEN s.project_id IS NOT NULL THEN 1 WHEN s.team_id IS NOT NULL THEN 2 ELSE 3 END LIMIT 1`, ws, issue).Scan(&command, &port)
	if errors.Is(err, sql.ErrNoRows) {
		return json.RawMessage(`[]`), nil, nil
	}
	return command, port, err
}
