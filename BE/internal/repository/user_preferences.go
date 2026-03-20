package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/carbon/carbon-backend/internal/domain"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

type UserPreferencesRepository struct {
	db *sqlx.DB
}

func NewUserPreferencesRepository(db *sqlx.DB) *UserPreferencesRepository {
	return &UserPreferencesRepository{db: db}
}

func (r *UserPreferencesRepository) Get(ctx context.Context, userID uuid.UUID) (*domain.UserPreferences, error) {
	var prefs domain.UserPreferences
	err := r.db.GetContext(ctx, &prefs, `SELECT * FROM user_preferences WHERE user_id = $1`, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &prefs, err
}

func (r *UserPreferencesRepository) Upsert(ctx context.Context, prefs *domain.UserPreferences) error {
	query := `
		INSERT INTO user_preferences (user_id, font_size, pointer_cursors, theme_mode, light_theme, dark_theme, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW())
		ON CONFLICT (user_id) DO UPDATE SET
			font_size = EXCLUDED.font_size,
			pointer_cursors = EXCLUDED.pointer_cursors,
			theme_mode = EXCLUDED.theme_mode,
			light_theme = EXCLUDED.light_theme,
			dark_theme = EXCLUDED.dark_theme,
			updated_at = NOW()
		RETURNING updated_at`
	return r.db.QueryRowContext(ctx, query,
		prefs.UserID, prefs.FontSize, prefs.PointerCursors,
		prefs.ThemeMode, prefs.LightTheme, prefs.DarkTheme,
	).Scan(&prefs.UpdatedAt)
}
