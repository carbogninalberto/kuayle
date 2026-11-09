package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/lib/pq"
)

type IssueRepository struct {
	db *sqlx.DB
}

func NewIssueRepository(db *sqlx.DB) *IssueRepository {
	return &IssueRepository{db: db}
}

func (r *IssueRepository) Create(ctx context.Context, tx *sqlx.Tx, issue *domain.Issue) error {
	query := `
		INSERT INTO issues (id, workspace_id, team_id, project_id, cycle_id, number, identifier_text, title, description, status, status_id, priority, creator_id, assignee_id, parent_id, due_date, sort_order, triaged)
		SELECT $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18 WHERE ` + issueReferencesVisible(ctx, issue) + `
		RETURNING created_at, updated_at`
	return tx.QueryRowContext(ctx, query,
		issue.ID, issue.WorkspaceID, issue.TeamID, issue.ProjectID, issue.CycleID,
		issue.Number, issue.Identifier, issue.Title, issue.Description,
		issue.Status, issue.StatusID, issue.Priority, issue.CreatorID, issue.AssigneeID,
		issue.ParentID, issue.DueDate, issue.SortOrder, issue.Triaged,
	).Scan(&issue.CreatedAt, &issue.UpdatedAt)
}

func (r *IssueRepository) NextNumber(ctx context.Context, tx *sqlx.Tx, teamID uuid.UUID) (int, error) {
	// Advisory lock per team to guarantee sequential numbering
	var num int
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1::text))`, teamID)
	if err != nil {
		return 0, err
	}
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(number), 0) + 1 FROM issues WHERE team_id = $1`, teamID).Scan(&num)
	return num, err
}

// TeamKey reads the team's current key inside the transaction. Call it after
// NextNumber: that holds the per-team lock a key rename also takes, so the key
// cannot change before the new issue is committed.
func (r *IssueRepository) TeamKey(ctx context.Context, tx *sqlx.Tx, teamID uuid.UUID) (string, error) {
	var key string
	err := tx.QueryRowContext(ctx, `SELECT key FROM teams WHERE id = $1`, teamID).Scan(&key)
	return key, err
}

