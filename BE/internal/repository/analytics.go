package repository

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/dto"
)

type AnalyticsRepository struct {
	db *sqlx.DB
}

func NewAnalyticsRepository(db *sqlx.DB) *AnalyticsRepository {
	return &AnalyticsRepository{db: db}
}

func (r *AnalyticsRepository) Overview(ctx context.Context, workspaceID, teamID string) (*dto.AnalyticsOverview, error) {
	var o dto.AnalyticsOverview
	issueScope := ""
	args := []interface{}{workspaceID}
	if teamID != "" {
		issueScope = " AND i.team_id = $2"
		args = append(args, teamID)
	}

	err := r.db.GetContext(ctx, &o.TotalIssues,
		`SELECT COUNT(*) FROM issues i WHERE i.workspace_id = $1`+issueScope, args...)
	if err != nil {
		return nil, err
	}

	err = r.db.GetContext(ctx, &o.OpenIssues,
		`SELECT COUNT(*)
		 FROM issues i
		 LEFT JOIN team_statuses ts ON ts.id = i.status_id
		 WHERE i.workspace_id = $1
		   AND COALESCE(ts.category, 'backlog') NOT IN ('completed', 'cancelled')`+issueScope, args...)
	if err != nil {
		return nil, err
	}

	err = r.db.GetContext(ctx, &o.CompletedIssues,
		`SELECT COUNT(*)
		 FROM issues i
		 INNER JOIN team_statuses ts ON ts.id = i.status_id
		 WHERE i.workspace_id = $1 AND ts.category = 'completed'`+issueScope, args...)
	if err != nil {
		return nil, err
	}

	err = r.db.GetContext(ctx, &o.OverdueIssues,
		`SELECT COUNT(*)
		 FROM issues i
		 LEFT JOIN team_statuses ts ON ts.id = i.status_id
		 WHERE i.workspace_id = $1
		   AND i.due_date < CURRENT_DATE
		   AND COALESCE(ts.category, 'backlog') NOT IN ('completed', 'cancelled')`+issueScope, args...)
	if err != nil {
		return nil, err
	}

	projectScope := ""
	if teamID != "" {
		projectScope = " AND team_id = $2"
	}
	err = r.db.GetContext(ctx, &o.TotalProjects,
		`SELECT COUNT(*) FROM projects WHERE workspace_id = $1`+projectScope, args...)
	if err != nil {
		return nil, err
	}

	if teamID == "" {
		err = r.db.GetContext(ctx, &o.TotalMembers,
			`SELECT COUNT(*) FROM workspace_members WHERE workspace_id = $1`, workspaceID)
	} else {
		err = r.db.GetContext(ctx, &o.TotalMembers,
			`SELECT COUNT(*) FROM team_members tm
			 INNER JOIN teams t ON t.id = tm.team_id
			 WHERE t.workspace_id = $1 AND tm.team_id = $2`, workspaceID, teamID)
	}
	if err != nil {
		return nil, err
	}

	err = r.db.GetContext(ctx, &o.StartedIssues,
		`SELECT COUNT(*) FROM issues i
		 INNER JOIN team_statuses ts ON ts.id = i.status_id
		 WHERE i.workspace_id = $1 AND ts.category = 'started'`+issueScope, args...)
	if err != nil {
		return nil, err
	}

	err = r.db.GetContext(ctx, &o.UnassignedIssues,
		`SELECT COUNT(*)
		 FROM issues i
		 LEFT JOIN team_statuses ts ON ts.id = i.status_id
		 WHERE i.workspace_id = $1
		   AND COALESCE(ts.category, 'backlog') NOT IN ('completed', 'cancelled')
		   AND NOT EXISTS (SELECT 1 FROM issue_assignees ia WHERE ia.issue_id = i.id)`+issueScope, args...)
	if err != nil {
		return nil, err
	}

	totalActive := o.OpenIssues + o.CompletedIssues
	if totalActive > 0 {
		o.CompletionRate = float64(o.CompletedIssues) / float64(totalActive) * 100
	}

	err = r.db.GetContext(ctx, &o.AvgLeadTimeHours,
		`SELECT COALESCE(EXTRACT(EPOCH FROM AVG(i.completed_at - i.created_at)) / 3600.0, 0)
		 FROM issues i
		 WHERE i.workspace_id = $1 AND i.completed_at IS NOT NULL`+issueScope, args...)
	if err != nil {
		return nil, err
	}

	err = r.db.GetContext(ctx, &o.AvgCycleTimeHours,
		`SELECT COALESCE(EXTRACT(EPOCH FROM AVG(i.completed_at - i.started_at)) / 3600.0, 0)
		 FROM issues i
		 WHERE i.workspace_id = $1
		   AND i.completed_at IS NOT NULL
		   AND i.started_at IS NOT NULL`+issueScope, args...)
	if err != nil {
		return nil, err
	}

	return &o, nil
}

