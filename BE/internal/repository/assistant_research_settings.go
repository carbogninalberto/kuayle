package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// ErrAssistantResearchSettingsDenied: only current workspace owners and admins
// can change the research settings.
var ErrAssistantResearchSettingsDenied = errors.New("assistant research settings are for workspace owners and admins")

// AssistantResearchSettingsRepository stores each workspace's research
// settings. No row means the defaults.
type AssistantResearchSettingsRepository struct{ db *sqlx.DB }

func NewAssistantResearchSettingsRepository(db *sqlx.DB) *AssistantResearchSettingsRepository {
	return &AssistantResearchSettingsRepository{db}
}

// Get returns the workspace's settings, or nil when it has none.
func (r *AssistantResearchSettingsRepository) Get(ctx context.Context, ws uuid.UUID) (*domain.AssistantResearchSettings, error) {
	var settings domain.AssistantResearchSettings
	err := r.db.GetContext(ctx, &settings, `SELECT workspace_id, search_provider, search_api_key_encrypted, subagent_model, subagent_max_parallel, updated_by, created_at, updated_at FROM assistant_research_settings WHERE workspace_id=$1`, ws)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &settings, nil
}

// Save writes the settings as user. The user's current owner or admin role is
// checked in the same statement, so a role changed concurrently cannot slip
// through.
func (r *AssistantResearchSettingsRepository) Save(ctx context.Context, settings *domain.AssistantResearchSettings, user uuid.UUID) error {
	err := r.db.QueryRowxContext(ctx, `INSERT INTO assistant_research_settings(workspace_id, search_provider, search_api_key_encrypted, subagent_model, subagent_max_parallel, updated_by)
		SELECT $1, $2, $3, $4, $5, $6 WHERE EXISTS (SELECT 1 FROM workspace_members WHERE workspace_id=$1 AND user_id=$6 AND role IN ('owner','admin'))
		ON CONFLICT (workspace_id) DO UPDATE SET search_provider=EXCLUDED.search_provider, search_api_key_encrypted=EXCLUDED.search_api_key_encrypted,
			subagent_model=EXCLUDED.subagent_model, subagent_max_parallel=EXCLUDED.subagent_max_parallel, updated_by=EXCLUDED.updated_by, updated_at=now()
		RETURNING created_at, updated_at`,
		settings.WorkspaceID, settings.SearchProvider, settings.SearchAPIKeyEncrypted, settings.SubagentModel, settings.SubagentMaxParallel, user).
		Scan(&settings.CreatedAt, &settings.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrAssistantResearchSettingsDenied
	}
	if err == nil {
		settings.UpdatedBy = &user
	}
	return err
}
