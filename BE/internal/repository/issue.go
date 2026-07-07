package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
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
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
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

func (r *IssueRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Issue, error) {
	var issue domain.Issue
	err := r.db.GetContext(ctx, &issue, `SELECT * FROM issues WHERE id = $1`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &issue, err
}

func (r *IssueRepository) GetByIdentifier(ctx context.Context, workspaceID uuid.UUID, identifier string) (*domain.Issue, error) {
	var issue domain.Issue
	err := r.db.GetContext(ctx, &issue, `SELECT * FROM issues WHERE workspace_id = $1 AND identifier_text = $2`, workspaceID, identifier)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &issue, err
}

func (r *IssueRepository) List(ctx context.Context, workspaceID uuid.UUID, params dto.IssueFilterParams) ([]domain.Issue, int, error) {
	where := []string{"i.workspace_id = :workspace_id"}
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
		where = append(where, "EXISTS (SELECT 1 FROM issue_labels il WHERE il.issue_id = i.id AND il.label_id = :label_id)")
		args["label_id"] = params.LabelID
	}
	if params.Search != "" {
		searchFields := []string{
			"i.title ILIKE :search",
			"i.identifier_text ILIKE :search",
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

	// Sort
	sortCol := "i.created_at"
	allowedSorts := map[string]bool{"created_at": true, "updated_at": true, "priority": true, "sort_order": true, "status": true, "due_date": true}
	if params.Sort != "" && allowedSorts[params.Sort] {
		sortCol = "i." + params.Sort
	}
	order := "DESC"
	if params.Order == "asc" {
		order = "ASC"
	}

	params.Defaults()
	args["limit"] = params.PerPage
	args["offset"] = params.Offset()

	dataQuery := fmt.Sprintf(`SELECT i.* FROM issues i WHERE %s ORDER BY %s %s LIMIT :limit OFFSET :offset`, whereClause, sortCol, order)
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

func (r *IssueRepository) Update(ctx context.Context, issue *domain.Issue) error {
	query := `
		UPDATE issues SET
			title = $1, description = $2, status = $3, priority = $4,
			assignee_id = $5, project_id = $6, cycle_id = $7, parent_id = $8,
			due_date = $9, sort_order = $10, triaged = $11,
			status_id = $12, updated_at = NOW()
		WHERE id = $13
		RETURNING updated_at`
	return r.db.QueryRowContext(ctx, query,
		issue.Title, issue.Description, issue.Status, issue.Priority,
		issue.AssigneeID, issue.ProjectID, issue.CycleID, issue.ParentID,
		issue.DueDate, issue.SortOrder, issue.Triaged,
		issue.StatusID, issue.ID,
	).Scan(&issue.UpdatedAt)
}

func (r *IssueRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM issues WHERE id = $1`, id)
	return err
}

func (r *IssueRepository) SetLabels(ctx context.Context, issueID uuid.UUID, labelIDs []uuid.UUID) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM issue_labels WHERE issue_id = $1`, issueID); err != nil {
		return err
	}

	for _, labelID := range labelIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO issue_labels (issue_id, label_id) VALUES ($1, $2)`, issueID, labelID); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (r *IssueRepository) GetLabels(ctx context.Context, issueID uuid.UUID) ([]domain.Label, error) {
	var labels []domain.Label
	query := `SELECT l.* FROM labels l INNER JOIN issue_labels il ON l.id = il.label_id WHERE il.issue_id = $1 ORDER BY l.name`
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
		WHERE il.issue_id IN (?) AND (l.deleted_at IS NULL)
		ORDER BY l.name`, issueIDs)
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
	err := r.db.SelectContext(ctx, &issues, `SELECT * FROM issues WHERE parent_id = $1 ORDER BY sort_order, created_at`, parentID)
	return issues, err
}

func (r *IssueRepository) CountSubIssues(ctx context.Context, parentID uuid.UUID) (int, int, error) {
	var total, done int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*), COUNT(*) FILTER (WHERE ts.category IN ('completed', 'cancelled')) FROM issues i LEFT JOIN team_statuses ts ON ts.id = i.status_id WHERE i.parent_id = $1`, parentID).Scan(&total, &done)
	return total, done, err
}

func (r *IssueRepository) CountSubIssuesForIssues(ctx context.Context, issueIDs []uuid.UUID) (map[uuid.UUID]domain.SubIssueCount, error) {
	result := make(map[uuid.UUID]domain.SubIssueCount, len(issueIDs))
	if len(issueIDs) == 0 {
		return result, nil
	}

	query, args, err := sqlx.In(`
		SELECT i.parent_id AS issue_id,
			COUNT(*) AS total,
			COUNT(*) FILTER (WHERE ts.category IN ('completed', 'cancelled')) AS done
		FROM issues i
		LEFT JOIN team_statuses ts ON ts.id = i.status_id
		WHERE i.parent_id IN (?)
		GROUP BY i.parent_id`, issueIDs)
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
	if assigneeID != nil {
		setClauses = append(setClauses, fmt.Sprintf("assignee_id = $%d", argIdx))
		args = append(args, *assigneeID)
		argIdx++
	}
	if statusID != nil {
		setClauses = append(setClauses, fmt.Sprintf("status_id = $%d", argIdx))
		args = append(args, *statusID)
		argIdx++
	}
	if cycleSet {
		setClauses = append(setClauses, fmt.Sprintf("cycle_id = $%d", argIdx))
		var cycleArg interface{}
		if cycleID != nil {
			cycleArg = *cycleID
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
		`UPDATE issues SET %s WHERE workspace_id = $1 AND id IN (%s)`,
		strings.Join(setClauses, ", "),
		strings.Join(idPlaceholders, ", "),
	)

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
	query := fmt.Sprintf(`DELETE FROM issues WHERE workspace_id = $1 AND id IN (%s)`, strings.Join(placeholders, ", "))
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

	if _, err := tx.ExecContext(ctx, `DELETE FROM issue_assignees WHERE issue_id = $1`, issueID); err != nil {
		return err
	}

	for _, uid := range userIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO issue_assignees (issue_id, user_id) VALUES ($1, $2)`, issueID, uid); err != nil {
			return err
		}
	}

	// Keep assignee_id in sync: set to first assignee or NULL
	if len(userIDs) > 0 {
		_, err = tx.ExecContext(ctx, `UPDATE issues SET assignee_id = $1, updated_at = NOW() WHERE id = $2`, userIDs[0], issueID)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE issues SET assignee_id = NULL, updated_at = NOW() WHERE id = $1`, issueID)
	}
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (r *IssueRepository) GetAssignees(ctx context.Context, issueID uuid.UUID) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := r.db.SelectContext(ctx, &ids, `SELECT user_id FROM issue_assignees WHERE issue_id = $1 ORDER BY created_at`, issueID)
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

	query, args, err := sqlx.In(`SELECT issue_id, user_id FROM issue_assignees WHERE issue_id IN (?) ORDER BY created_at`, issueIDs)
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
	_, err := r.db.ExecContext(ctx, `INSERT INTO issue_subscribers (issue_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, issueID, userID)
	return err
}

