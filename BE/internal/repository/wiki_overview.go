package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

const wikiSettingsColumns = `wiki_settings.id, wiki_settings.registry_enabled, wiki_settings.overview_enabled,
	wiki_settings.overview_frequency, wiki_settings.overview_weekday, wiki_settings.overview_time,
	wiki_settings.overview_period_days, wiki_settings.overview_next_run_at, wiki_settings.overview_last_run_at,
	wiki_settings.overview_last_error`

// Settings returns a visible wiki's settings; a wiki that never saved any
// gets the defaults (everything off).
func (r *WikiRepository) Settings(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope) (domain.WikiSettings, error) {
	settings := domain.WikiSettings{WikiOverviewSchedule: domain.DefaultWikiOverviewSchedule()}
	if err := r.RequireScope(ctx, workspaceID, scope); err != nil {
		return settings, err
	}
	column, _ := wikiSettingsScope(scope.Kind)
	err := r.db.GetContext(ctx, &settings, `SELECT `+wikiSettingsColumns+` FROM wiki_settings
		WHERE wiki_settings.workspace_id = $1 AND `+column+` = $2`, workspaceID, scope.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.WikiSettings{WikiOverviewSchedule: domain.DefaultWikiOverviewSchedule()}, nil
	}
	return settings, err
}

// SetOverviewSchedule saves a visible wiki's overview schedule and its next
// run (nil when off). userID becomes the person whose visibility scheduled
// runs use; the last error is cleared.
func (r *WikiRepository) SetOverviewSchedule(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, userID uuid.UUID, schedule domain.WikiOverviewSchedule, nextRun *time.Time) error {
	if !scope.Valid() {
		return ErrWikiScopeNotFound
	}
	_, conflict := wikiSettingsScope(scope.Kind)
	var teamID, projectID *uuid.UUID
	if scope.Kind == domain.WikiScopeTeam {
		teamID = &scope.ID
	} else {
		projectID = &scope.ID
	}
	result, err := r.db.ExecContext(ctx, `INSERT INTO wiki_settings (workspace_id, team_id, project_id, updated_by, overview_enabled,
		overview_frequency, overview_weekday, overview_time, overview_period_days, overview_actor, overview_next_run_at)
		SELECT $1, $2, $3, $4, $5, $6, $7, $8, $9, $4, $10 WHERE `+wikiScopeVisible(ctx, scope.Kind, uuidSQL(scope.ID), "CAST($1 AS uuid)")+`
		ON CONFLICT `+conflict+` DO UPDATE SET updated_by = EXCLUDED.updated_by, updated_at = NOW(),
		overview_enabled = EXCLUDED.overview_enabled, overview_frequency = EXCLUDED.overview_frequency,
		overview_weekday = EXCLUDED.overview_weekday, overview_time = EXCLUDED.overview_time,
		overview_period_days = EXCLUDED.overview_period_days, overview_actor = EXCLUDED.overview_actor,
		overview_next_run_at = EXCLUDED.overview_next_run_at, overview_last_error = NULL
		WHERE wiki_settings.workspace_id = EXCLUDED.workspace_id`,
		workspaceID, teamID, projectID, userID, schedule.Enabled, schedule.Frequency, schedule.Weekday, schedule.Time, schedule.PeriodDays, nextRun)
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return err
	} else if affected == 0 {
		return ErrWikiScopeNotFound
	}
	return nil
}

// RegistryRange returns a visible wiki's registry pages from start to end
// (UTC days, inclusive), oldest first.
func (r *WikiRepository) RegistryRange(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, start, end time.Time) ([]domain.WikiPage, error) {
	if err := r.RequireScope(ctx, workspaceID, scope); err != nil {
		return nil, err
	}
	pages := []domain.WikiPage{}
	err := r.db.SelectContext(ctx, &pages, `SELECT `+wikiPageColumns+` FROM wiki_pages`+wikiEditorJoin+`
		WHERE `+wikiPageInScope(ctx, scope)+` AND wiki_pages.registry_date BETWEEN CAST($3 AS date) AND CAST($4 AS date)
		ORDER BY wiki_pages.registry_date`, workspaceID, scope.ID, start.UTC().Format(time.DateOnly), end.UTC().Format(time.DateOnly))
	return pages, err
}

const wikiOverviewColumns = `wiki_registry_overviews.id, wiki_registry_overviews.workspace_id, wiki_registry_overviews.team_id,
	wiki_registry_overviews.project_id, wiki_registry_overviews.period_start, wiki_registry_overviews.period_end,
	wiki_registry_overviews.content, wiki_registry_overviews.trigger, wiki_registry_overviews.generated_by,
	overview_author.name AS generated_by_name, wiki_registry_overviews.generated_at`

