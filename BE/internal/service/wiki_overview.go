package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/sanitize"
	log "github.com/sirupsen/logrus"
)

// ErrNoRegistryEntries: the period has nothing logged to summarise.
var ErrNoRegistryEntries = errors.New("nothing was logged in the registry for this period")

// WikiOverviewTimezone is the zone schedules run in. Workspaces have no
// timezone setting yet, so it is UTC, like registry days.
var WikiOverviewTimezone = time.UTC

const (
	wikiOverviewsDefault = 10
	wikiOverviewsMax     = 50
	// wikiOverviewEntriesBytes bounds the registry text sent to the model;
	// the newest days are kept.
	wikiOverviewEntriesBytes = 48 << 10
	wikiOverviewContentBytes = 128 << 10
)

// WikiOverviewAI summarises registry text with the workspace's AI settings.
type WikiOverviewAI interface {
	SummarizeRegistry(ctx context.Context, workspaceID uuid.UUID, scope, start, end, entries string) (string, error)
}

// SetOverviewAI enables registry overviews.
func (s *WikiService) SetOverviewAI(ai WikiOverviewAI) { s.overviewAI = ai }

// Settings returns the wiki's settings, including its overview schedule.
func (s *WikiService) Settings(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope) (domain.WikiSettings, error) {
	settings, err := s.repo.Settings(ctx, workspaceID, scope)
	return settings, wikiError(err)
}

// UpdateSettings changes the fields that are set: the registry switch and
// the overview schedule, whose next run is computed here.
func (s *WikiService) UpdateSettings(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, userID uuid.UUID, registry *bool, schedule *domain.WikiOverviewSchedule) (domain.WikiSettings, error) {
	if registry == nil && schedule == nil {
		return domain.WikiSettings{}, &WikiValidationError{Field: "registry_enabled", Message: "provide registry_enabled or overview_schedule"}
	}
	var next *time.Time
	if schedule != nil {
		if err := schedule.Validate(); err != nil {
			return domain.WikiSettings{}, &WikiValidationError{Field: "overview_schedule", Message: strings.TrimPrefix(err.Error(), domain.ErrWikiOverviewSchedule.Error()+": ")}
		}
		if schedule.Enabled {
			at, _ := schedule.NextRun(s.now(), WikiOverviewTimezone)
			next = &at
		}
	}
	if registry != nil {
		if err := s.repo.SetRegistryEnabled(ctx, workspaceID, scope, userID, *registry); err != nil {
			return domain.WikiSettings{}, wikiError(err)
		}
	}
	if schedule != nil {
		if err := s.repo.SetOverviewSchedule(ctx, workspaceID, scope, userID, *schedule, next); err != nil {
			return domain.WikiSettings{}, wikiError(err)
		}
	}
	return s.Settings(ctx, workspaceID, scope)
}

// Overviews returns a page of the wiki's overviews, newest first, and the
// cursor for the next page, nil when there is none.
func (s *WikiService) Overviews(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, before *time.Time, limit int) ([]domain.WikiOverview, *time.Time, error) {
	if limit <= 0 {
		limit = wikiOverviewsDefault
	}
	if limit > wikiOverviewsMax {
		return nil, nil, &WikiValidationError{Field: "limit", Message: "must be at most 50"}
	}
	overviews, err := s.repo.ListOverviews(ctx, workspaceID, scope, before, limit+1)
	if err != nil {
		return nil, nil, wikiError(err)
	}
	if len(overviews) <= limit {
		return overviews, nil, nil
	}
	overviews = overviews[:limit]
	return overviews, &overviews[limit-1].GeneratedAt, nil
}

// GenerateOverview summarises the registry from start to end (UTC days,
// inclusive, at most 31) with the workspace's AI model and stores it. It reads
// only pages the caller can see; an empty period is ErrNoRegistryEntries.
func (s *WikiService) GenerateOverview(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, userID *uuid.UUID, start, end time.Time, trigger string) (*domain.WikiOverview, error) {
	start, end = utcDay(start), utcDay(end)
	if end.Before(start) {
		return nil, &WikiValidationError{Field: "period_end", Message: "must not be before period_start"}
	}
	if end.Sub(start) >= domain.WikiOverviewMaxDays*24*time.Hour {
		return nil, &WikiValidationError{Field: "period_end", Message: "a period is at most 31 days"}
	}
	if s.overviewAI == nil {
		return nil, ErrAISettingsNotConfigured
	}
	pages, err := s.repo.RegistryRange(ctx, workspaceID, scope, start, end)
	if err != nil {
		return nil, wikiError(err)
	}
	entries := registryText(pages)
	if strings.TrimSpace(entries) == "" {
		return nil, ErrNoRegistryEntries
	}
	content, err := s.overviewAI.SummarizeRegistry(ctx, workspaceID, string(scope.Kind)+" wiki", start.Format(time.DateOnly), end.Format(time.DateOnly), entries)
	if err != nil {
		return nil, err
	}
	content = sanitize.SanitizeEditorContent(content)
	if strings.TrimSpace(content) == "" || len(content) > wikiOverviewContentBytes {
		return nil, ErrAIProviderRequestFailed
	}
	overview := &domain.WikiOverview{WorkspaceID: workspaceID, PeriodStart: start, PeriodEnd: end, Content: content, Trigger: trigger, GeneratedBy: userID}
	if scope.Kind == domain.WikiScopeTeam {
		overview.TeamID = &scope.ID
	} else {
		overview.ProjectID = &scope.ID
	}
	if err := s.repo.CreateOverview(ctx, overview); err != nil {
		return nil, wikiError(err)
	}
	return overview, nil
}

