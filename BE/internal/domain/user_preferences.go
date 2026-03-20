package domain

import (
	"time"

	"github.com/google/uuid"
)

type UserPreferences struct {
	UserID         uuid.UUID `json:"user_id" db:"user_id"`
	FontSize       string    `json:"font_size" db:"font_size"`
	PointerCursors bool      `json:"pointer_cursors" db:"pointer_cursors"`
	ThemeMode      string    `json:"theme_mode" db:"theme_mode"`
	LightTheme     string    `json:"light_theme" db:"light_theme"`
	DarkTheme      string    `json:"dark_theme" db:"dark_theme"`
	UpdatedAt      time.Time `json:"updated_at" db:"updated_at"`
}
