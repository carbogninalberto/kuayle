package dto

import "time"

type CreateFavoriteRequest struct {
	EntityType string `json:"entity_type" validate:"required,oneof=project view team cycle label"`
	EntityID   string `json:"entity_id" validate:"required,uuid"`
}

type FavoriteResponse struct {
	ID         string    `json:"id"`
	EntityType string    `json:"entity_type"`
	EntityID   string    `json:"entity_id"`
	Position   int       `json:"position"`
	CreatedAt  time.Time `json:"created_at"`
}
