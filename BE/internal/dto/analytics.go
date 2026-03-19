package dto

type AnalyticsOverview struct {
	TotalIssues     int `json:"total_issues"`
	OpenIssues      int `json:"open_issues"`
	CompletedIssues int `json:"completed_issues"`
	OverdueIssues   int `json:"overdue_issues"`
	TotalProjects   int `json:"total_projects"`
	TotalMembers    int `json:"total_members"`
}

type StatusCount struct {
	Status string `json:"status" db:"status"`
	Count  int    `json:"count" db:"count"`
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
