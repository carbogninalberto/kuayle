package service

import (
	"context"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/google/uuid"
)

type FavoriteService struct {
	favRepo repository.FavoriteRepo
}

func NewFavoriteService(favRepo repository.FavoriteRepo) *FavoriteService {
	return &FavoriteService{favRepo: favRepo}
}

func (s *FavoriteService) List(ctx context.Context, workspaceID, userID uuid.UUID) ([]domain.Favorite, error) {
	return s.favRepo.ListByUser(ctx, workspaceID, userID)
}

func (s *FavoriteService) Create(ctx context.Context, workspaceID, userID uuid.UUID, req dto.CreateFavoriteRequest) (*domain.Favorite, error) {
	entityID, _ := uuid.Parse(req.EntityID)
	fav := &domain.Favorite{
		ID:          uuid.New(),
		WorkspaceID: workspaceID,
		UserID:      userID,
		EntityType:  req.EntityType,
		EntityID:    entityID,
	}
	if err := s.favRepo.Create(ctx, fav); err != nil {
		return nil, err
	}
	return fav, nil
}

func (s *FavoriteService) Delete(ctx context.Context, id uuid.UUID) error {
	return s.favRepo.DeleteByID(ctx, id)
}