func utcDay(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// registryText turns registry pages into plain text, one line per entry
// under each day, keeping the newest days within the size bound.
func registryText(pages []domain.WikiPage) string {
	days := make([]string, 0, len(pages))
	for _, page := range pages {
		var b strings.Builder
		if page.RegistryDate != nil {
			b.WriteString(page.RegistryDate.UTC().Format(time.DateOnly) + ":\n")
		}
		blocks := strings.NewReplacer("</p>", "\n", "</li>", "\n", "<br>", "\n", "<br/>", "\n").Replace(page.Content)
		for _, line := range strings.Split(blocks, "\n") {
			if text := strings.TrimSpace(sanitize.StripHTML(line)); text != "" {
				b.WriteString("- " + text + "\n")
			}
		}
		days = append(days, b.String())
	}
	size, first := 0, len(days)
	for first > 0 && size+len(days[first-1]) <= wikiOverviewEntriesBytes {
		first--
		size += len(days[first])
	}
	if first == len(days) && len(days) > 0 {
		last := days[len(days)-1]
		return last[len(last)-wikiOverviewEntriesBytes:]
	}
	return strings.Join(days[first:], "\n")
}

// RunOverviewSchedules generates scheduled overviews every minute until ctx
// ends. Each due slot is claimed in the database first, so with several
// replicas only one runs it.
func (s *WikiService) RunOverviewSchedules(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		s.runDueOverviews(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// runDueOverviews runs the schedules due now and returns how many it claimed.
func (s *WikiService) runDueOverviews(ctx context.Context) int {
	now := s.now()
	due, err := s.repo.DueOverviewSchedules(ctx, now, 20)
	if err != nil {
		log.WithError(err).Warn("Could not list due wiki overview schedules")
		return 0
	}
	claimed := 0
	for _, schedule := range due {
		next, err := schedule.WikiOverviewSchedule.NextRun(now, WikiOverviewTimezone)
		if err != nil {
			continue
		}
		won, err := s.repo.ClaimOverviewRun(ctx, schedule.SettingsID, schedule.NextRunAt, next)
		if err != nil || !won {
			continue
		}
		claimed++
		runErr := s.runScheduledOverview(ctx, schedule, now)
		if err := s.repo.RecordOverviewRun(ctx, schedule.SettingsID, s.now(), runErr); err != nil {
			log.WithError(err).Warn("Could not record a wiki overview run")
		}
	}
	return claimed
}

// runScheduledOverview generates one scheduled overview as the person who
// saved the schedule, with their current visibility, and returns a
// user-facing error or nil.
func (s *WikiService) runScheduledOverview(ctx context.Context, schedule domain.DueWikiOverview, now time.Time) *string {
	fail := func(message string) *string { return &message }
	if schedule.Actor == nil {
		return fail("the person who set this schedule is no longer a member; save the schedule again")
	}
	actor := domain.WithAccess(ctx, domain.Access{WorkspaceID: schedule.WorkspaceID, UserID: *schedule.Actor})
	start, end := schedule.WikiOverviewSchedule.Period(now, WikiOverviewTimezone)
	_, err := s.GenerateOverview(actor, schedule.WorkspaceID, schedule.Scope(), schedule.Actor, start, end, domain.WikiOverviewTriggerSchedule)
	switch {
	case err == nil, errors.Is(err, ErrNoRegistryEntries):
		return nil
	case errors.Is(err, ErrWikiNotFound):
		return fail("the person who set this schedule can no longer see this wiki; save the schedule again")
	default:
		return fail(WikiOverviewErrorText(err))
	}
}

// WikiOverviewErrorText is a user-facing reason an overview failed.
func WikiOverviewErrorText(err error) string {
	switch {
	case errors.Is(err, ErrAISettingsNotConfigured):
		return "AI is not configured; set a base URL, model and API key in Settings → AI"
	case errors.Is(err, repository.ErrPrivateWorkspaceOperation):
		return "AI is unavailable after private teams are enabled"
	case errors.Is(err, ErrAIProviderRequestFailed):
		return truncateRunes("the AI provider failed: "+err.Error(), 500)
	}
	return "the overview could not be generated"
}