func (r *AnalyticsRepository) Distribution(ctx context.Context, workspaceID, teamID string) (*dto.AnalyticsIssueDistribution, error) {
	teamScope := ""
	args := []interface{}{workspaceID}
	if teamID != "" {
		teamScope = " AND t.id = $2"
		args = append(args, teamID)
	}
	issueTeamScope := ""
	if teamID != "" {
		issueTeamScope = " AND i.team_id = $2"
	}
	var byStatus []dto.StatusCount
	err := r.db.SelectContext(ctx, &byStatus,
		`SELECT ts.id::text AS status_id, ts.name AS name, ts.category::text AS category, ts.color,
		        COUNT(i.id) AS count
		 FROM team_statuses ts
		 INNER JOIN teams t ON t.id = ts.team_id
		 LEFT JOIN issues i ON i.status_id = ts.id AND i.workspace_id = $1
		 WHERE t.workspace_id = $1`+teamScope+`
		 GROUP BY ts.id, ts.name, ts.category, ts.color, ts.position
		 ORDER BY ts.position`, args...)
	if err != nil {
		return nil, err
	}

	var byPriority []dto.PriorityCount
	err = r.db.SelectContext(ctx, &byPriority,
		`SELECT priority, COUNT(*) AS count
		 FROM issues i WHERE i.workspace_id = $1`+issueTeamScope+`
		 GROUP BY priority ORDER BY priority`, args...)
	if err != nil {
		return nil, err
	}

	if byStatus == nil {
		byStatus = []dto.StatusCount{}
	}
	if byPriority == nil {
		byPriority = []dto.PriorityCount{}
	}

	return &dto.AnalyticsIssueDistribution{
		ByStatus:   byStatus,
		ByPriority: byPriority,
	}, nil
}

var allowedMeasures = map[string]bool{
	"issue_count": true, "issue_age": true, "lead_time": true, "cycle_time": true, "triage_time": true,
}
var allowedDims = map[string]bool{
	"none": true, "status_type": true, "status": true, "priority": true,
	"assignee": true, "team": true, "project": true, "cycle": true, "label": true, "creator": true,
}

var allowedStatusTypes = map[string]bool{
	"backlog": true, "unstarted": true, "started": true, "completed": true, "cancelled": true,
}

func validateOptionalUUID(name string, value *string, allowNone bool) error {
	if value == nil || *value == "" || (allowNone && *value == "none") {
		return nil
	}
	if _, err := uuid.Parse(*value); err != nil {
		return fmt.Errorf("invalid %s", name)
	}
	return nil
}

func validateUUIDList(name string, value *string) error {
	if value == nil || *value == "" {
		return nil
	}
	for _, item := range strings.Split(*value, ",") {
		if _, err := uuid.Parse(strings.TrimSpace(item)); err != nil {
			return fmt.Errorf("invalid %s", name)
		}
	}
	return nil
}

func ValidateInsightParams(params *dto.AnalyticsInsightsParams) error {
	if params.Measure == "" {
		params.Measure = "issue_count"
	}
	if !allowedMeasures[params.Measure] {
		return fmt.Errorf("invalid measure: %s", params.Measure)
	}
	if params.Slice == "" {
		params.Slice = "none"
	}
	if !allowedDims[params.Slice] {
		return fmt.Errorf("invalid slice: %s", params.Slice)
	}
	if params.Segment == "" {
		params.Segment = "none"
	}
	if !allowedDims[params.Segment] {
		return fmt.Errorf("invalid segment: %s", params.Segment)
	}
	if params.Slice != "none" && params.Segment != "none" && params.Slice == params.Segment {
		return fmt.Errorf("segment must differ from slice")
	}
	if params.From != "" {
		if _, err := time.Parse("2006-01-02", params.From); err != nil {
			return fmt.Errorf("invalid from date: %s", params.From)
		}
	}
	if params.To != "" {
		if _, err := time.Parse("2006-01-02", params.To); err != nil {
			return fmt.Errorf("invalid to date: %s", params.To)
		}
	}
	if params.From != "" && params.To != "" {
		from, _ := time.Parse("2006-01-02", params.From)
		to, _ := time.Parse("2006-01-02", params.To)
		if from.After(to) {
			return fmt.Errorf("from date must be before or equal to to date")
		}
	}
	for _, filter := range []struct {
		name      string
		value     *string
		allowNone bool
	}{
		{"team_id", params.TeamID, false},
		{"project_id", params.ProjectID, true},
		{"cycle_id", params.CycleID, true},
		{"assignee_id", params.AssigneeID, true},
		{"creator_id", params.CreatorID, false},
		{"label_id", params.LabelID, false},
	} {
		if err := validateOptionalUUID(filter.name, filter.value, filter.allowNone); err != nil {
			return err
		}
	}
	if err := validateUUIDList("status_id", params.StatusID); err != nil {
		return err
	}
	if params.StatusType != nil && *params.StatusType != "" && !allowedStatusTypes[*params.StatusType] {
		return fmt.Errorf("invalid status_type")
	}
	if params.Priority != nil && *params.Priority != "" {
		for _, item := range strings.Split(*params.Priority, ",") {
			priority, err := strconv.Atoi(strings.TrimSpace(item))
			if err != nil || priority < 0 || priority > 4 {
				return fmt.Errorf("invalid priority")
			}
		}
	}
	return nil
}

func ValidateBurnupParams(params *dto.AnalyticsBurnupParams) error {
	if params.Interval == "" {
		params.Interval = "week"
	}
	if params.Interval != "day" && params.Interval != "week" && params.Interval != "month" {
		return fmt.Errorf("invalid interval: must be day, week, or month")
	}
	if params.From == "" || params.To == "" {
		return fmt.Errorf("from and to are required")
	}
	from, err := time.Parse("2006-01-02", params.From)
	if err != nil {
		return fmt.Errorf("invalid from date")
	}
	to, err := time.Parse("2006-01-02", params.To)
	if err != nil {
		return fmt.Errorf("invalid to date")
	}
	if from.After(to) {
		return fmt.Errorf("from must be before or equal to to")
	}
	for _, filter := range []struct {
		name      string
		value     *string
		allowNone bool
	}{
		{"team_id", params.TeamID, false},
		{"project_id", params.ProjectID, true},
		{"cycle_id", params.CycleID, true},
		{"assignee_id", params.AssigneeID, true},
	} {
		if err := validateOptionalUUID(filter.name, filter.value, filter.allowNone); err != nil {
			return err
		}
	}
	return nil
}