// CreateOverview stores an overview if its wiki is visible to the caller; the
// check and the insert are one statement.
func (r *WikiRepository) CreateOverview(ctx context.Context, overview *domain.WikiOverview) error {
	scope := domain.WikiScope{}
	if overview.TeamID != nil {
		scope = domain.TeamWiki(*overview.TeamID)
	} else if overview.ProjectID != nil {
		scope = domain.ProjectWiki(*overview.ProjectID)
	}
	if !scope.Valid() {
		return ErrWikiScopeNotFound
	}
	err := r.db.QueryRowContext(ctx, `INSERT INTO wiki_registry_overviews (workspace_id, team_id, project_id, period_start, period_end, content, trigger, generated_by)
		SELECT $1, $2, $3, CAST($4 AS date), CAST($5 AS date), $6, $7, $8 WHERE `+wikiScopeVisible(ctx, scope.Kind, uuidSQL(scope.ID), "CAST($1 AS uuid)")+`
		RETURNING id, generated_at, (SELECT overview_author.name FROM users overview_author WHERE overview_author.id = wiki_registry_overviews.generated_by)`,
		overview.WorkspaceID, overview.TeamID, overview.ProjectID, overview.PeriodStart.Format(time.DateOnly), overview.PeriodEnd.Format(time.DateOnly),
		overview.Content, overview.Trigger, overview.GeneratedBy).Scan(&overview.ID, &overview.GeneratedAt, &overview.GeneratedByName)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrWikiScopeNotFound
	}
	return err
}

// ListOverviews returns a visible wiki's overviews, newest first, generated
// before the given time when one is set.
func (r *WikiRepository) ListOverviews(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, before *time.Time, limit int) ([]domain.WikiOverview, error) {
	if err := r.RequireScope(ctx, workspaceID, scope); err != nil {
		return nil, err
	}
	column := "wiki_registry_overviews.team_id"
	if scope.Kind == domain.WikiScopeProject {
		column = "wiki_registry_overviews.project_id"
	}
	overviews := []domain.WikiOverview{}
	err := r.db.SelectContext(ctx, &overviews, `SELECT `+wikiOverviewColumns+` FROM wiki_registry_overviews
		LEFT JOIN users overview_author ON overview_author.id = wiki_registry_overviews.generated_by
		WHERE wiki_registry_overviews.workspace_id = $1 AND `+column+` = $2
		AND `+wikiScopeVisible(ctx, scope.Kind, column, "wiki_registry_overviews.workspace_id")+`
		AND (CAST($3 AS timestamptz) IS NULL OR wiki_registry_overviews.generated_at < CAST($3 AS timestamptz))
		ORDER BY wiki_registry_overviews.generated_at DESC, wiki_registry_overviews.id DESC LIMIT $4`, workspaceID, scope.ID, before, limit)
	return overviews, err
}

// DueOverviewSchedules lists enabled schedules whose next run is due. It is
// a system read for the scheduler, which then acts as each schedule's actor.
func (r *WikiRepository) DueOverviewSchedules(ctx context.Context, now time.Time, limit int) ([]domain.DueWikiOverview, error) {
	due := []domain.DueWikiOverview{}
	err := r.db.SelectContext(ctx, &due, `SELECT id, workspace_id, team_id, project_id, overview_actor, overview_next_run_at,
		overview_enabled, overview_frequency, overview_weekday, overview_time, overview_period_days
		FROM wiki_settings WHERE overview_enabled AND overview_next_run_at <= $1
		ORDER BY overview_next_run_at LIMIT $2`, now, limit)
	return due, err
}

// ClaimOverviewRun moves a schedule from the slot it was due at to its next
// slot. Only one claimer of a slot succeeds, across replicas.
func (r *WikiRepository) ClaimOverviewRun(ctx context.Context, settingsID uuid.UUID, slot, next time.Time) (bool, error) {
	result, err := r.db.ExecContext(ctx, `UPDATE wiki_settings SET overview_next_run_at = $3
		WHERE id = $1 AND overview_enabled AND overview_next_run_at = $2`, settingsID, slot, next)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected == 1, err
}

// RecordOverviewRun records when a scheduled run ended and its error, if any.
func (r *WikiRepository) RecordOverviewRun(ctx context.Context, settingsID uuid.UUID, at time.Time, runErr *string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE wiki_settings SET overview_last_run_at = $2, overview_last_error = $3 WHERE id = $1`, settingsID, at, runErr)
	return err
}
