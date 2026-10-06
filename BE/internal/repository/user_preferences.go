package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

type UserPreferencesRepository struct {
	db *sqlx.DB
}

func NewUserPreferencesRepository(db *sqlx.DB) *UserPreferencesRepository {
	return &UserPreferencesRepository{db: db}
}

func (r *UserPreferencesRepository) Get(ctx context.Context, userID uuid.UUID) (*domain.UserPreferences, error) {
	var prefs domain.UserPreferences
	err := r.db.GetContext(ctx, &prefs, `SELECT p.user_id,p.font_size,p.pointer_cursors,p.theme_mode,p.light_theme,p.dark_theme,
 p.workflow_sort_mode,p.workflow_sort_order,p.recent_due_dates,p.issues_group_by,p.updated_at,
 COALESCE((SELECT jsonb_object_agg(entry.key,entry.value)
 FROM jsonb_each(p.team_workflow_sort_overrides) entry
 JOIN teams preference_team ON preference_team.id::text=split_part(entry.key,'/',2)
 JOIN workspaces preference_workspace ON preference_workspace.id=preference_team.workspace_id
 WHERE entry.key=preference_workspace.slug||'/'||preference_team.id::text
 AND `+teamVisible(ctx, "preference_team.id")+`),'{}'::jsonb) AS team_workflow_sort_overrides
 FROM user_preferences p WHERE p.user_id = $1`, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &prefs, err
}

func (r *UserPreferencesRepository) Upsert(ctx context.Context, prefs *domain.UserPreferences) error {
	query := `
		INSERT INTO user_preferences (user_id, font_size, pointer_cursors, theme_mode, light_theme, dark_theme, workflow_sort_mode, workflow_sort_order, team_workflow_sort_overrides, recent_due_dates, issues_group_by, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW())
		ON CONFLICT (user_id) DO UPDATE SET
			font_size = EXCLUDED.font_size,
			pointer_cursors = EXCLUDED.pointer_cursors,
			theme_mode = EXCLUDED.theme_mode,
			light_theme = EXCLUDED.light_theme,
			dark_theme = EXCLUDED.dark_theme,
			workflow_sort_mode = EXCLUDED.workflow_sort_mode,
			workflow_sort_order = EXCLUDED.workflow_sort_order,
			team_workflow_sort_overrides = EXCLUDED.team_workflow_sort_overrides,
			recent_due_dates = EXCLUDED.recent_due_dates,
			issues_group_by = EXCLUDED.issues_group_by,
			updated_at = NOW()
		RETURNING updated_at`
	return r.db.QueryRowContext(ctx, query,
		prefs.UserID, prefs.FontSize, prefs.PointerCursors,
		prefs.ThemeMode, prefs.LightTheme, prefs.DarkTheme,
		prefs.WorkflowSortMode, prefs.WorkflowSortOrder, prefs.TeamWorkflowSortOverrides, prefs.RecentDueDates, prefs.IssuesGroupBy,
	).Scan(&prefs.UpdatedAt)
}
