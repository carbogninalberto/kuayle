package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

type AISettingsRepository struct {
	db *sqlx.DB
}

func NewAISettingsRepository(db *sqlx.DB) *AISettingsRepository {
	return &AISettingsRepository{db: db}
}

func (r *AISettingsRepository) GetByWorkspaceID(ctx context.Context, workspaceID uuid.UUID) (*domain.AISettings, error) {
	var settings domain.AISettings
	err := r.db.GetContext(ctx, &settings, `SELECT * FROM ai_settings WHERE workspace_id = $1`, workspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &settings, err
}

func (r *AISettingsRepository) Upsert(ctx context.Context, settings *domain.AISettings) error {
	query := `INSERT INTO ai_settings (workspace_id, provider, base_url, model, api_key_encrypted, description_expand_prompt)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (workspace_id) DO UPDATE SET
			provider = EXCLUDED.provider,
			base_url = EXCLUDED.base_url,
			model = EXCLUDED.model,
			api_key_encrypted = EXCLUDED.api_key_encrypted,
			description_expand_prompt = EXCLUDED.description_expand_prompt,
			updated_at = NOW()
		RETURNING created_at, updated_at`
	return r.db.QueryRowContext(ctx, query,
		settings.WorkspaceID,
		settings.Provider,
		settings.BaseURL,
		settings.Model,
		settings.APIKeyEncrypted,
		settings.DescriptionExpandPrompt,
	).Scan(&settings.CreatedAt, &settings.UpdatedAt)
}
