package domain

import (
	"time"

	"github.com/google/uuid"
)

type Cycle struct {
	ID        uuid.UUID  `json:"id" db:"id"`
	TeamID    uuid.UUID  `json:"team_id" db:"team_id"`
	Name      string     `json:"name" db:"name"`
	Number    int        `json:"number" db:"number"`
	StartDate *time.Time `json:"start_date" db:"start_date"`
	EndDate   *time.Time `json:"end_date" db:"end_date"`
	CreatedAt time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt time.Time  `json:"updated_at" db:"updated_at"`
}
