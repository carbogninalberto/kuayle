package service

import (
	"context"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/google/uuid"
)

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
			UserID:         userID,
			FontSize:       "default",
			PointerCursors: true,
			ThemeMode:      "dark",
			LightTheme:     "light",
			DarkTheme:      "dark",
		}, nil
	}
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

	if err := s.prefsRepo.Upsert(ctx, prefs); err != nil {
		return nil, err
	}
	return prefs, nil
}