func (r *IssueRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Issue, error) {
	var issue domain.Issue
	err := r.db.GetContext(ctx, &issue, `SELECT * FROM issues WHERE id = $1 AND `+teamVisible(ctx, "issues.team_id"), id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &issue, err
}

// splitIdentifier splits "ENG-12" into its team key and number, for looking up a
// retired team key. A text that is not of that shape gives ("", -1), which
// matches no alias.
func splitIdentifier(identifier string) (string, int) {
	if i := strings.LastIndex(identifier, "-"); i > 0 {
		// 32 bits: the column is an int4, a longer number cannot match and must
		// not reach the database as an encode error.
		if n, err := strconv.ParseInt(identifier[i+1:], 10, 32); err == nil {
			return identifier[:i], int(n)
		}
	}
	return "", -1
}

func (r *IssueRepository) GetByIdentifier(ctx context.Context, workspaceID uuid.UUID, identifier string) (*domain.Issue, error) {
	var issue domain.Issue
	// The current identifier first, on the (workspace_id, identifier_text) index.
	err := r.db.GetContext(ctx, &issue, `SELECT * FROM issues WHERE workspace_id = $1 AND identifier_text = $2 AND `+teamVisible(ctx, "issues.team_id"), workspaceID, identifier)
	if !errors.Is(err, sql.ErrNoRows) {
		if err != nil {
			return nil, err
		}
		return &issue, nil
	}
	// On a miss, a retired team key (an alias) still resolves: ENG-12 finds
	// PLT-12 after ENG was renamed to PLT. The alias gives the team, and the
	// issue is found by (team_id, number).
	oldKey, number := splitIdentifier(identifier)
	if oldKey == "" {
		return nil, nil
	}
	err = r.db.GetContext(ctx, &issue, `SELECT issues.* FROM team_key_aliases a JOIN issues ON issues.team_id = a.team_id AND issues.number = $3 AND issues.workspace_id = a.workspace_id WHERE a.workspace_id = $1 AND a.old_key = $2 AND `+teamVisible(ctx, "issues.team_id"), workspaceID, oldKey, number)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &issue, err
}

func (r *IssueRepository) List(ctx context.Context, workspaceID uuid.UUID, params dto.IssueFilterParams) ([]domain.Issue, int, error) {
	where := []string{"i.workspace_id = :workspace_id", teamVisible(ctx, "i.team_id")}
	args := map[string]interface{}{"workspace_id": workspaceID}

	// Multi-value status filter (comma-separated) — supports both legacy slugs and status_id UUIDs
	if params.Status != "" {
		statuses := strings.Split(params.Status, ",")
		// Detect if the values are UUIDs (status_id) or legacy slugs
		col := "i.status"
		if _, err := uuid.Parse(strings.TrimSpace(statuses[0])); err == nil {
			col = "i.status_id"
		}
		if len(statuses) == 1 {
			where = append(where, col+" = :status")
			args["status"] = strings.TrimSpace(statuses[0])
		} else {
			placeholders := make([]string, len(statuses))
			for i, s := range statuses {
				key := fmt.Sprintf("status_%d", i)
				placeholders[i] = ":" + key
				args[key] = strings.TrimSpace(s)
			}
			where = append(where, fmt.Sprintf("%s IN (%s)", col, strings.Join(placeholders, ",")))
		}
	}
	if params.StatusType != "" {
		where = append(where, issueStatusCategorySubqueryExpr("i")+" = :status_type")
		args["status_type"] = params.StatusType
	}
	// Multi-value priority filter (comma-separated)
	if params.Priority != "" {
		priorities := strings.Split(params.Priority, ",")
		if len(priorities) == 1 {
			where = append(where, "i.priority = :priority")
			args["priority"] = priorities[0]
		} else {
			placeholders := make([]string, len(priorities))
			for i, p := range priorities {
				key := fmt.Sprintf("priority_%d", i)
				placeholders[i] = ":" + key
				args[key] = strings.TrimSpace(p)
			}
			where = append(where, fmt.Sprintf("i.priority IN (%s)", strings.Join(placeholders, ",")))
		}
	}
	if params.AssigneeID != "" {
		if params.AssigneeID == "none" {
			where = append(where, "NOT EXISTS (SELECT 1 FROM issue_assignees ia WHERE ia.issue_id = i.id)")
		} else {
			where = append(where, "EXISTS (SELECT 1 FROM issue_assignees ia WHERE ia.issue_id = i.id AND ia.user_id = :assignee_id)")
			args["assignee_id"] = params.AssigneeID
		}
	}
	if params.CreatorID != "" {
		where = append(where, "i.creator_id = :creator_id")
		args["creator_id"] = params.CreatorID
	}
	if params.TeamID != "" {
		where = append(where, "i.team_id = :team_id")
		args["team_id"] = params.TeamID
	}
	if params.ProjectID != "" {
		if params.ProjectID == "none" {
			where = append(where, "i.project_id IS NULL")
		} else {
			where = append(where, "i.project_id = :project_id")
			args["project_id"] = params.ProjectID
		}
	}
	if params.CycleID != "" {
		if params.CycleID == "none" {
			where = append(where, "i.cycle_id IS NULL")
		} else {
			where = append(where, "i.cycle_id = :cycle_id")
			args["cycle_id"] = params.CycleID
		}
	}
	if params.LabelID != "" {
		if params.LabelID == "none" {
			where = append(where, `NOT EXISTS (
				SELECT 1 FROM issue_labels il
				INNER JOIN labels l ON l.id = il.label_id AND l.deleted_at IS NULL
				WHERE il.issue_id = i.id AND `+labelVisible(ctx, "l")+`
			)`)
		} else {
			where = append(where, "EXISTS (SELECT 1 FROM issue_labels il JOIN labels l ON l.id = il.label_id WHERE il.issue_id = i.id AND il.label_id = :label_id AND "+labelVisible(ctx, "l")+")")
			args["label_id"] = params.LabelID
		}
	}
	if params.Search != "" {
		searchFields := []string{
			"i.title ILIKE :search",
			"i.identifier_text ILIKE :search",
			// Retired identifiers (ENG-12 after ENG became PLT): the matching
			// (team, number) pairs are resolved once, not probed for every row.
			`(i.team_id, i.number) IN (
				SELECT tka.team_id, ai.number FROM team_key_aliases tka
				INNER JOIN issues ai ON ai.team_id = tka.team_id
				WHERE tka.workspace_id = :workspace_id AND (tka.old_key || '-' || ai.number) ILIKE :search
			)`,
			"i.description ILIKE :search",
			"i.status ILIKE :search",
			"CAST(i.number AS TEXT) ILIKE :search",
			"CAST(i.priority AS TEXT) ILIKE :search",
			"CASE i.priority WHEN 0 THEN 'No priority' WHEN 1 THEN 'Urgent' WHEN 2 THEN 'High' WHEN 3 THEN 'Medium' WHEN 4 THEN 'Low' END ILIKE :search",
			"TO_CHAR(i.due_date, 'YYYY-MM-DD') ILIKE :search",
			`EXISTS (
				SELECT 1 FROM team_statuses ts
				WHERE ts.id = i.status_id
				AND (ts.name ILIKE :search OR ts.slug ILIKE :search OR ts.category ILIKE :search)
			)`,
			`EXISTS (
				SELECT 1 FROM projects p
				WHERE p.id = i.project_id
				AND (p.name ILIKE :search OR p.description ILIKE :search OR p.status ILIKE :search)
			)`,
			`EXISTS (
				SELECT 1 FROM cycles c
				WHERE c.id = i.cycle_id
				AND (
					c.name ILIKE :search OR c.status ILIKE :search OR c.description ILIKE :search
					OR c.goals ILIKE :search OR c.retrospective ILIKE :search
					OR TO_CHAR(c.start_date, 'YYYY-MM-DD') ILIKE :search
					OR TO_CHAR(c.end_date, 'YYYY-MM-DD') ILIKE :search
				)
			)`,
			`EXISTS (
				SELECT 1 FROM users u
				WHERE u.id = i.creator_id
				AND (u.name ILIKE :search OR u.display_name ILIKE :search OR u.email ILIKE :search)
			)`,
			`EXISTS (
				SELECT 1 FROM users u
				WHERE u.id = i.assignee_id
				AND (u.name ILIKE :search OR u.display_name ILIKE :search OR u.email ILIKE :search)
			)`,
			`EXISTS (
				SELECT 1 FROM issue_assignees ia
				INNER JOIN users u ON u.id = ia.user_id
				WHERE ia.issue_id = i.id
				AND (u.name ILIKE :search OR u.display_name ILIKE :search OR u.email ILIKE :search)
			)`,
			`EXISTS (
				SELECT 1 FROM issue_labels il
				INNER JOIN labels l ON l.id = il.label_id
				WHERE il.issue_id = i.id
				AND l.deleted_at IS NULL
				AND (l.name ILIKE :search OR l.description ILIKE :search OR l.color ILIKE :search)
				AND ` + labelVisible(ctx, "l") + `
			)`,
			`EXISTS (
				SELECT 1 FROM teams t
				WHERE t.id = i.team_id
				AND (t.name ILIKE :search OR t.key ILIKE :search OR t.description ILIKE :search)
			)`,
		}
		where = append(where, "("+strings.Join(searchFields, " OR ")+")")
		args["search"] = "%" + params.Search + "%"
	}
	if params.DueBefore != "" {
		where = append(where, "i.due_date <= :due_before")
		args["due_before"] = params.DueBefore
	}
	if params.DueAfter != "" {
		where = append(where, "i.due_date >= :due_after")
		args["due_after"] = params.DueAfter
	}
	if params.Triaged != "" {
		if params.Triaged == "false" {
			where = append(where, "i.triaged = false")
		} else {
			where = append(where, "i.triaged = true")
		}
	}
	if params.ParentID != "" {
		if params.ParentID == "none" {
			where = append(where, "i.parent_id IS NULL")
		} else {
			where = append(where, "i.parent_id = :parent_id")
			args["parent_id"] = params.ParentID
		}
	}
	switch params.SubIssues {
	case "exclude", "top_level":
		where = append(where, "i.parent_id IS NULL")
	case "only":
		where = append(where, "i.parent_id IS NOT NULL")
	case "has_sub_issues":
		where = append(where, "EXISTS (SELECT 1 FROM issues child WHERE child.parent_id = i.id)")
	}

	whereClause := strings.Join(where, " AND ")

	// Count
	countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM issues i WHERE %s`, whereClause)
	countQuery, countArgs, err := sqlx.Named(countQuery, args)
	if err != nil {
		return nil, 0, err
	}
	countQuery = r.db.Rebind(countQuery)
	var total int
	if err := r.db.GetContext(ctx, &total, countQuery, countArgs...); err != nil {
		return nil, 0, err
	}

	// Sort. Group ordering must happen before pagination so lazily loaded pages
	// match the visual grouping applied by the client.
	joins := ""
	if params.GroupBy == "status" || params.Sort == "status" {
		joins = " LEFT JOIN team_statuses ts ON ts.id = i.status_id"
	}

	sortCol := "i.created_at"
	allowedSorts := map[string]string{
		"created_at": "i.created_at",
		"updated_at": "i.updated_at",
		"priority":   "i.priority",
		"sort_order": "i.sort_order",
		"status":     "COALESCE(ts.position, 999)",
		"due_date":   "i.due_date",
	}
	if col, ok := allowedSorts[params.Sort]; ok {
		sortCol = col
	}
	order := "DESC"
	if params.Order == "asc" {
		order = "ASC"
	}
	orderParts := make([]string, 0, 5)
	switch params.GroupBy {
	case "status":
		if rank := statusCategoryRankSQL(params.StatusCategoryOrder, issueStatusCategoryExpr("i", "ts")); rank != "" {
			orderParts = append(orderParts, rank+" ASC")
		}
		orderParts = append(orderParts, "COALESCE(ts.position, 999) ASC", "i.status_id ASC")
	case "priority":
		orderParts = append(orderParts, "i.priority ASC")
	case "assignee":
		orderParts = append(orderParts, "i.assignee_id IS NULL ASC", "i.assignee_id ASC")
	case "project":
		orderParts = append(orderParts, "i.project_id IS NULL ASC", "i.project_id ASC")
	}
	orderParts = append(orderParts, fmt.Sprintf("%s %s", sortCol, order))
	if sortCol != "i.created_at" {
		orderParts = append(orderParts, "i.created_at DESC")
	}
	orderParts = append(orderParts, "i.id DESC")

	params.Defaults()
	args["limit"] = params.PerPage
	args["offset"] = params.Offset()

	dataQuery := fmt.Sprintf(`SELECT i.* FROM issues i%s WHERE %s ORDER BY %s LIMIT :limit OFFSET :offset`, joins, whereClause, strings.Join(orderParts, ", "))
	dataQuery, dataArgs, err := sqlx.Named(dataQuery, args)
	if err != nil {
		return nil, 0, err
	}
	dataQuery = r.db.Rebind(dataQuery)

	var issues []domain.Issue
	if err := r.db.SelectContext(ctx, &issues, dataQuery, dataArgs...); err != nil {
		return nil, 0, err
	}

	return issues, total, nil
}

// Update changes issue properties without writing a previously loaded content snapshot.
// Automations and relationship changes must preserve concurrent content edits.
func (r *IssueRepository) Update(ctx context.Context, issue *domain.Issue) error {
	return r.UpdateContent(ctx, issue, false, false)
}

// UpdateContent writes only the content fields explicitly changed by the editor.
func (r *IssueRepository) UpdateContent(ctx context.Context, issue *domain.Issue, titleChanged, descriptionChanged bool) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	actor, _ := ctx.Value(issueHistoryActorKey{}).(uuid.UUID)
	if actor == uuid.Nil {
		if access, ok := domain.AccessFromContext(ctx); ok {
			actor = access.UserID
		}
	}
	actorValue := ""
	if actor != uuid.Nil {
		actorValue = actor.String()
	}
	// Transaction-local: an actor must never leak to the next pooled request.
	if _, err = tx.ExecContext(ctx, `SELECT set_config('kuayle.history_actor', $1, true)`, actorValue); err != nil {
		return err
	}
	agentValue := ""
	if agent := domain.ActingAgent(ctx); agent != nil {
		agentValue = *agent
	}
	if _, err = tx.ExecContext(ctx, `SELECT set_config('kuayle.history_agent', $1, true)`, agentValue); err != nil {
		return err
	}
	query := `
		UPDATE issues SET
			title = CASE WHEN $14 THEN $1 ELSE title END,
			description = CASE WHEN $15 THEN $2 ELSE description END, status = $3, priority = $4,
			assignee_id = $5, project_id = $6, cycle_id = $7, parent_id = $8,
			due_date = $9, sort_order = $10, triaged = $11,
			status_id = $12, updated_at = NOW()
		WHERE id = $13 AND ` + teamVisible(ctx, "issues.team_id") + " AND " + issueReferencesVisible(ctx, issue) + `
		RETURNING title, description, updated_at`
	err = tx.QueryRowContext(ctx, query,
		issue.Title, issue.Description, issue.Status, issue.Priority,
		issue.AssigneeID, issue.ProjectID, issue.CycleID, issue.ParentID,
		issue.DueDate, issue.SortOrder, issue.Triaged,
		issue.StatusID, issue.ID, titleChanged, descriptionChanged,
	).Scan(&issue.Title, &issue.Description, &issue.UpdatedAt)
	if err != nil {
		return err
	}
	// The UPDATE above holds the issue row lock until commit, so a concurrent
	// SetLabels waits and then checks scope against the new project. Labels
	// scoped to a project the issue is no longer in stop applying, in the same
	// transaction as the move.
	if _, err = tx.ExecContext(ctx, `DELETE FROM issue_labels il USING labels l WHERE il.issue_id = $1 AND l.id = il.label_id
		AND l.project_id IS NOT NULL AND l.project_id IS DISTINCT FROM $2::uuid`, issue.ID, issue.ProjectID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *IssueRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM issues WHERE id = $1 AND `+teamVisible(ctx, "issues.team_id"), id)
	return err
}

