package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
)

var defaultWorkflowSortOrder = []string{"backlog", "unstarted", "started", "completed", "cancelled"}

var ErrInvalidPreferences = errors.New("invalid preferences")

type PreferencesService struct {
	prefsRepo repository.UserPreferencesRepo
}

func NewPreferencesService(prefsRepo repository.UserPreferencesRepo) *PreferencesService {
	return &PreferencesService{prefsRepo: prefsRepo}
}

func (s *PreferencesService) Get(ctx context.Context, userID uuid.UUID) (*domain.UserPreferences, error) {
	prefs, err := s.prefsRepo.Get(ctx, userID)
	if err != nil {
		return nil, err
	}
	if prefs == nil {
		return &domain.UserPreferences{
			UserID:                    userID,
			FontSize:                  "default",
			PointerCursors:            true,
			ThemeMode:                 "dark",
			LightTheme:                "light",
			DarkTheme:                 "dark",
			WorkflowSortMode:          "default",
			WorkflowSortOrder:         domain.WorkflowSortOrder(defaultWorkflowSortOrder),
			TeamWorkflowSortOverrides: domain.TeamWorkflowSortOverrides{},
			RecentDueDates:            domain.RecentDueDates{},
		}, nil
	}
	if prefs.WorkflowSortMode == "" {
		prefs.WorkflowSortMode = "default"
	}
	if len(prefs.WorkflowSortOrder) == 0 {
		prefs.WorkflowSortOrder = domain.WorkflowSortOrder(defaultWorkflowSortOrder)
	}
	if prefs.TeamWorkflowSortOverrides == nil {
		prefs.TeamWorkflowSortOverrides = domain.TeamWorkflowSortOverrides{}
	}
	prefs.RecentDueDates = domain.RecentDueDates(normalizeRecentDueDates([]string(prefs.RecentDueDates)))
	return prefs, nil
}

func (s *PreferencesService) Update(ctx context.Context, userID uuid.UUID, req dto.UpdatePreferencesRequest) (*domain.UserPreferences, error) {
	prefs, err := s.Get(ctx, userID)
	if err != nil {
		return nil, err
	}

	if req.FontSize != nil {
		prefs.FontSize = *req.FontSize
	}
	if req.PointerCursors != nil {
		prefs.PointerCursors = *req.PointerCursors
	}
	if req.ThemeMode != nil {
		prefs.ThemeMode = *req.ThemeMode
	}
	if req.LightTheme != nil {
		prefs.LightTheme = *req.LightTheme
	}
	if req.DarkTheme != nil {
		prefs.DarkTheme = *req.DarkTheme
	}
	if req.WorkflowSortMode != nil {
		prefs.WorkflowSortMode = *req.WorkflowSortMode
	}
	if req.WorkflowSortOrder != nil {
		order, err := normalizeWorkflowSortOrder(*req.WorkflowSortOrder)
		if err != nil {
			return nil, err
		}
		prefs.WorkflowSortOrder = domain.WorkflowSortOrder(order)
	}
	if req.TeamWorkflowSortOverrides != nil {
		overrides, err := normalizeWorkflowSortOverrides(*req.TeamWorkflowSortOverrides)
		if err != nil {
			return nil, err
		}
		prefs.TeamWorkflowSortOverrides = overrides
	}
	if req.RecentDueDates != nil {
		prefs.RecentDueDates = domain.RecentDueDates(normalizeRecentDueDates(*req.RecentDueDates))
	}

	if err := s.prefsRepo.Upsert(ctx, prefs); err != nil {
		return nil, err
	}
	return prefs, nil
}

func normalizeRecentDueDates(dates []string) []string {
	normalized := make([]string, 0, 3)
	seen := map[string]bool{}
	for _, date := range dates {
		if len(normalized) == 3 {
			break
		}
		if seen[date] {
			continue
		}
		if _, err := time.Parse("2006-01-02", date); err != nil {
			continue
		}
		seen[date] = true
		normalized = append(normalized, date)
	}
	return normalized
}

func normalizeWorkflowSortOverrides(req map[string]dto.WorkflowSortOverride) (domain.TeamWorkflowSortOverrides, error) {
	overrides := domain.TeamWorkflowSortOverrides{}
	for key, override := range req {
		if key == "" {
			return nil, fmt.Errorf("%w: team workflow sort override key cannot be empty", ErrInvalidPreferences)
		}
		if !validOverrideMode(override.Mode) {
			return nil, fmt.Errorf("%w: invalid team workflow sort mode %q", ErrInvalidPreferences, override.Mode)
		}

		var order domain.WorkflowSortOrder
		if len(override.WorkflowSortOrder) > 0 {
			normalized, err := normalizeWorkflowSortOrder(override.WorkflowSortOrder)
			if err != nil {
				return nil, err
			}
			order = domain.WorkflowSortOrder(normalized)
		}

		overrides[key] = domain.WorkflowSortOverride{
			Mode:              override.Mode,
			WorkflowSortOrder: order,
		}
	}
	return overrides, nil
}

func normalizeWorkflowSortOrder(order []string) ([]string, error) {
	if len(order) != len(defaultWorkflowSortOrder) {
		return nil, fmt.Errorf("%w: workflow sort order must contain all status categories", ErrInvalidPreferences)
	}
	seen := make(map[string]bool, len(order))
	valid := make(map[string]bool, len(defaultWorkflowSortOrder))
	for _, category := range defaultWorkflowSortOrder {
		valid[category] = true
	}
	for _, category := range order {
		if !valid[category] {
			return nil, fmt.Errorf("%w: invalid workflow category %q", ErrInvalidPreferences, category)
		}
		if seen[category] {
			return nil, fmt.Errorf("%w: duplicate workflow category %q", ErrInvalidPreferences, category)
		}
		seen[category] = true
	}
	return order, nil
}

func validOverrideMode(mode string) bool {
	switch mode {
	case "inherit", "default", "active-first", "custom":
		return true
	default:
		return false
	}
}