func (r *AnalyticsRepository) Insights(ctx context.Context, workspaceID string, params *dto.AnalyticsInsightsParams) (*dto.AnalyticsInsightsResponse, error) {
	where, whereArgs, err := r.buildInsightWhere(workspaceID, params)
	if err != nil {
		return nil, err
	}

	includeSubIssues := params.IncludeSubIssues == nil || *params.IncludeSubIssues
	if !includeSubIssues {
		where = append(where, "i.parent_id IS NULL")
	}
	includeTriage := params.IncludeTriage == nil || *params.IncludeTriage
	if !includeTriage {
		where = append(where, "i.triaged = TRUE")
	}
	switch params.Measure {
	case "lead_time":
		where = append(where, "i.completed_at IS NOT NULL")
	case "cycle_time":
		where = append(where, "i.completed_at IS NOT NULL", "i.started_at IS NOT NULL")
	case "triage_time":
		where = append(where, "i.triaged_at IS NOT NULL")
	}

	dateCol := r.measureDateCol(params.Measure)
	_, valueExpr, unit := r.measureExpr(params.Measure)

	sliceSelect, sliceJoin, sliceGroup, _ := r.dimensionParts(params.Slice, "sl")
	segSelect, segJoin, segGroup, _ := r.dimensionParts(params.Segment, "sg")

	selectParts := []string{}
	groupParts := []string{}
	joinParts := []string{}

	if sliceGroup != "" {
		selectParts = append(selectParts, sliceSelect)
		groupParts = append(groupParts, sliceGroup)
		if sliceJoin != "" {
			joinParts = append(joinParts, sliceJoin)
		}
	}
	if segGroup != "" {
		selectParts = append(selectParts, segSelect)
		groupParts = append(groupParts, segGroup)
		if segJoin != "" {
			joinParts = append(joinParts, segJoin)
		}
	}

	if isDurationMeasure(params.Measure) {
		selectParts = append(selectParts,
			fmt.Sprintf("COUNT(*) AS cnt"),
			fmt.Sprintf("COALESCE(AVG(%s), 0) AS val", valueExpr),
			fmt.Sprintf("COALESCE(percentile_cont(0.50) WITHIN GROUP (ORDER BY %s), 0) AS p50", valueExpr),
			fmt.Sprintf("COALESCE(percentile_cont(0.75) WITHIN GROUP (ORDER BY %s), 0) AS p75", valueExpr),
			fmt.Sprintf("COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY %s), 0) AS p95", valueExpr),
		)
	} else {
		selectParts = append(selectParts,
			"COUNT(*) AS cnt",
			"COALESCE(AVG(1), 0) AS val",
			"0 AS p50",
			"0 AS p75",
			"0 AS p95",
		)
	}

	joinClause := ""
	if len(joinParts) > 0 {
		joinClause = strings.Join(joinParts, "\n")
	}

	whereClause := ""
	if len(where) > 0 {
		whereClause = "WHERE " + strings.Join(where, " AND ")
	}
	groupClause := ""
	if len(groupParts) > 0 {
		groupClause = "GROUP BY " + strings.Join(groupParts, ", ")
	}

	// Calculate workspace totals without dimension joins so multi-value
	// assignee and label groups cannot inflate the aggregate.
	totalQuery := fmt.Sprintf(`
		SELECT COUNT(*) AS count, COALESCE(AVG(%s), 0) AS aggregate
		FROM issues i
		LEFT JOIN team_statuses ts ON ts.id = i.status_id
		%s`,
		valueExpr,
		whereClause,
	)
	totalQuery = r.db.Rebind(totalQuery)

	var totals struct {
		Count     int     `db:"count"`
		Aggregate float64 `db:"aggregate"`
	}
	if err := r.db.GetContext(ctx, &totals, totalQuery, whereArgs...); err != nil {
		return nil, err
	}

	// Main grouped query
	query := fmt.Sprintf(`
		SELECT %s
		FROM issues i
		LEFT JOIN team_statuses ts ON ts.id = i.status_id
		%s
		%s
		%s
		ORDER BY cnt DESC`,
		strings.Join(selectParts, ", "),
		joinClause,
		whereClause,
		groupClause,
	)
	query = r.db.Rebind(query)

	type row struct {
		SlKey   *string  `db:"sl_key"`
		SlLabel *string  `db:"sl_label"`
		SlColor *string  `db:"sl_color"`
		SgKey   *string  `db:"sg_key"`
		SgLabel *string  `db:"sg_label"`
		SgColor *string  `db:"sg_color"`
		Cnt     int      `db:"cnt"`
		Val     float64  `db:"val"`
		P50     *float64 `db:"p50"`
		P75     *float64 `db:"p75"`
		P95     *float64 `db:"p95"`
	}

	var rows []row
	if err := r.db.SelectContext(ctx, &rows, query, whereArgs...); err != nil {
		return nil, err
	}

	groups := []dto.AnalyticsGroup{}
	groupMap := map[string]*dto.AnalyticsGroup{}
	groupOrder := []string{}

	hasSegment := params.Segment != "none"

	for _, row := range rows {
		slKey := "__none__"
		if row.SlKey != nil {
			slKey = *row.SlKey
		}
		slLabel := formatLabel(slKey, row.SlLabel)
		slColor := ""
		if row.SlColor != nil {
			slColor = *row.SlColor
		}

		if _, ok := groupMap[slKey]; !ok {
			groupMap[slKey] = &dto.AnalyticsGroup{
				Key:      slKey,
				Label:    slLabel,
				Color:    slColor,
				Segments: []dto.AnalyticsSegment{},
			}
			groupOrder = append(groupOrder, slKey)
		}
		g := groupMap[slKey]
		g.Count += row.Cnt
		g.Value += row.Val * float64(row.Cnt)

		// Group-level aggregate/percentiles: only when no segments (row-level data is per-segment)
		if !hasSegment && params.Measure != "issue_count" {
			agg := row.Val
			if g.Aggregate == nil || agg > *g.Aggregate {
				g.Aggregate = &agg
			}
			if row.P50 != nil {
				if g.P50 == nil || *row.P50 > *g.P50 {
					g.P50 = row.P50
				}
			}
			if row.P75 != nil {
				if g.P75 == nil || *row.P75 > *g.P75 {
					g.P75 = row.P75
				}
			}
			if row.P95 != nil {
				if g.P95 == nil || *row.P95 > *g.P95 {
					g.P95 = row.P95
				}
			}
		}

		if hasSegment && row.SgKey != nil {
			sgKey := *row.SgKey
			sgLabel := formatLabel(sgKey, row.SgLabel)
			sgColor := ""
			if row.SgColor != nil {
				sgColor = *row.SgColor
			}
			seg := dto.AnalyticsSegment{
				Key:   sgKey,
				Label: sgLabel,
				Color: sgColor,
				Count: row.Cnt,
				Value: row.Val,
			}
			if params.Measure != "issue_count" {
				v := row.Val
				seg.Aggregate = &v
				seg.P50 = row.P50
				seg.P75 = row.P75
				seg.P95 = row.P95
			}
			g.Segments = append(g.Segments, seg)
		}
	}

	if hasSegment {
		summarySelect := sliceSelect
		summaryGroup := sliceGroup
		if summarySelect == "" {
			summarySelect = "'__none__'::text AS sl_key, 'All'::text AS sl_label, NULL::text AS sl_color"
		}
		summaryGroupClause := ""
		if summaryGroup != "" {
			summaryGroupClause = "GROUP BY " + summaryGroup
		}
		summaryQuery := fmt.Sprintf(`
			SELECT %s,
			       COUNT(*) AS count,
			       AVG(%s) AS aggregate,
			       percentile_cont(0.50) WITHIN GROUP (ORDER BY %s) AS p50,
			       percentile_cont(0.75) WITHIN GROUP (ORDER BY %s) AS p75,
			       percentile_cont(0.95) WITHIN GROUP (ORDER BY %s) AS p95
			FROM issues i
			LEFT JOIN team_statuses ts ON ts.id = i.status_id
			%s
			%s
			%s`,
			summarySelect, valueExpr, valueExpr, valueExpr, valueExpr,
			sliceJoin, whereClause, summaryGroupClause,
		)
		summaryQuery = r.db.Rebind(summaryQuery)
		var summaries []struct {
			Key       string  `db:"sl_key"`
			Label     *string `db:"sl_label"`
			Color     *string `db:"sl_color"`
			Count     int     `db:"count"`
			Aggregate float64 `db:"aggregate"`
			P50       float64 `db:"p50"`
			P75       float64 `db:"p75"`
			P95       float64 `db:"p95"`
		}
		if err := r.db.SelectContext(ctx, &summaries, summaryQuery, whereArgs...); err != nil {
			return nil, err
		}
		for _, summary := range summaries {
			if group := groupMap[summary.Key]; group != nil {
				group.Count = summary.Count
				if params.Measure == "issue_count" {
					group.Value = float64(summary.Count)
					continue
				}
				aggregate := summary.Aggregate
				p50, p75, p95 := summary.P50, summary.P75, summary.P95
				group.Value = aggregate
				group.Aggregate = &aggregate
				group.P50 = &p50
				group.P75 = &p75
				group.P95 = &p95
			}
		}
	}

	for _, gk := range groupOrder {
		g := groupMap[gk]
		if g.Count > 0 && !hasSegment {
			g.Value = g.Value / float64(g.Count)
		}
		if params.Measure == "issue_count" {
			g.Value = float64(g.Count)
		}
		if g.Aggregate == nil && isDurationMeasure(params.Measure) {
			z := 0.0
			g.Aggregate = &z
			if !hasSegment {
				g.P50 = &z
				g.P75 = &z
				g.P95 = &z
			}
		}
		if isDurationMeasure(params.Measure) && hasSegment && g.Aggregate == nil {
			g.Aggregate = &g.Value
		}
		groups = append(groups, *g)
	}
	if groups == nil {
		groups = []dto.AnalyticsGroup{}
	}

	aggregate := totals.Aggregate
	if params.Measure == "issue_count" {
		aggregate = float64(totals.Count)
	}

	// Points for duration/age measures
	points := []dto.AnalyticsPoint{}
	if isDurationMeasure(params.Measure) {
		points, err = r.insightPoints(ctx, workspaceID, params, where, whereArgs, dateCol, valueExpr)
		if err != nil {
			return nil, err
		}
	}

	return &dto.AnalyticsInsightsResponse{
		Measure:    params.Measure,
		Slice:      params.Slice,
		Segment:    params.Segment,
		Unit:       unit,
		TotalCount: totals.Count,
		Aggregate:  aggregate,
		Groups:     groups,
		Points:     points,
	}, nil
}