func (r *IssueRepository) SetLabels(ctx context.Context, issueID uuid.UUID, labelIDs []uuid.UUID) error {
	// The privacy graph barrier comes before the issue row lock below, the
	// order cascading team and workspace deletes use.
	tx, err := beginConversationTransaction(ctx, r.db)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := requireVisible(ctx, tx, issueVisible(ctx, uuidSQL(issueID))); err != nil {
		return err
	}
	// Serializes with a concurrent project move: the move's UPDATE takes the
	// same row lock (its statement trigger takes the shared barrier first),
	// and the scope check below then sees the committed project.
	if _, err := tx.ExecContext(ctx, `SELECT 1 FROM issues WHERE id = $1 FOR UPDATE`, issueID); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM issue_labels WHERE issue_id = $1`, issueID); err != nil {
		return err
	}

	// Labels from another workspace, or scoped to another project, are ignored
	// rather than attached.
	for _, labelID := range labelIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO issue_labels (issue_id, label_id) SELECT $1, $2 WHERE EXISTS (
			SELECT 1 FROM issues label_issue JOIN labels l ON l.workspace_id = label_issue.workspace_id
			WHERE label_issue.id = $1 AND l.id = $2 AND l.deleted_at IS NULL
			AND (l.project_id IS NULL OR l.project_id = label_issue.project_id))`, issueID, labelID); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (r *IssueRepository) GetLabels(ctx context.Context, issueID uuid.UUID) ([]domain.Label, error) {
	var labels []domain.Label
	query := `SELECT l.* FROM labels l INNER JOIN issue_labels il ON l.id = il.label_id WHERE il.issue_id = $1 AND ` + issueVisible(ctx, "il.issue_id") + ` AND ` + labelVisible(ctx, "l") + ` ORDER BY l.name`
	err := r.db.SelectContext(ctx, &labels, query, issueID)
	return labels, err
}

