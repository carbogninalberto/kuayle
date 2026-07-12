package dto

type AnalyticsScopeParams struct {
	TeamID string `query:"team_id"`
}

type AnalyticsOverview struct {
	TotalIssues       int     `json:"total_issues"`
	OpenIssues        int     `json:"open_issues"`
	CompletedIssues   int     `json:"completed_issues"`
	OverdueIssues     int     `json:"overdue_issues"`
	TotalProjects     int     `json:"total_projects"`
	TotalMembers      int     `json:"total_members"`
	StartedIssues     int     `json:"started_issues"`
	UnassignedIssues  int     `json:"unassigned_issues"`
	CompletionRate    float64 `json:"completion_rate"`
	AvgLeadTimeHours  float64 `json:"avg_lead_time_hours"`
	AvgCycleTimeHours float64 `json:"avg_cycle_time_hours"`
}

type StatusCount struct {
	StatusID   string  `json:"status_id" db:"status_id"`
	StatusName string  `json:"name" db:"name"`
	Category   string  `json:"category" db:"category"`
	Color      *string `json:"color" db:"color"`
	Count      int     `json:"count" db:"count"`
}

type PriorityCount struct {
	Priority int `json:"priority" db:"priority"`
	Count    int `json:"count" db:"count"`
}

type AssigneeCount struct {
	AssigneeID *string `json:"assignee_id" db:"assignee_id"`
	Count      int     `json:"count" db:"count"`
}

type AnalyticsIssueDistribution struct {
	ByStatus   []StatusCount   `json:"by_status"`
	ByPriority []PriorityCount `json:"by_priority"`
}

type AnalyticsInsightsParams struct {
	Measure          string  `query:"measure"`
	Slice            string  `query:"slice"`
	Segment          string  `query:"segment"`
	From             string  `query:"from"`
	To               string  `query:"to"`
	TeamID           *string `query:"team_id"`
	ProjectID        *string `query:"project_id"`
	CycleID          *string `query:"cycle_id"`
	AssigneeID       *string `query:"assignee_id"`
	CreatorID        *string `query:"creator_id"`
	StatusID         *string `query:"status_id"`
	StatusType       *string `query:"status_type"`
	Priority         *string `query:"priority"`
	LabelID          *string `query:"label_id"`
	IncludeSubIssues *bool   `query:"include_sub_issues"`
	IncludeTriage    *bool   `query:"include_triage"`
}

type AnalyticsGroup struct {
	Key       string             `json:"key"`
	Label     string             `json:"label"`
	Color     string             `json:"color"`
	Count     int                `json:"count"`
	Value     float64            `json:"value"`
	Aggregate *float64           `json:"aggregate,omitempty"`
	P50       *float64           `json:"p50,omitempty"`
	P75       *float64           `json:"p75,omitempty"`
	P95       *float64           `json:"p95,omitempty"`
	Segments  []AnalyticsSegment `json:"segments"`
}

type AnalyticsSegment struct {
	Key       string   `json:"key"`
	Label     string   `json:"label"`
	Color     string   `json:"color"`
	Count     int      `json:"count"`
	Value     float64  `json:"value"`
	Aggregate *float64 `json:"aggregate,omitempty"`
	P50       *float64 `json:"p50,omitempty"`
	P75       *float64 `json:"p75,omitempty"`
	P95       *float64 `json:"p95,omitempty"`
}

type AnalyticsPoint struct {
	IssueID    string  `json:"issue_id"`
	Identifier string  `json:"identifier"`
	Title      string  `json:"title"`
	Value      float64 `json:"value"`
	SliceKey   string  `json:"slice_key"`
	SegmentKey string  `json:"segment_key"`
}

type AnalyticsInsightsResponse struct {
	Measure    string           `json:"measure"`
	Slice      string           `json:"slice"`
	Segment    string           `json:"segment"`
	Unit       string           `json:"unit"`
	TotalCount int              `json:"total_count"`
	Aggregate  float64          `json:"aggregate"`
	Groups     []AnalyticsGroup `json:"groups"`
	Points     []AnalyticsPoint `json:"points"`
}

type AnalyticsBurnupParams struct {
	From             string  `query:"from"`
	To               string  `query:"to"`
	Interval         string  `query:"interval"`
	TeamID           *string `query:"team_id"`
	ProjectID        *string `query:"project_id"`
	CycleID          *string `query:"cycle_id"`
	AssigneeID       *string `query:"assignee_id"`
	StatusType       *string `query:"status_type"`
	IncludeSubIssues *bool   `query:"include_sub_issues"`
	IncludeTriage    *bool   `query:"include_triage"`
}

type AnalyticsBurnupPoint struct {
	Date           string `json:"date"`
	Created        int    `json:"created"`
	Completed      int    `json:"completed"`
	TotalCreated   int    `json:"total_created"`
	TotalCompleted int    `json:"total_completed"`
	Scope          int    `json:"scope"`
}

type AnalyticsBurnupResponse struct {
	Interval string                 `json:"interval"`
	From     string                 `json:"from"`
	To       string                 `json:"to"`
	Points   []AnalyticsBurnupPoint `json:"points"`
}
