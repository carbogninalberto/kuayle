package domain

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type WorkflowSortOrder []string

func (o WorkflowSortOrder) Value() (driver.Value, error) {
	if o == nil {
		return "[]", nil
	}
	data, err := json.Marshal(o)
	return string(data), err
}

func (o *WorkflowSortOrder) Scan(value interface{}) error {
	return scanJSON(value, o)
}

type WorkflowSortOverride struct {
	Mode              string            `json:"mode"`
	WorkflowSortOrder WorkflowSortOrder `json:"workflow_sort_order,omitempty"`
}

type TeamWorkflowSortOverrides map[string]WorkflowSortOverride

func (o TeamWorkflowSortOverrides) Value() (driver.Value, error) {
	if o == nil {
		return "{}", nil
	}
	data, err := json.Marshal(o)
	return string(data), err
}

func (o *TeamWorkflowSortOverrides) Scan(value interface{}) error {
	return scanJSON(value, o)
}

func scanJSON(value interface{}, target interface{}) error {
	if value == nil {
		return nil
	}
	var data []byte
	switch v := value.(type) {
	case []byte:
		data = v
	case string:
		data = []byte(v)
	default:
		return fmt.Errorf("unsupported JSON scan type %T", value)
	}
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, target)
}

type UserPreferences struct {
	UserID                    uuid.UUID                 `json:"user_id" db:"user_id"`
	FontSize                  string                    `json:"font_size" db:"font_size"`
	PointerCursors            bool                      `json:"pointer_cursors" db:"pointer_cursors"`
	ThemeMode                 string                    `json:"theme_mode" db:"theme_mode"`
	LightTheme                string                    `json:"light_theme" db:"light_theme"`
	DarkTheme                 string                    `json:"dark_theme" db:"dark_theme"`
	WorkflowSortMode          string                    `json:"workflow_sort_mode" db:"workflow_sort_mode"`
	WorkflowSortOrder         WorkflowSortOrder         `json:"workflow_sort_order" db:"workflow_sort_order"`
	TeamWorkflowSortOverrides TeamWorkflowSortOverrides `json:"team_workflow_sort_overrides" db:"team_workflow_sort_overrides"`
	UpdatedAt                 time.Time                 `json:"updated_at" db:"updated_at"`
}