func (r *IssueRepository) GetLabelsForIssues(ctx context.Context, issueIDs []uuid.UUID) (map[uuid.UUID][]domain.Label, error) {
	if len(issueIDs) == 0 {
		return make(map[uuid.UUID][]domain.Label), nil
	}

	type labelRow struct {
		domain.Label
		IssueID uuid.UUID `db:"issue_id"`
	}
	var rows []labelRow

	query, args, err := sqlx.In(`SELECT l.*, il.issue_id FROM labels l
		INNER JOIN issue_labels il ON l.id = il.label_id
		WHERE il.issue_id IN (?) AND (l.deleted_at IS NULL) AND `+issueVisible(ctx, "il.issue_id")+` AND `+labelVisible(ctx, "l")+` ORDER BY l.name`, issueIDs)
	if err != nil {
		return nil, err
	}
	query = r.db.Rebind(query)

	if err := r.db.SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, err
	}

	result := make(map[uuid.UUID][]domain.Label, len(issueIDs))
	for _, row := range rows {
		result[row.IssueID] = append(result[row.IssueID], row.Label)
	}
	return result, nil
}

func (r *IssueRepository) ListSubIssues(ctx context.Context, parentID uuid.UUID) ([]domain.Issue, error) {
	var issues []domain.Issue
	err := r.db.SelectContext(ctx, &issues, `SELECT * FROM issues WHERE parent_id = $1 AND `+teamVisible(ctx, "issues.team_id")+` ORDER BY sort_order, created_at`, parentID)
	return issues, err
}

