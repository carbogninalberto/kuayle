package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// GitHubBotRepository stores each workspace's GitHub bot identity and abilities.
type GitHubBotRepository struct{ db *sqlx.DB }

func NewGitHubBotRepository(db *sqlx.DB) *GitHubBotRepository { return &GitHubBotRepository{db} }

type githubBotRow struct {
	domain.GitHubBotSettings
	Abilities []byte `db:"abilities"`
}

const githubBotColumns = `workspace_id,display_name,avatar_url,avatar_file,abilities,mention_harness,mention_model,updated_at`

// Get returns the workspace's settings; a workspace that saved none gets the defaults.
func (r *GitHubBotRepository) Get(ctx context.Context, ws uuid.UUID) (*domain.GitHubBotSettings, error) {
	var row githubBotRow
	err := r.db.GetContext(ctx, &row, `SELECT `+githubBotColumns+` FROM workspace_github_bot_settings WHERE workspace_id=$1`, ws)
	if errors.Is(err, sql.ErrNoRows) {
		return &domain.GitHubBotSettings{WorkspaceID: ws, Abilities: map[string]bool{}}, nil
	}
	if err != nil {
		return nil, err
	}
	return row.settings()
}

func (row githubBotRow) settings() (*domain.GitHubBotSettings, error) {
	settings := row.GitHubBotSettings
	settings.Abilities = map[string]bool{}
	if len(row.Abilities) > 0 {
		if err := json.Unmarshal(row.Abilities, &settings.Abilities); err != nil {
			return nil, err
		}
	}
	return &settings, nil
}

// Update applies change to the workspace's current settings under a row lock
// and stores the result. It returns the saved settings and the uploaded
// avatar file they no longer reference, which the caller deletes after the
// commit; concurrent updates are serialized, so a file is never deleted while
// still referenced. An error from change aborts without writing.
func (r *GitHubBotRepository) Update(ctx context.Context, ws, user uuid.UUID, change func(*domain.GitHubBotSettings) error) (*domain.GitHubBotSettings, *string, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO workspace_github_bot_settings(workspace_id,updated_by) VALUES($1,$2) ON CONFLICT(workspace_id) DO NOTHING`, ws, user); err != nil {
		return nil, nil, err
	}
	var row githubBotRow
	if err = tx.GetContext(ctx, &row, `SELECT `+githubBotColumns+` FROM workspace_github_bot_settings WHERE workspace_id=$1 FOR UPDATE`, ws); err != nil {
		return nil, nil, err
	}
	current, err := row.settings()
	if err != nil {
		return nil, nil, err
	}
	previous := current.AvatarFile
	if err = change(current); err != nil {
		return nil, nil, err
	}
	abilities := map[string]bool{}
	for name, enabled := range current.Abilities {
		if domain.IsGitHubBotAbility(name) {
			abilities[name] = enabled
		}
	}
	encoded, err := json.Marshal(abilities)
	if err != nil {
		return nil, nil, err
	}
	err = tx.GetContext(ctx, &row, `UPDATE workspace_github_bot_settings SET display_name=$2,avatar_url=$3,avatar_file=$4,abilities=$5,mention_harness=$6,mention_model=$7,updated_by=$8,updated_at=now()
 WHERE workspace_id=$1 RETURNING `+githubBotColumns, ws, current.DisplayName, current.AvatarURL, current.AvatarFile, encoded, current.MentionHarness, current.MentionModel, user)
	if err != nil {
		return nil, nil, err
	}
	saved, err := row.settings()
	if err != nil {
		return nil, nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, nil, err
	}
	if previous != nil && (saved.AvatarFile == nil || *saved.AvatarFile != *previous) {
		return saved, previous, nil
	}
	return saved, nil, nil
}