func (r *AnalyticsRepository) insightPoints(ctx context.Context, workspaceID string, params *dto.AnalyticsInsightsParams, where []string, whereArgs []interface{}, dateCol, valueExpr string) ([]dto.AnalyticsPoint, error) {
	slicePointExpr, sliceJoin1, _, _ := r.dimensionPointParts(params.Slice, "sl")
	segPointExpr, segJoin1, _, _ := r.dimensionPointParts(params.Segment, "sg")

	pointSelects := []string{
		"i.id::text AS issue_id",
		"i.identifier_text",
		"i.title",
		fmt.Sprintf("(%s) AS value", valueExpr),
	}
	if slicePointExpr != "" {
		pointSelects = append(pointSelects, slicePointExpr+" AS slice_key")
	} else {
		pointSelects = append(pointSelects, "'' AS slice_key")
	}
	if segPointExpr != "" {
		pointSelects = append(pointSelects, segPointExpr+" AS segment_key")
	} else {
		pointSelects = append(pointSelects, "'' AS segment_key")
	}

	pointJoins := []string{}
	if sliceJoin1 != "" {
		pointJoins = append(pointJoins, sliceJoin1)
	}
	if segJoin1 != "" {
		pointJoins = append(pointJoins, segJoin1)
	}

	whereClause := ""
	if len(where) > 0 {
		whereClause = "WHERE " + strings.Join(where, " AND ")
	}
	joinClause := ""
	if len(pointJoins) > 0 {
		joinClause = strings.Join(pointJoins, "\n")
	}

	query := fmt.Sprintf(`
		SELECT %s
		FROM issues i
		LEFT JOIN team_statuses ts ON ts.id = i.status_id
		%s
		%s
		AND %s IS NOT NULL
		ORDER BY value DESC
		LIMIT 100`,
		strings.Join(pointSelects, ", "),
		joinClause,
		whereClause,
		dateCol,
	)
	query = r.db.Rebind(query)

	type pointRow struct {
		IssueID    string  `db:"issue_id"`
		Identifier string  `db:"identifier_text"`
		Title      string  `db:"title"`
		Value      float64 `db:"value"`
		SliceKey   string  `db:"slice_key"`
		SegmentKey string  `db:"segment_key"`
	}

	var rows []pointRow
	if err := r.db.SelectContext(ctx, &rows, query, whereArgs...); err != nil {
		return nil, err
	}

	points := make([]dto.AnalyticsPoint, 0, len(rows))
	for _, row := range rows {
		points = append(points, dto.AnalyticsPoint{
			IssueID:    row.IssueID,
			Identifier: row.Identifier,
			Title:      row.Title,
			Value:      row.Value,
			SliceKey:   row.SliceKey,
			SegmentKey: row.SegmentKey,
		})
	}
	return points, nil
}