func (r *IssueRepository) CountSubIssues(ctx context.Context, parentID uuid.UUID) (int, int, error) {
	var total, done int
	query := fmt.Sprintf(`SELECT COUNT(*), COUNT(*) FILTER (WHERE %s IN ('completed', 'cancelled')) FROM issues i LEFT JOIN team_statuses ts ON ts.id = i.status_id WHERE i.parent_id = $1`, issueStatusCategoryExpr("i", "ts")) + " AND " + teamVisible(ctx, "i.team_id")
	err := r.db.QueryRowContext(ctx, query, parentID).Scan(&total, &done)
	return total, done, err
}

func (r *IssueRepository) CountSubIssuesForIssues(ctx context.Context, issueIDs []uuid.UUID) (map[uuid.UUID]domain.SubIssueCount, error) {
	result := make(map[uuid.UUID]domain.SubIssueCount, len(issueIDs))
	if len(issueIDs) == 0 {
		return result, nil
	}

	query, args, err := sqlx.In(fmt.Sprintf(`
		SELECT i.parent_id AS issue_id,
			COUNT(*) AS total,
			COUNT(*) FILTER (WHERE %s IN ('completed', 'cancelled')) AS done
		FROM issues i
		LEFT JOIN team_statuses ts ON ts.id = i.status_id
		WHERE i.parent_id IN (?) AND %s
		GROUP BY i.parent_id`, issueStatusCategoryExpr("i", "ts"), teamVisible(ctx, "i.team_id")), issueIDs)
	if err != nil {
		return nil, err
	}
	query = r.db.Rebind(query)

	var rows []domain.SubIssueCount
	if err := r.db.SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.IssueID] = row
	}
	return result, nil
}

func (r *IssueRepository) WouldCreateCycle(ctx context.Context, issueID, parentID uuid.UUID) (bool, error) {
	if issueID == parentID {
		return true, nil
	}
	var exists bool
	err := r.db.GetContext(ctx, &exists, `
		WITH RECURSIVE descendants AS (
			SELECT id
			FROM issues
			WHERE parent_id = $1

			UNION ALL

			SELECT i.id
			FROM issues i
			INNER JOIN descendants d ON i.parent_id = d.id
		)
		SELECT EXISTS (SELECT 1 FROM descendants WHERE id = $2)`, issueID, parentID)
	return exists, err
}

