package domain

import (
	"time"

	"github.com/google/uuid"
)

type CycleStatus string

const (
	CycleStatusUpcoming  CycleStatus = "upcoming"
	CycleStatusActive    CycleStatus = "active"
	CycleStatusCompleted CycleStatus = "completed"
)

type Cycle struct {
	ID            uuid.UUID   `json:"id" db:"id"`
	TeamID        uuid.UUID   `json:"team_id" db:"team_id"`
	Name          string      `json:"name" db:"name"`
	Number        int         `json:"number" db:"number"`
	Status        CycleStatus `json:"status" db:"status"`
	Description   *string     `json:"description" db:"description"`
	Goals         *string     `json:"goals" db:"goals"`
	Retrospective *string     `json:"retrospective" db:"retrospective"`
	StartDate     *time.Time  `json:"start_date" db:"start_date"`
	EndDate       *time.Time  `json:"end_date" db:"end_date"`
	CompletedAt   *time.Time  `json:"completed_at" db:"completed_at"`
	CreatedAt     time.Time   `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time   `json:"updated_at" db:"updated_at"`
}
