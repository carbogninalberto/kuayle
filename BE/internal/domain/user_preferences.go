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

type RecentDueDates []string

func (d RecentDueDates) Value() (driver.Value, error) {
	if d == nil {
		return "[]", nil
	}
	data, err := json.Marshal(d)
	return string(data), err
}

func (d *RecentDueDates) Scan(value interface{}) error {
	return scanJSON(value, d)
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

// NotificationEventKeys are the events a user can switch browser notifications
// on or off for.
var NotificationEventKeys = []string{"approval_requested", "task_finished", "review_needed", "assigned", "mentioned", "comment", "issue_updated"}

// NotificationSounds are the selectable notification sounds; "off" is silent.
var NotificationSounds = []string{"off", "chime", "bell", "pop", "drop", "pulse"}

const DefaultNotificationSound = "chime"

// NotificationEvents maps an event key to whether it notifies. A stored map
// may be partial: a missing key is enabled (see Complete).
type NotificationEvents map[string]bool

func (e NotificationEvents) Value() (driver.Value, error) {
	if e == nil {
		return "{}", nil
	}
	data, err := json.Marshal(e)
	return string(data), err
}

func (e *NotificationEvents) Scan(value interface{}) error {
	return scanJSON(value, e)
}

// Complete returns every known event key, enabled unless the map disables it.
// Keys that are not known are dropped.
func (e NotificationEvents) Complete() NotificationEvents {
	complete := make(NotificationEvents, len(NotificationEventKeys))
	for _, key := range NotificationEventKeys {
		enabled, stored := e[key]
		complete[key] = !stored || enabled
	}
	return complete
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
	RecentDueDates            RecentDueDates            `json:"recent_due_dates" db:"recent_due_dates"`
	IssuesGroupBy             string                    `json:"issues_group_by" db:"issues_group_by"`
	BrowserNotifications      bool                      `json:"browser_notifications" db:"browser_notifications"`
	WorkflowEditorPanelOpen   bool                      `json:"workflow_editor_panel_open" db:"workflow_editor_panel_open"`
	NotificationSound         string                    `json:"notification_sound" db:"notification_sound"`
	NotificationEvents        NotificationEvents        `json:"notification_events" db:"notification_events"`
	UpdatedAt                 time.Time                 `json:"updated_at" db:"updated_at"`
}