func (r *IssueRepository) CycleIsActive(ctx context.Context, cycleID uuid.UUID) (bool, error) {
	var active bool
	err := r.db.GetContext(ctx, &active, `SELECT status = 'active' FROM cycles WHERE id = $1`, cycleID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return active, err
}

func (r *IssueRepository) BulkUpdate(ctx context.Context, workspaceID uuid.UUID, issueIDs []uuid.UUID, status *string, priority *int, assigneeID *uuid.UUID, statusID *uuid.UUID, cycleID *uuid.UUID, cycleSet bool) (int, error) {
	setClauses := []string{"updated_at = NOW()"}
	args := []interface{}{workspaceID}
	argIdx := 2

	if status != nil {
		setClauses = append(setClauses, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, *status)
		argIdx++
	}
	if priority != nil {
		setClauses = append(setClauses, fmt.Sprintf("priority = $%d", argIdx))
		args = append(args, *priority)
		argIdx++
	}
	// References must belong to the updated issue's workspace.
	referenceChecks := []string{}
	if assigneeID != nil {
		setClauses = append(setClauses, fmt.Sprintf("assignee_id = $%d", argIdx))
		referenceChecks = append(referenceChecks, optionalWorkspaceMember("issues.workspace_id", fmt.Sprintf("CAST($%d AS uuid)", argIdx)))
		args = append(args, *assigneeID)
		argIdx++
	}
	if statusID != nil {
		setClauses = append(setClauses, fmt.Sprintf("status_id = $%d", argIdx))
		referenceChecks = append(referenceChecks, fmt.Sprintf("EXISTS (SELECT 1 FROM team_statuses bulk_status JOIN teams bulk_status_team ON bulk_status_team.id = bulk_status.team_id WHERE bulk_status.id = $%d AND bulk_status_team.workspace_id = issues.workspace_id)", argIdx))
		args = append(args, *statusID)
		argIdx++
	}
	if cycleSet {
		setClauses = append(setClauses, fmt.Sprintf("cycle_id = $%d", argIdx))
		var cycleArg interface{}
		if cycleID != nil {
			cycleArg = *cycleID
			referenceChecks = append(referenceChecks, fmt.Sprintf("EXISTS (SELECT 1 FROM cycles bulk_cycle JOIN teams bulk_cycle_team ON bulk_cycle_team.id = bulk_cycle.team_id WHERE bulk_cycle.id = $%d AND bulk_cycle_team.workspace_id = issues.workspace_id)", argIdx))
		}
		args = append(args, cycleArg)
		argIdx++
	}

	if len(setClauses) == 1 {
		return 0, nil
	}

	// Build IN clause for issue IDs
	idPlaceholders := make([]string, len(issueIDs))
	for i, id := range issueIDs {
		idPlaceholders[i] = fmt.Sprintf("$%d", argIdx)
		args = append(args, id)
		argIdx++
	}

	query := fmt.Sprintf(
		`UPDATE issues SET %s WHERE workspace_id = $1 AND id IN (%s) AND `+teamVisible(ctx, "issues.team_id"),
		strings.Join(setClauses, ", "),
		strings.Join(idPlaceholders, ", "),
	)
	for _, check := range referenceChecks {
		query += " AND " + check
	}

	result, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}

func (r *IssueRepository) BulkDelete(ctx context.Context, workspaceID uuid.UUID, issueIDs []uuid.UUID) (int, error) {
	if len(issueIDs) == 0 {
		return 0, nil
	}
	args := []interface{}{workspaceID}
	placeholders := make([]string, len(issueIDs))
	for i, id := range issueIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+2)
		args = append(args, id)
	}
	query := fmt.Sprintf(`DELETE FROM issues WHERE workspace_id = $1 AND id IN (%s) AND `+teamVisible(ctx, "issues.team_id"), strings.Join(placeholders, ", "))
	result, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}

func (r *IssueRepository) SetAssignees(ctx context.Context, issueID uuid.UUID, userIDs []uuid.UUID) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := requireVisible(ctx, tx, issueVisible(ctx, uuidSQL(issueID))); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM issue_assignees WHERE issue_id = $1`, issueID); err != nil {
		return err
	}

	// Users who are not members of the issue's workspace are ignored.
	assigned := make([]uuid.UUID, 0, len(userIDs))
	for _, uid := range userIDs {
		result, err := tx.ExecContext(ctx, `INSERT INTO issue_assignees (issue_id, user_id) SELECT $1, $2 WHERE EXISTS (
			SELECT 1 FROM issues assignee_issue JOIN workspace_members am ON am.workspace_id = assignee_issue.workspace_id
			WHERE assignee_issue.id = $1 AND am.user_id = $2)`, issueID, uid)
		if err != nil {
			return err
		}
		if n, _ := result.RowsAffected(); n > 0 {
			assigned = append(assigned, uid)
		}
	}

	// Keep assignee_id in sync: set to first assignee or NULL
	if len(assigned) > 0 {
		_, err = tx.ExecContext(ctx, `UPDATE issues SET assignee_id = $1, updated_at = NOW() WHERE id = $2 AND `+teamVisible(ctx, "issues.team_id"), assigned[0], issueID)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE issues SET assignee_id = NULL, updated_at = NOW() WHERE id = $1 AND `+teamVisible(ctx, "issues.team_id"), issueID)
	}
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (r *IssueRepository) GetAssignees(ctx context.Context, issueID uuid.UUID) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := r.db.SelectContext(ctx, &ids, `SELECT user_id FROM issue_assignees WHERE issue_id = $1 AND `+issueVisible(ctx, "issue_assignees.issue_id")+` ORDER BY created_at`, issueID)
	return ids, err
}

func (r *IssueRepository) GetAssigneesForIssues(ctx context.Context, issueIDs []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
	if len(issueIDs) == 0 {
		return make(map[uuid.UUID][]uuid.UUID), nil
	}

	type row struct {
		IssueID uuid.UUID `db:"issue_id"`
		UserID  uuid.UUID `db:"user_id"`
	}
	var rows []row

	query, args, err := sqlx.In(`SELECT issue_id, user_id FROM issue_assignees WHERE issue_id IN (?) AND `+issueVisible(ctx, "issue_assignees.issue_id")+` ORDER BY created_at`, issueIDs)
	if err != nil {
		return nil, err
	}
	query = r.db.Rebind(query)

	if err := r.db.SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, err
	}

	result := make(map[uuid.UUID][]uuid.UUID, len(issueIDs))
	for _, r := range rows {
		result[r.IssueID] = append(result[r.IssueID], r.UserID)
	}
	return result, nil
}