func (r *AnalyticsRepository) dimensionPointParts(dim, alias string) (selectPart, joinPart, groupCol, labelCol string) {
	switch dim {
	case "none":
		return "", "", "", ""
	case "status_type":
		return fmt.Sprintf("COALESCE(ts.category, 'backlog')"), "", "", ""
	case "status":
		return fmt.Sprintf("i.status_id::text"), "", "", ""
	case "priority":
		return "i.priority::text", "", "", ""
	case "assignee":
		return fmt.Sprintf("COALESCE(ia_%s.user_id::text, 'unassigned')", alias),
			fmt.Sprintf("LEFT JOIN issue_assignees ia_%s ON ia_%s.issue_id = i.id", alias, alias),
			"", ""
	case "team":
		return "i.team_id::text", "", "", ""
	case "project":
		return "COALESCE(i.project_id::text, '__null__')", "", "", ""
	case "cycle":
		return "COALESCE(i.cycle_id::text, '__null__')", "", "", ""
	case "label":
		return fmt.Sprintf("COALESCE(l_%s.id::text, '__null__')", alias),
			fmt.Sprintf("LEFT JOIN issue_labels il_%s ON il_%s.issue_id = i.id LEFT JOIN labels l_%s ON l_%s.id = il_%s.label_id AND l_%s.deleted_at IS NULL", alias, alias, alias, alias, alias, alias),
			"", ""
	case "creator":
		return "i.creator_id::text", "", "", ""
	}
	return "", "", "", ""
}

func formatLabel(key string, label *string) string {
	if label != nil && *label != "" {
		return *label
	}
	switch key {
	case "__null__", "null", "":
		return "None"
	case "unassigned":
		return "Unassigned"
	case "__none__":
		return "All"
	}
	return key
}

func isDurationMeasure(measure string) bool {
	return measure == "issue_age" || measure == "lead_time" || measure == "cycle_time" || measure == "triage_time"
}

func (r *AnalyticsRepository) measureDateCol(measure string) string {
	switch measure {
	case "issue_age", "issue_count":
		return "i.created_at"
	case "lead_time", "cycle_time":
		return "i.completed_at"
	case "triage_time":
		return "i.triaged_at"
	default:
		return "i.created_at"
	}
}