func (r *IssueRepository) Unsubscribe(ctx context.Context, issueID, userID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM issue_subscribers WHERE issue_id = $1 AND user_id = $2`, issueID, userID)
	return err
}

func (r *IssueRepository) IsSubscribed(ctx context.Context, issueID, userID uuid.UUID) (bool, error) {
	var exists bool
	err := r.db.GetContext(ctx, &exists, `SELECT EXISTS (SELECT 1 FROM issue_subscribers WHERE issue_id = $1 AND user_id = $2)`, issueID, userID)
	return exists, err
}

func (r *IssueRepository) GetSubscribers(ctx context.Context, issueID uuid.UUID) ([]uuid.UUID, error) {
	var userIDs []uuid.UUID
	err := r.db.SelectContext(ctx, &userIDs, `SELECT user_id FROM issue_subscribers WHERE issue_id = $1`, issueID)
	return userIDs, err
}

func (r *IssueRepository) GetSubscribedIssueIDs(ctx context.Context, issueIDs []uuid.UUID, userID uuid.UUID) (map[uuid.UUID]bool, error) {
	result := make(map[uuid.UUID]bool)
	if len(issueIDs) == 0 {
		return result, nil
	}
	query, args, err := sqlx.In(`SELECT issue_id FROM issue_subscribers WHERE user_id = ? AND issue_id IN (?)`, userID, issueIDs)
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