func (r *IssueRepository) Subscribe(ctx context.Context, issueID, userID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO issue_subscribers (issue_id, user_id) SELECT $1, $2 WHERE `+issueVisible(ctx, "$1")+` ON CONFLICT DO NOTHING`, issueID, userID)
	return err
}

func (r *IssueRepository) Unsubscribe(ctx context.Context, issueID, userID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM issue_subscribers WHERE issue_id = $1 AND user_id = $2`, issueID, userID)
	return err
}

func (r *IssueRepository) IsSubscribed(ctx context.Context, issueID, userID uuid.UUID) (bool, error) {
	var exists bool
	err := r.db.GetContext(ctx, &exists, `SELECT EXISTS (SELECT 1 FROM issue_subscribers WHERE issue_id = $1 AND user_id = $2 AND `+issueVisible(ctx, "issue_id")+`)`, issueID, userID)
	return exists, err
}

func (r *IssueRepository) GetSubscribers(ctx context.Context, issueID uuid.UUID) ([]uuid.UUID, error) {
	var userIDs []uuid.UUID
	err := r.db.SelectContext(ctx, &userIDs, `SELECT user_id FROM issue_subscribers WHERE issue_id = $1 AND `+issueVisible(ctx, "issue_id"), issueID)
	return userIDs, err
}

func (r *IssueRepository) GetSubscribedIssueIDs(ctx context.Context, issueIDs []uuid.UUID, userID uuid.UUID) (map[uuid.UUID]bool, error) {
	result := make(map[uuid.UUID]bool)
	if len(issueIDs) == 0 {
		return result, nil
	}
	query, args, err := sqlx.In(`SELECT issue_id FROM issue_subscribers WHERE user_id = ? AND issue_id IN (?) AND `+issueVisible(ctx, "issue_id"), userID, issueIDs)
	if err != nil {
		return nil, err
	}
	query = r.db.Rebind(query)
	var subscribed []uuid.UUID
	if err := r.db.SelectContext(ctx, &subscribed, query, args...); err != nil {
		return nil, err
	}
	for _, id := range subscribed {
		result[id] = true
	}
	return result, nil
}

func (r *IssueRepository) BeginTx(ctx context.Context) (*sqlx.Tx, error) {
	return r.db.BeginTxx(ctx, nil)
}

// ErrLabelProjectMismatch is returned when a label is scoped to a project other
// than the one the issue belongs to.
var ErrLabelProjectMismatch = errors.New("label belongs to a different project")

// ValidateLabelScope rejects project-scoped labels that do not belong to
// projectID (the project of the issue the labels are about to be attached to).
func (r *IssueRepository) ValidateLabelScope(ctx context.Context, projectID *uuid.UUID, labelIDs []uuid.UUID) error {
	if len(labelIDs) == 0 {
		return nil
	}
	var mismatched bool
	err := r.db.GetContext(ctx, &mismatched, `SELECT EXISTS(SELECT 1 FROM labels WHERE id = ANY($1::uuid[]) AND deleted_at IS NULL
		AND project_id IS NOT NULL AND project_id IS DISTINCT FROM $2::uuid)`, pq.Array(labelIDs), projectID)
	if err != nil {
		return err
	}
	if mismatched {
		return ErrLabelProjectMismatch
	}
	return nil
}

// ErrBulkIssueNotFound is returned when a bulk write names an issue that is
// not in the workspace or not visible to the caller.
var ErrBulkIssueNotFound = errors.New("issue not found")

// ErrBulkProjectUnavailable is returned when the project is outside the
// workspace, hidden from the caller, or crosses a private-team boundary for
// one of the issues.
var ErrBulkProjectUnavailable = errors.New("project is unavailable or crosses a private-team boundary")

