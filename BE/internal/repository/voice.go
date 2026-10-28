package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// VoiceRepository stores each workspace's chosen speech-to-text model.
type VoiceRepository struct{ db *sqlx.DB }

func NewVoiceRepository(db *sqlx.DB) *VoiceRepository { return &VoiceRepository{db} }

// Model returns the workspace's model, or "off" when none was chosen.
func (r *VoiceRepository) Model(ctx context.Context, ws uuid.UUID) (string, error) {
	var model string
	err := r.db.GetContext(ctx, &model, `SELECT model FROM workspace_voice_settings WHERE workspace_id=$1`, ws)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.VoiceModelOff, nil
	}
	return model, err
}

// SetModel records the owner's choice.
func (r *VoiceRepository) SetModel(ctx context.Context, ws, user uuid.UUID, model string) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO workspace_voice_settings(workspace_id,model,updated_by) VALUES($1,$2,$3)
		ON CONFLICT(workspace_id) DO UPDATE SET model=EXCLUDED.model,updated_by=EXCLUDED.updated_by,updated_at=now()`, ws, model, user)
	return err
}
