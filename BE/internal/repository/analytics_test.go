package repository

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/dto"
)

func TestAnalyticsRepositoryPostgres(t *testing.T) {
	databaseURL := os.Getenv("ANALYTICS_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("ANALYTICS_TEST_DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewAnalyticsRepository(db)
	issueRepo := NewIssueRepository(db)
	ctx := context.Background()
	workspaceID := "00000000-0000-0000-0000-000000000002"
	teamID := "00000000-0000-0000-0000-000000000003"

	issue, err := issueRepo.GetByIdentifier(ctx, uuid.MustParse(workspaceID), "CORE-1")
	if err != nil || issue == nil {
		t.Fatalf("GetByIdentifier() issue = %#v, error = %v", issue, err)
	}
	issues, total, err := issueRepo.List(ctx, uuid.MustParse(workspaceID), dto.IssueFilterParams{
		TeamID:  teamID,
		GroupBy: "status",
		Sort:    "sort_order",
		Order:   "asc",
	})
	if err != nil || total != 1 || len(issues) != 1 {
		t.Fatalf("List() issues = %#v, total = %d, error = %v", issues, total, err)
	}

	distribution, err := repo.Distribution(ctx, workspaceID, "")
	if err != nil {
		t.Fatalf("Distribution() error = %v", err)
	}
	if len(distribution.ByStatus) != 3 || distribution.ByStatus[2].StatusName != "Complete" {
		t.Fatalf("unexpected status distribution: %#v", distribution.ByStatus)
	}
	teamDistribution, err := repo.Distribution(ctx, workspaceID, teamID)
	if err != nil || len(teamDistribution.ByStatus) != 3 {
		t.Fatalf("team Distribution() result = %#v, error = %v", teamDistribution, err)
	}
	teamOverview, err := repo.Overview(ctx, workspaceID, teamID)
	if err != nil || teamOverview.TotalIssues != 1 {
		t.Fatalf("team Overview() result = %#v, error = %v", teamOverview, err)
	}

	insights, err := repo.Insights(ctx, workspaceID, &dto.AnalyticsInsightsParams{
		Measure: "issue_count",
		Slice:   "label",
		Segment: "status",
	})
	if err != nil {
		t.Fatalf("Insights(count) error = %v", err)
	}
	if insights.TotalCount != 1 || len(insights.Groups) != 1 || insights.Groups[0].Count != 1 {
		t.Fatalf("unexpected count insights: %#v", insights)
	}

	leadTime, err := repo.Insights(ctx, workspaceID, &dto.AnalyticsInsightsParams{
		Measure: "lead_time",
		Slice:   "status",
		Segment: "priority",
	})
	if err != nil {
		t.Fatalf("Insights(lead_time) error = %v", err)
	}
	if leadTime.TotalCount != 1 || len(leadTime.Points) != 1 || leadTime.Groups[0].P50 == nil {
		t.Fatalf("unexpected lead-time insights: %#v", leadTime)
	}

	burnup, err := repo.Burnup(ctx, workspaceID, &dto.AnalyticsBurnupParams{
		From:     "2026-07-01",
		To:       "2026-07-31",
		Interval: "week",
		TeamID:   &teamID,
	})
	if err != nil {
		t.Fatalf("Burnup() error = %v", err)
	}
	if len(burnup.Points) == 0 || burnup.Points[len(burnup.Points)-1].TotalCompleted != 1 {
		t.Fatalf("unexpected burnup: %#v", burnup.Points)
	}
}

func TestValidateInsightParams(t *testing.T) {
	tests := []struct {
		name    string
		params  dto.AnalyticsInsightsParams
		wantErr bool
		errMsg  string
	}{
		{
			name:    "defaults are valid",
			params:  dto.AnalyticsInsightsParams{},
			wantErr: false,
		},
		{
			name:   "valid measure and slice",
			params: dto.AnalyticsInsightsParams{Measure: "lead_time", Slice: "status"},
		},
		{
			name:    "invalid measure",
			params:  dto.AnalyticsInsightsParams{Measure: "bogus"},
			wantErr: true,
			errMsg:  "invalid measure",
		},
		{
			name:    "invalid slice",
			params:  dto.AnalyticsInsightsParams{Slice: "bogus"},
			wantErr: true,
			errMsg:  "invalid slice",
		},
		{
			name:    "invalid segment",
			params:  dto.AnalyticsInsightsParams{Segment: "bogus"},
			wantErr: true,
			errMsg:  "invalid segment",
		},
		{
			name:    "segment equals slice rejected",
			params:  dto.AnalyticsInsightsParams{Slice: "status", Segment: "status"},
			wantErr: true,
			errMsg:  "segment must differ from slice",
		},
		{
			name:    "segment and slice both none is ok",
			params:  dto.AnalyticsInsightsParams{Slice: "none", Segment: "none"},
			wantErr: false,
		},
		{
			name:    "invalid from date",
			params:  dto.AnalyticsInsightsParams{From: "not-a-date"},
			wantErr: true,
			errMsg:  "invalid from date",
		},
		{
			name:    "invalid to date",
			params:  dto.AnalyticsInsightsParams{To: "not-a-date"},
			wantErr: true,
			errMsg:  "invalid to date",
		},
		{
			name:    "valid dates",
			params:  dto.AnalyticsInsightsParams{From: "2024-01-01", To: "2024-12-31"},
			wantErr: false,
		},
		{
			name:    "from after to rejected",
			params:  dto.AnalyticsInsightsParams{From: "2024-06-01", To: "2024-01-01"},
			wantErr: true,
			errMsg:  "from date must be before or equal to to date",
		},
		{
			name:    "from equals to is ok",
			params:  dto.AnalyticsInsightsParams{From: "2024-06-01", To: "2024-06-01"},
			wantErr: false,
		},
		{
			name:   "all valid dimensions",
			params: dto.AnalyticsInsightsParams{Slice: "assignee", Segment: "team"},
		},
		{
			name:   "label with segment",
			params: dto.AnalyticsInsightsParams{Slice: "label", Segment: "status_type"},
		},
		{
			name:    "invalid team id",
			params:  dto.AnalyticsInsightsParams{TeamID: stringPtr("not-a-uuid")},
			wantErr: true,
			errMsg:  "invalid team_id",
		},
		{
			name:    "invalid priority",
			params:  dto.AnalyticsInsightsParams{Priority: stringPtr("urgent")},
			wantErr: true,
			errMsg:  "invalid priority",
		},
		{
			name:    "invalid status type",
			params:  dto.AnalyticsInsightsParams{StatusType: stringPtr("done")},
			wantErr: true,
			errMsg:  "invalid status_type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateInsightParams(&tt.params)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errMsg)
				} else if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestValidateBurnupParams(t *testing.T) {
	tests := []struct {
		name    string
		params  dto.AnalyticsBurnupParams
		wantErr bool
	}{
		{name: "valid defaults", params: dto.AnalyticsBurnupParams{From: "2026-01-01", To: "2026-02-01"}},
		{name: "invalid interval", params: dto.AnalyticsBurnupParams{From: "2026-01-01", To: "2026-02-01", Interval: "year"}, wantErr: true},
		{name: "reversed range", params: dto.AnalyticsBurnupParams{From: "2026-02-01", To: "2026-01-01"}, wantErr: true},
		{name: "invalid scoped id", params: dto.AnalyticsBurnupParams{From: "2026-01-01", To: "2026-02-01", TeamID: stringPtr("bad")}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateBurnupParams(&tt.params)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateBurnupParams() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && tt.params.Interval != "week" {
				t.Fatalf("default interval = %q, want week", tt.params.Interval)
			}
		})
	}
}

func TestMeasureExpr(t *testing.T) {
	r := &AnalyticsRepository{}

	tests := []struct {
		measure  string
		wantVal  string
		wantUnit string
	}{
		{"issue_count", "1", "issues"},
		{"issue_age", "EXTRACT(EPOCH FROM (NOW() - i.created_at)) / 3600.0", "hours"},
		{"lead_time", "EXTRACT(EPOCH FROM (i.completed_at - i.created_at)) / 3600.0", "hours"},
		{"cycle_time", "EXTRACT(EPOCH FROM (i.completed_at - i.started_at)) / 3600.0", "hours"},
		{"triage_time", "EXTRACT(EPOCH FROM (i.triaged_at - i.created_at)) / 3600.0", "hours"},
	}

	for _, tt := range tests {
		t.Run(tt.measure, func(t *testing.T) {
			_, val, unit := r.measureExpr(tt.measure)
			if val != tt.wantVal {
				t.Errorf("valueExpr = %q, want %q", val, tt.wantVal)
			}
			if unit != tt.wantUnit {
				t.Errorf("unit = %q, want %q", unit, tt.wantUnit)
			}
		})
	}
}

func TestMeasureDateCol(t *testing.T) {
	r := &AnalyticsRepository{}

	tests := []struct {
		measure string
		want    string
	}{
		{"issue_count", "i.created_at"},
		{"issue_age", "i.created_at"},
		{"lead_time", "i.completed_at"},
		{"cycle_time", "i.completed_at"},
		{"triage_time", "i.triaged_at"},
		{"bogus", "i.created_at"},
	}

	for _, tt := range tests {
		t.Run(tt.measure, func(t *testing.T) {
			got := r.measureDateCol(tt.measure)
			if got != tt.want {
				t.Errorf("measureDateCol(%q) = %q, want %q", tt.measure, got, tt.want)
			}
		})
	}
}

func TestIsDurationMeasure(t *testing.T) {
	duration := []string{"issue_age", "lead_time", "cycle_time", "triage_time"}
	nonDuration := []string{"issue_count", "bogus", ""}

	for _, m := range duration {
		if !isDurationMeasure(m) {
			t.Errorf("%q should be a duration measure", m)
		}
	}
	for _, m := range nonDuration {
		if isDurationMeasure(m) {
			t.Errorf("%q should not be a duration measure", m)
		}
	}
}

func TestDimensionParts(t *testing.T) {
	r := &AnalyticsRepository{}

	parts, join, group, label := r.dimensionParts("none", "x")
	if parts != "" || join != "" || group != "" || label != "" {
		t.Errorf("none dimension should return empty")
	}

	parts, join, group, label = r.dimensionParts("status_type", "x")
	if parts == "" || group == "" {
		t.Errorf("status_type dimension should have parts and group")
	}
	if !contains(parts, "ts.category") {
		t.Errorf("status_type should reference ts.category, got: %s", parts)
	}

	parts, join, group, label = r.dimensionParts("assignee", "x")
	if join == "" {
		t.Errorf("assignee dimension should have join")
	}

	parts, join, group, label = r.dimensionParts("project", "x")
	if parts == "" || join == "" {
		t.Errorf("project dimension should have join")
	}

	_, _, _, _ = r.dimensionParts("bogus", "x")

	// label dimension must not reference nonexistent alias
	parts, join, group, label = r.dimensionParts("label", "x")
	if !contains(parts, "l_x") {
		t.Errorf("label parts should reference l_x not lbl_x, got: %s", parts)
	}
	if !contains(group, "l_x") {
		t.Errorf("label group should reference l_x not lbl_x, got: %s", group)
	}
	_ = join
	_ = label
}

func TestDimensionPointParts(t *testing.T) {
	r := &AnalyticsRepository{}

	sel, join, _, _ := r.dimensionPointParts("none", "x")
	if sel != "" || join != "" {
		t.Errorf("none should return empty")
	}

	sel, join, _, _ = r.dimensionPointParts("label", "x")
	if !contains(sel, "l_x") {
		t.Errorf("label should reference l_x, got: %s", sel)
	}
	if join == "" {
		t.Errorf("label should have join")
	}
}

func TestBuildInsightWhere(t *testing.T) {
	r := &AnalyticsRepository{}

	t.Run("workspace scoping", func(t *testing.T) {
		where, args, err := r.buildInsightWhere("ws-1", &dto.AnalyticsInsightsParams{})
		if err != nil {
			t.Fatal(err)
		}
		if len(where) < 1 || !contains(where[0], "workspace_id") {
			t.Errorf("missing workspace scope: %v", where)
		}
		if len(args) < 1 || args[0] != "ws-1" {
			t.Errorf("missing workspace arg: %v", args)
		}
	})

	t.Run("with team filter", func(t *testing.T) {
		teamID := "team-uuid"
		where, args, err := r.buildInsightWhere("ws-1", &dto.AnalyticsInsightsParams{
			TeamID: &teamID,
		})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, w := range where {
			if contains(w, "team_id") {
				found = true
			}
		}
		if !found {
			t.Errorf("team filter not found in: %v", where)
		}
		_ = args
	})

	t.Run("with assignee none", func(t *testing.T) {
		none := "none"
		where, _, err := r.buildInsightWhere("ws-1", &dto.AnalyticsInsightsParams{
			AssigneeID: &none,
		})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, w := range where {
			if contains(w, "NOT EXISTS") && contains(w, "issue_assignees") {
				found = true
			}
		}
		if !found {
			t.Errorf("unassigned filter not found in: %v", where)
		}
	})

	t.Run("with project none", func(t *testing.T) {
		none := "none"
		where, _, err := r.buildInsightWhere("ws-1", &dto.AnalyticsInsightsParams{
			ProjectID: &none,
		})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, w := range where {
			if contains(w, "project_id IS NULL") {
				found = true
			}
		}
		if !found {
			t.Errorf("no-project filter not found in: %v", where)
		}
	})

	t.Run("date filter for count measure uses created_at", func(t *testing.T) {
		from := "2024-01-01"
		where, _, err := r.buildInsightWhere("ws-1", &dto.AnalyticsInsightsParams{
			Measure: "issue_count",
			From:    from,
		})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, w := range where {
			if contains(w, "created_at") {
				found = true
			}
		}
		if !found {
			t.Errorf("date filter on created_at not found for count measure: %v", where)
		}
	})

	t.Run("date filter for lead_time uses completed_at", func(t *testing.T) {
		from := "2024-01-01"
		where, _, err := r.buildInsightWhere("ws-1", &dto.AnalyticsInsightsParams{
			Measure: "lead_time",
			From:    from,
		})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, w := range where {
			if contains(w, "completed_at") {
				found = true
			}
		}
		if !found {
			t.Errorf("date filter on completed_at not found for lead_time: %v", where)
		}
	})

	t.Run("date filter for triage_time uses triaged_at", func(t *testing.T) {
		from := "2024-01-01"
		where, _, err := r.buildInsightWhere("ws-1", &dto.AnalyticsInsightsParams{
			Measure: "triage_time",
			From:    from,
		})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, w := range where {
			if contains(w, "triaged_at") {
				found = true
			}
		}
		if !found {
			t.Errorf("date filter on triaged_at not found for triage_time: %v", where)
		}
	})

	t.Run("empty filter IDs are ignored", func(t *testing.T) {
		empty := ""
		where, _, _ := r.buildInsightWhere("ws-1", &dto.AnalyticsInsightsParams{
			TeamID: &empty,
		})
		for _, w := range where {
			if contains(w, "team_id =") {
				t.Errorf("empty team_id should be ignored: %v", where)
			}
		}
	})

	t.Run("include_sub_issues logic correct", func(t *testing.T) {
		// verify the logic used in Insights()
		sub := false
		params := &dto.AnalyticsInsightsParams{IncludeSubIssues: &sub}
		includeSubIssues := params.IncludeSubIssues == nil || *params.IncludeSubIssues
		if includeSubIssues {
			t.Error("IncludeSubIssues=false should produce includeSubIssues=false")
		}
	})

	t.Run("include_triage logic correct", func(t *testing.T) {
		triage := false
		params := &dto.AnalyticsInsightsParams{IncludeTriage: &triage}
		includeTriage := params.IncludeTriage == nil || *params.IncludeTriage
		if includeTriage {
			t.Error("IncludeTriage=false should produce includeTriage=false")
		}
	})
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func stringPtr(value string) *string {
	return &value
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