// BulkSetProject moves issues to projectID (nil removes the project) in one
// transaction and returns the issues that changed. It locks every issue row
// first, so the checks and the old values recorded in history are current, and
// writes only project_id: concurrent edits to other fields are never reverted.
// Labels scoped to a project an issue leaves are dropped, and project and
// label history is recorded for actorID, in the same transaction. If any issue
// is missing, hidden, or cannot take the project, nothing is written.
func (r *IssueRepository) BulkSetProject(ctx context.Context, workspaceID uuid.UUID, issueIDs []uuid.UUID, projectID *uuid.UUID, actorID uuid.UUID) ([]domain.Issue, error) {
	if len(issueIDs) == 0 {
		return nil, nil
	}
	// The privacy graph barrier comes before the issue row locks, as in SetLabels.
	tx, err := beginConversationTransaction(ctx, r.db)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	allowed := "TRUE"
	if projectID != nil {
		allowed = "EXISTS (SELECT 1 FROM projects p WHERE p.id = " + uuidSQL(*projectID) + " AND p.workspace_id = i.workspace_id AND " +
			projectVisible(ctx, "p.workspace_id", "p.team_id") + " AND " + compatibleTeams("i.team_id", "p.team_id") + ")"
	}
	type lockedIssue struct {
		ID        uuid.UUID  `db:"id"`
		ProjectID *uuid.UUID `db:"project_id"`
		Allowed   bool       `db:"allowed"`
	}
	var locked []lockedIssue
	// Rows are locked in ID order so concurrent bulk moves cannot deadlock.
	if err := tx.SelectContext(ctx, &locked, `SELECT i.id, i.project_id, `+allowed+` AS allowed FROM issues i
		WHERE i.id = ANY($1) AND i.workspace_id = $2 AND `+teamVisible(ctx, "i.team_id")+`
		ORDER BY i.id FOR UPDATE OF i`, pq.Array(issueIDs), workspaceID); err != nil {
		return nil, err
	}
	if len(locked) != len(issueIDs) {
		return nil, ErrBulkIssueNotFound
	}
	moveIDs := make([]uuid.UUID, 0, len(locked))
	oldValues := make([]string, 0, len(locked))
	for _, issue := range locked {
		if !issue.Allowed {
			return nil, ErrBulkProjectUnavailable
		}
		if issue.ProjectID == nil && projectID == nil || issue.ProjectID != nil && projectID != nil && *issue.ProjectID == *projectID {
			continue
		}
		old := ""
		if issue.ProjectID != nil {
			old = issue.ProjectID.String()
		}
		moveIDs = append(moveIDs, issue.ID)
		oldValues = append(oldValues, old)
	}
	if len(moveIDs) == 0 {
		return nil, nil
	}
	newValue := ""
	if projectID != nil {
		newValue = projectID.String()
	}

	// All parts of one statement read the same snapshot, so the label history
	// sees the labels from before the DELETE next to it. Labels history is
	// recorded only when a label the caller can see is dropped, with the names
	// the issue page shows (see the service's labelSnapshot).
	leaves := `l.project_id IS NOT NULL AND l.project_id IS DISTINCT FROM $2::uuid`
	names := func(extra string) string {
		return `(SELECT COALESCE(string_agg(l.name, ', ' ORDER BY l.name), '') FROM issue_labels il JOIN labels l ON l.id = il.label_id
			WHERE il.issue_id = changed.issue_id AND ` + labelVisible(ctx, "l") + extra + `)`
	}
	var moved []domain.Issue
	err = tx.SelectContext(ctx, &moved, `WITH moved AS (
			UPDATE issues SET project_id = $2, updated_at = NOW() WHERE id = ANY($1) RETURNING *
		), dropped AS (
			DELETE FROM issue_labels il USING labels l WHERE il.issue_id = ANY($1) AND l.id = il.label_id AND `+leaves+`
		), project_history AS (
			INSERT INTO issue_history (issue_id, user_id, field, old_value, new_value, agent)
			SELECT moved_issue.id, $3, 'project', moved_issue.old_value, $5, $6
			FROM unnest($1::uuid[], $4::text[]) AS moved_issue(id, old_value)
		), label_history AS (
			INSERT INTO issue_history (issue_id, user_id, field, old_value, new_value, agent)
			SELECT changed.issue_id, $3, 'labels', `+names("")+`, `+names(" AND NOT ("+leaves+")")+`, $6
			FROM (SELECT DISTINCT il.issue_id FROM issue_labels il JOIN labels l ON l.id = il.label_id
				WHERE il.issue_id = ANY($1) AND `+leaves+` AND `+labelVisible(ctx, "l")+`) changed
		)
		SELECT * FROM moved ORDER BY id`,
		pq.Array(moveIDs), projectID, actorID, pq.Array(oldValues), newValue, domain.ActingAgent(ctx))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return moved, nil
}

// GetSubscribersForIssues returns each visible issue's subscribers.
func (r *IssueRepository) GetSubscribersForIssues(ctx context.Context, issueIDs []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
	result := make(map[uuid.UUID][]uuid.UUID, len(issueIDs))
	if len(issueIDs) == 0 {
		return result, nil
	}
	var rows []struct {
		IssueID uuid.UUID `db:"issue_id"`
		UserID  uuid.UUID `db:"user_id"`
	}
	if err := r.db.SelectContext(ctx, &rows, `SELECT issue_id, user_id FROM issue_subscribers WHERE issue_id = ANY($1) AND `+issueVisible(ctx, "issue_id"), pq.Array(issueIDs)); err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.IssueID] = append(result[row.IssueID], row.UserID)
	}
	return result, nil
}

// statusCategoryRankSQL turns a comma separated category list into a CASE
// expression over categoryExpr (the issue's status category, legacy included). Only known categories are used, each at most
// once, so the result never contains caller supplied text. Categories that are
// not listed, and issues without a status row, sort after the listed ones.
func statusCategoryRankSQL(order, categoryExpr string) string {
	known := map[string]bool{
		string(domain.StatusCategoryBacklog):   true,
		string(domain.StatusCategoryUnstarted): true,
		string(domain.StatusCategoryStarted):   true,
		string(domain.StatusCategoryCompleted): true,
		string(domain.StatusCategoryCancelled): true,
	}
	var sb strings.Builder
	seen := map[string]bool{}
	rank := 0
	for _, part := range strings.Split(order, ",") {
		category := strings.TrimSpace(part)
		if !known[category] || seen[category] {
			continue
		}
		seen[category] = true
		fmt.Fprintf(&sb, " WHEN '%s' THEN %d", category, rank)
		rank++
	}
	if rank == 0 {
		return ""
	}
	return fmt.Sprintf("(CASE %s%s ELSE %d END)", categoryExpr, sb.String(), rank)
}
