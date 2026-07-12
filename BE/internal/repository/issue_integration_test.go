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

func TestIssueRepositoryPostgres(t *testing.T) {
	databaseURL := os.Getenv("ISSUE_TEST_DATABASE_URL")
	workspaceID := os.Getenv("ISSUE_TEST_WORKSPACE_ID")
	teamID := os.Getenv("ISSUE_TEST_TEAM_ID")
	identifier := os.Getenv("ISSUE_TEST_IDENTIFIER")
	if databaseURL == "" || workspaceID == "" || teamID == "" || identifier == "" {
		t.Skip("issue repository integration test environment is not set")
	}

	db, err := sqlx.Connect("pgx", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewIssueRepository(db)
	workspaceUUID := uuid.MustParse(workspaceID)
	issue, err := repo.GetByIdentifier(context.Background(), workspaceUUID, identifier)
	if err != nil || issue == nil {
		t.Fatalf("GetByIdentifier() issue = %#v, error = %v", issue, err)
	}

	issues, total, err := repo.List(context.Background(), workspaceUUID, dto.IssueFilterParams{
		TeamID:  teamID,
		GroupBy: "status",
		Sort:    "sort_order",
		Order:   "asc",
	})
	if err != nil || total == 0 || len(issues) == 0 {
		t.Fatalf("List() returned %d issues of %d, error = %v", len(issues), total, err)
	}

	analyticsRepo := NewAnalyticsRepository(db)
	overview, err := analyticsRepo.Overview(context.Background(), workspaceID, teamID)
	if err != nil || overview.TotalIssues == 0 {
		t.Fatalf("team Overview() result = %#v, error = %v", overview, err)
	}
	distribution, err := analyticsRepo.Distribution(context.Background(), workspaceID, teamID)
	if err != nil || len(distribution.ByStatus) == 0 {
		t.Fatalf("team Distribution() result = %#v, error = %v", distribution, err)
	}
}
