package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
)

// ErrAssistantToolSettingDenied: only current workspace owners and admins can
// switch assistant tools on or off.
var ErrAssistantToolSettingDenied = errors.New("assistant tool settings are for workspace owners and admins")

// AssistantToolSettingsRepository stores which built-in assistant tools each
// workspace has turned off. No row means the tool is enabled, except for the
// DefaultOffAssistantTools, which are off until an owner or admin turns them on.
type AssistantToolSettingsRepository struct{ db *sqlx.DB }

func NewAssistantToolSettingsRepository(db *sqlx.DB) *AssistantToolSettingsRepository {
	return &AssistantToolSettingsRepository{db}
}

// DefaultOffAssistantTools are off in a workspace until an owner or admin
// turns them on: spawn_research_subagents spends the workspace's AI provider
// budget on several model calls at once.
var DefaultOffAssistantTools = []string{"spawn_research_subagents"}

// Disabled returns the names of the workspace's disabled tools: those turned
// off, and the default-off tools nobody turned on.
func (r *AssistantToolSettingsRepository) Disabled(ctx context.Context, ws uuid.UUID) (map[string]bool, error) {
	var names []string
	if err := r.db.SelectContext(ctx, &names, `SELECT tool_name FROM assistant_tool_settings WHERE workspace_id=$1 AND NOT enabled
		UNION SELECT name FROM unnest($2::text[]) AS name
		WHERE NOT EXISTS (SELECT 1 FROM assistant_tool_settings WHERE workspace_id=$1 AND tool_name=name AND enabled)`, ws, pq.Array(DefaultOffAssistantTools)); err != nil {
		return nil, err
	}
	disabled := make(map[string]bool, len(names))
	for _, name := range names {
		disabled[name] = true
	}
	return disabled, nil
}

// Set records a choice. The user's current owner or admin role is checked in
// the same statement, so a role changed concurrently cannot slip through.
func (r *AssistantToolSettingsRepository) Set(ctx context.Context, ws, user uuid.UUID, name string, enabled bool) error {
	result, err := r.db.ExecContext(ctx, `INSERT INTO assistant_tool_settings(workspace_id,tool_name,enabled,updated_by)
		SELECT $1,$2,$3,$4 WHERE EXISTS (SELECT 1 FROM workspace_members WHERE workspace_id=$1 AND user_id=$4 AND role IN ('owner','admin'))
		ON CONFLICT(workspace_id,tool_name) DO UPDATE SET enabled=EXCLUDED.enabled,updated_by=EXCLUDED.updated_by,updated_at=now()`, ws, name, enabled, user)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err == nil && rows == 0 {
		err = ErrAssistantToolSettingDenied
	}
	return err
}