func (r *AnalyticsRepository) measureExpr(measure string) (aggExpr, valueExpr, unit string) {
	switch measure {
	case "issue_age":
		return "AVG(EXTRACT(EPOCH FROM (NOW() - i.created_at)) / 3600.0)",
			"EXTRACT(EPOCH FROM (NOW() - i.created_at)) / 3600.0",
			"hours"
	case "lead_time":
		return "AVG(EXTRACT(EPOCH FROM (i.completed_at - i.created_at)) / 3600.0)",
			"EXTRACT(EPOCH FROM (i.completed_at - i.created_at)) / 3600.0",
			"hours"
	case "cycle_time":
		return "AVG(EXTRACT(EPOCH FROM (i.completed_at - i.started_at)) / 3600.0)",
			"EXTRACT(EPOCH FROM (i.completed_at - i.started_at)) / 3600.0",
			"hours"
	case "triage_time":
		return "AVG(EXTRACT(EPOCH FROM (i.triaged_at - i.created_at)) / 3600.0)",
			"EXTRACT(EPOCH FROM (i.triaged_at - i.created_at)) / 3600.0",
			"hours"
	default:
		return "COUNT(*)", "1", "issues"
	}
}

func (r *AnalyticsRepository) dimensionParts(dim, alias string) (selectPart, joinPart, groupCol, labelCol string) {
	switch dim {
	case "none":
		return "", "", "", ""
	case "status_type":
		return fmt.Sprintf("COALESCE(ts.category, 'backlog') AS %s_key, COALESCE(ts.category, 'backlog') AS %s_label, NULL AS %s_color", alias, alias, alias),
			"",
			"COALESCE(ts.category, 'backlog')",
			""
	case "status":
		return fmt.Sprintf("i.status_id::text AS %s_key, ts.name AS %s_label, ts.color AS %s_color", alias, alias, alias),
			"",
			"i.status_id::text, ts.name, ts.color",
			""
	case "priority":
		return fmt.Sprintf("i.priority::text AS %s_key, CASE i.priority WHEN 0 THEN 'No priority' WHEN 1 THEN 'Urgent' WHEN 2 THEN 'High' WHEN 3 THEN 'Medium' WHEN 4 THEN 'Low' END AS %s_label, NULL AS %s_color", alias, alias, alias),
			"",
			"i.priority",
			""
	case "assignee":
		return fmt.Sprintf("COALESCE(ia_%s.user_id::text, 'unassigned') AS %s_key, COALESCE(u_%s.display_name, u_%s.name, 'Unassigned') AS %s_label, NULL AS %s_color", alias, alias, alias, alias, alias, alias),
			fmt.Sprintf("LEFT JOIN issue_assignees ia_%s ON ia_%s.issue_id = i.id LEFT JOIN users u_%s ON u_%s.id = ia_%s.user_id", alias, alias, alias, alias, alias),
			fmt.Sprintf("ia_%s.user_id, u_%s.display_name, u_%s.name", alias, alias, alias),
			""
	case "team":
		return fmt.Sprintf("i.team_id::text AS %s_key, t_%s.name AS %s_label, NULL AS %s_color", alias, alias, alias, alias),
			fmt.Sprintf("LEFT JOIN teams t_%s ON t_%s.id = i.team_id", alias, alias),
			fmt.Sprintf("i.team_id, t_%s.name", alias),
			""
	case "project":
		return fmt.Sprintf("COALESCE(i.project_id::text, '__null__') AS %s_key, COALESCE(p_%s.name, 'No project') AS %s_label, NULL AS %s_color", alias, alias, alias, alias),
			fmt.Sprintf("LEFT JOIN projects p_%s ON p_%s.id = i.project_id", alias, alias),
			fmt.Sprintf("i.project_id, p_%s.name", alias),
			""
	case "cycle":
		return fmt.Sprintf("COALESCE(i.cycle_id::text, '__null__') AS %s_key, COALESCE(c_%s.name, 'No cycle') AS %s_label, NULL AS %s_color", alias, alias, alias, alias),
			fmt.Sprintf("LEFT JOIN cycles c_%s ON c_%s.id = i.cycle_id", alias, alias),
			fmt.Sprintf("i.cycle_id, c_%s.name", alias),
			""
	case "label":
		return fmt.Sprintf("COALESCE(l_%s.id::text, '__null__') AS %s_key, COALESCE(l_%s.name, 'No label') AS %s_label, COALESCE(l_%s.color, '') AS %s_color", alias, alias, alias, alias, alias, alias),
			fmt.Sprintf("LEFT JOIN issue_labels il_%s ON il_%s.issue_id = i.id LEFT JOIN labels l_%s ON l_%s.id = il_%s.label_id AND l_%s.deleted_at IS NULL", alias, alias, alias, alias, alias, alias),
			fmt.Sprintf("l_%s.id, l_%s.name, l_%s.color", alias, alias, alias),
			""
	case "creator":
		return fmt.Sprintf("i.creator_id::text AS %s_key, COALESCE(cr_%s.display_name, cr_%s.name, 'Unknown') AS %s_label, NULL AS %s_color", alias, alias, alias, alias, alias),
			fmt.Sprintf("LEFT JOIN users cr_%s ON cr_%s.id = i.creator_id", alias, alias),
			fmt.Sprintf("i.creator_id, cr_%s.display_name, cr_%s.name", alias, alias),
			""
	}
	return "", "", "", ""
}

