package handler

import (
	"net/http"

	"github.com/carbon/carbon-backend/internal/domain"
	"github.com/carbon/carbon-backend/internal/dto"
	"github.com/carbon/carbon-backend/pkg/response"
	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v4"
)

type AnalyticsHandler struct {
	db *sqlx.DB
}

func NewAnalyticsHandler(db *sqlx.DB) *AnalyticsHandler {
	return &AnalyticsHandler{db: db}
}

func (h *AnalyticsHandler) Overview(c echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	ctx := c.Request().Context()

	var overview dto.AnalyticsOverview

	// Total issues
	h.db.GetContext(ctx, &overview.TotalIssues,
		`SELECT COUNT(*) FROM issues WHERE workspace_id = $1`, ws.ID)

	// Open issues (not done/cancelled)
	h.db.GetContext(ctx, &overview.OpenIssues,
		`SELECT COUNT(*) FROM issues WHERE workspace_id = $1 AND status NOT IN ('done', 'cancelled')`, ws.ID)

	// Completed issues
	h.db.GetContext(ctx, &overview.CompletedIssues,
		`SELECT COUNT(*) FROM issues WHERE workspace_id = $1 AND status = 'done'`, ws.ID)

	// Overdue issues
	h.db.GetContext(ctx, &overview.OverdueIssues,
		`SELECT COUNT(*) FROM issues WHERE workspace_id = $1 AND due_date < NOW() AND status NOT IN ('done', 'cancelled')`, ws.ID)

	// Total projects
	h.db.GetContext(ctx, &overview.TotalProjects,
		`SELECT COUNT(*) FROM projects WHERE workspace_id = $1`, ws.ID)

	// Total members
	h.db.GetContext(ctx, &overview.TotalMembers,
		`SELECT COUNT(*) FROM workspace_members WHERE workspace_id = $1`, ws.ID)

	return response.Success(c, http.StatusOK, overview)
}

func (h *AnalyticsHandler) IssueDistribution(c echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	ctx := c.Request().Context()

	var byStatus []dto.StatusCount
	h.db.SelectContext(ctx, &byStatus,
		`SELECT status, COUNT(*) as count FROM issues WHERE workspace_id = $1 GROUP BY status ORDER BY count DESC`, ws.ID)

	var byPriority []dto.PriorityCount
	h.db.SelectContext(ctx, &byPriority,
		`SELECT priority, COUNT(*) as count FROM issues WHERE workspace_id = $1 GROUP BY priority ORDER BY priority`, ws.ID)

	return response.Success(c, http.StatusOK, dto.AnalyticsIssueDistribution{
		ByStatus:   byStatus,
		ByPriority: byPriority,
	})
}