func (r *AnalyticsRepository) buildInsightWhere(workspaceID string, params *dto.AnalyticsInsightsParams) ([]string, []interface{}, error) {
	where := []string{"i.workspace_id = $1"}
	args := []interface{}{workspaceID}
	idx := 2

	dateCol := r.measureDateCol(params.Measure)

	if params.From != "" {
		where = append(where, fmt.Sprintf("%s >= $%d::date", dateCol, idx))
		args = append(args, params.From)
		idx++
	}
	if params.To != "" {
		where = append(where, fmt.Sprintf("%s < ($%d::date + INTERVAL '1 day')", dateCol, idx))
		args = append(args, params.To)
		idx++
	}
	if params.TeamID != nil && *params.TeamID != "" {
		where = append(where, fmt.Sprintf("i.team_id = $%d", idx))
		args = append(args, *params.TeamID)
		idx++
	}
	if params.ProjectID != nil && *params.ProjectID != "" {
		if *params.ProjectID == "none" {
			where = append(where, "i.project_id IS NULL")
		} else {
			where = append(where, fmt.Sprintf("i.project_id = $%d", idx))
			args = append(args, *params.ProjectID)
			idx++
		}
	}
	if params.CycleID != nil && *params.CycleID != "" {
		if *params.CycleID == "none" {
			where = append(where, "i.cycle_id IS NULL")
		} else {
			where = append(where, fmt.Sprintf("i.cycle_id = $%d", idx))
			args = append(args, *params.CycleID)
			idx++
		}
	}
	if params.AssigneeID != nil && *params.AssigneeID != "" {
		if *params.AssigneeID == "none" {
			where = append(where, "NOT EXISTS (SELECT 1 FROM issue_assignees ia WHERE ia.issue_id = i.id)")
		} else {
			where = append(where, fmt.Sprintf("EXISTS (SELECT 1 FROM issue_assignees ia WHERE ia.issue_id = i.id AND ia.user_id = $%d)", idx))
			args = append(args, *params.AssigneeID)
			idx++
		}
	}
	if params.CreatorID != nil && *params.CreatorID != "" {
		where = append(where, fmt.Sprintf("i.creator_id = $%d", idx))
		args = append(args, *params.CreatorID)
		idx++
	}
	if params.StatusID != nil && *params.StatusID != "" {
		where = append(where, fmt.Sprintf("i.status_id = ANY(string_to_array($%d, ',')::uuid[])", idx))
		args = append(args, *params.StatusID)
		idx++
	}
	if params.StatusType != nil && *params.StatusType != "" {
		where = append(where, fmt.Sprintf("ts.category = $%d", idx))
		args = append(args, *params.StatusType)
		idx++
	}
	if params.Priority != nil && *params.Priority != "" {
		where = append(where, fmt.Sprintf("i.priority = ANY(string_to_array($%d, ',')::int[])", idx))
		args = append(args, *params.Priority)
		idx++
	}
	if params.LabelID != nil && *params.LabelID != "" {
		where = append(where, fmt.Sprintf("EXISTS (SELECT 1 FROM issue_labels il WHERE il.issue_id = i.id AND il.label_id = $%d)", idx))
		args = append(args, *params.LabelID)
		idx++
	}

	return where, args, nil
}

func (r *AnalyticsRepository) Burnup(ctx context.Context, workspaceID string, params *dto.AnalyticsBurnupParams) (*dto.AnalyticsBurnupResponse, error) {
	interval := params.Interval
	intervalSQL := map[string]string{"day": "1 day", "week": "1 week", "month": "1 month"}[interval]

	// Base issue filter conditions (for created scope)
	issueWhere := []string{"i.workspace_id = $1"}
	whereArgs := []interface{}{workspaceID}
	idx := 2
	teamArgIdx := 0
	projectArgIdx := 0
	cycleArgIdx := 0
	assigneeArgIdx := 0

	if params.TeamID != nil && *params.TeamID != "" {
		teamArgIdx = idx
		issueWhere = append(issueWhere, fmt.Sprintf("i.team_id = $%d", idx))
		whereArgs = append(whereArgs, *params.TeamID)
		idx++
	}
	if params.ProjectID != nil && *params.ProjectID != "" {
		if *params.ProjectID == "none" {
			issueWhere = append(issueWhere, "i.project_id IS NULL")
		} else {
			projectArgIdx = idx
			issueWhere = append(issueWhere, fmt.Sprintf("i.project_id = $%d", idx))
			whereArgs = append(whereArgs, *params.ProjectID)
			idx++
		}
	}
	if params.CycleID != nil && *params.CycleID != "" {
		if *params.CycleID == "none" {
			issueWhere = append(issueWhere, "i.cycle_id IS NULL")
		} else {
			cycleArgIdx = idx
			issueWhere = append(issueWhere, fmt.Sprintf("i.cycle_id = $%d", idx))
			whereArgs = append(whereArgs, *params.CycleID)
			idx++
		}
	}
	if params.AssigneeID != nil && *params.AssigneeID != "" {
		if *params.AssigneeID == "none" {
			issueWhere = append(issueWhere, "NOT EXISTS (SELECT 1 FROM issue_assignees ia WHERE ia.issue_id = i.id)")
		} else {
			assigneeArgIdx = idx
			issueWhere = append(issueWhere, fmt.Sprintf("EXISTS (SELECT 1 FROM issue_assignees ia WHERE ia.issue_id = i.id AND ia.user_id = $%d)", idx))
			whereArgs = append(whereArgs, *params.AssigneeID)
			idx++
		}
	}
	includeSubIssues := params.IncludeSubIssues == nil || *params.IncludeSubIssues
	if !includeSubIssues {
		issueWhere = append(issueWhere, "i.parent_id IS NULL")
	}
	includeTriage := params.IncludeTriage == nil || *params.IncludeTriage
	if !includeTriage {
		issueWhere = append(issueWhere, "i.triaged = TRUE")
	}

	// Event filter: use workspace_id from event snapshot
	eventWhere := []string{"le.workspace_id = $1"}
	if teamArgIdx != 0 {
		eventWhere = append(eventWhere, fmt.Sprintf("le.team_id = $%d", teamArgIdx))
	}
	if params.ProjectID != nil && *params.ProjectID == "none" {
		eventWhere = append(eventWhere, "le.project_id IS NULL")
	} else if projectArgIdx != 0 {
		eventWhere = append(eventWhere, fmt.Sprintf("le.project_id = $%d", projectArgIdx))
	}
	if params.CycleID != nil && *params.CycleID == "none" {
		eventWhere = append(eventWhere, "le.cycle_id IS NULL")
	} else if cycleArgIdx != 0 {
		eventWhere = append(eventWhere, fmt.Sprintf("le.cycle_id = $%d", cycleArgIdx))
	}
	if params.AssigneeID != nil && *params.AssigneeID == "none" {
		eventWhere = append(eventWhere, "NOT EXISTS (SELECT 1 FROM issue_assignees event_ia WHERE event_ia.issue_id = le.issue_id)")
	} else if assigneeArgIdx != 0 {
		eventWhere = append(eventWhere, fmt.Sprintf("EXISTS (SELECT 1 FROM issue_assignees event_ia WHERE event_ia.issue_id = le.issue_id AND event_ia.user_id = $%d)", assigneeArgIdx))
	}
	if !includeSubIssues {
		eventWhere = append(eventWhere, "EXISTS (SELECT 1 FROM issues event_i WHERE event_i.id = le.issue_id AND event_i.parent_id IS NULL)")
	}
	if !includeTriage {
		eventWhere = append(eventWhere, "EXISTS (SELECT 1 FROM issues event_i WHERE event_i.id = le.issue_id AND event_i.triaged = TRUE)")
	}
	eventWhereClause := strings.Join(eventWhere, " AND ")

	fromIdx := idx
	toIdx := idx + 1
	allArgs := append(whereArgs, params.From, params.To)

	query := fmt.Sprintf(`
		WITH buckets AS (
			SELECT generate_series(
				$%d::date,
				$%d::date,
				'%s'::interval
			)::date AS dt
		)
		SELECT
			b.dt::text AS dt,
			COALESCE((SELECT COUNT(*) FROM issue_lifecycle_events le
				WHERE %s
				  AND le.event_type = 'created'
				  AND le.created_at::date >= b.dt
				  AND le.created_at::date < (b.dt + '%s'::interval)), 0) AS created,
			COALESCE((SELECT COUNT(*) FROM issue_lifecycle_events le
				WHERE %s
				  AND le.event_type = 'created'
				  AND le.created_at::date <= (b.dt + '%s'::interval - '1 day'::interval)), 0) AS total_created,
			COALESCE((SELECT COALESCE(SUM(net), 0) FROM (
				SELECT le.issue_id,
					CASE
						WHEN le.to_category = 'completed'
						 AND (le.from_category IS NULL OR le.from_category != 'completed')
						THEN 1
						WHEN le.from_category = 'completed'
						 AND (le.to_category IS NULL OR le.to_category != 'completed')
						THEN -1
						ELSE 0
					END AS net
				FROM issue_lifecycle_events le
				WHERE %s
				  AND le.created_at::date >= b.dt
				  AND le.created_at::date < (b.dt + '%s'::interval)
			) net_changes WHERE net_changes.net != 0), 0) AS completed,
			COALESCE((SELECT COALESCE(SUM(net), 0) FROM (
				SELECT le.issue_id,
					CASE
						WHEN le.to_category = 'completed'
						 AND (le.from_category IS NULL OR le.from_category != 'completed')
						THEN 1
						WHEN le.from_category = 'completed'
						 AND (le.to_category IS NULL OR le.to_category != 'completed')
						THEN -1
						ELSE 0
					END AS net
				FROM issue_lifecycle_events le
				WHERE %s
				  AND le.created_at::date <= (b.dt + '%s'::interval - '1 day'::interval)
			) net_changes WHERE net_changes.net != 0), 0) AS total_completed
		FROM buckets b
		ORDER BY b.dt`,
		fromIdx, toIdx, intervalSQL,
		eventWhereClause, intervalSQL,
		eventWhereClause, intervalSQL,
		eventWhereClause, intervalSQL,
		eventWhereClause, intervalSQL,
	)

	query = r.db.Rebind(query)

	type burnupRow struct {
		Dt             string `db:"dt"`
		Created        int    `db:"created"`
		Completed      int    `db:"completed"`
		TotalCreated   int    `db:"total_created"`
		TotalCompleted int    `db:"total_completed"`
	}

	var rows []burnupRow
	if err := r.db.SelectContext(ctx, &rows, query, allArgs...); err != nil {
		return nil, err
	}

	points := make([]dto.AnalyticsBurnupPoint, 0, len(rows))
	for _, row := range rows {
		scope := row.TotalCreated - row.TotalCompleted
		if scope < 0 {
			scope = 0
		}
		points = append(points, dto.AnalyticsBurnupPoint{
			Date:           row.Dt,
			Created:        row.Created,
			Completed:      row.Completed,
			TotalCreated:   row.TotalCreated,
			TotalCompleted: row.TotalCompleted,
			Scope:          scope,
		})
	}

	return &dto.AnalyticsBurnupResponse{
		Interval: params.Interval,
		From:     params.From,
		To:       params.To,
		Points:   points,
	}, nil
}
