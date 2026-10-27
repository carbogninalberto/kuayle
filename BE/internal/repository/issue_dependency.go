package repository

import (
	"context"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// A blocker relation may be stored from either side ("A blocked_by B" or
// "B blocking A"), because the inverse row is written best-effort.
const issueBlockerEdges = `(SELECT issue_id AS blocked_id, related_issue_id AS blocker_id FROM issue_relations WHERE type='blocked_by'
 UNION SELECT related_issue_id, issue_id FROM issue_relations WHERE type='blocking')`

// issueDone is true for issues in a completed or cancelled status (or the
// legacy done/cancelled status when no team status is set).
const issueDone = `COALESCE(bs.category IN ('completed','cancelled'), b.status IN ('done','cancelled'))`

// openBlockersExist matches when the issue in column has an unfinished blocker.
// Agent work on such an issue waits; it starts once every blocker is finished.
func openBlockersExist(column string) string {
	return `EXISTS(SELECT 1 FROM ` + issueBlockerEdges + ` d JOIN issues b ON b.id=d.blocker_id LEFT JOIN team_statuses bs ON bs.id=b.status_id
 WHERE d.blocked_id=` + column + ` AND b.id<>d.blocked_id AND NOT ` + issueDone + `)`
}

type IssueDependencyRepository struct{ db *sqlx.DB }

func NewIssueDependencyRepository(db *sqlx.DB) *IssueDependencyRepository {
	return &IssueDependencyRepository{db: db}
}

// Blockers returns the blockers of the given issues within the workspace.
// Callers filter them by the actor's visibility before showing them.
func (r *IssueDependencyRepository) Blockers(ctx context.Context, ws uuid.UUID, issues []uuid.UUID) ([]domain.IssueBlocker, error) {
	blockers := []domain.IssueBlocker{}
	if len(issues) == 0 {
		return blockers, nil
	}
	query, args, err := sqlx.In(`SELECT DISTINCT d.blocked_id, b.id AS blocker_id, b.identifier_text AS identifier, b.title, b.team_id, `+issueDone+` AS done
 FROM `+issueBlockerEdges+` d JOIN issues b ON b.id=d.blocker_id AND b.workspace_id=? LEFT JOIN team_statuses bs ON bs.id=b.status_id
 WHERE d.blocked_id IN (?) AND b.id<>d.blocked_id ORDER BY b.identifier_text`, ws, issues)
	if err != nil {
		return nil, err
	}
	err = r.db.SelectContext(ctx, &blockers, r.db.Rebind(query), args...)
	return blockers, err
}

// Blocks reports whether blocker already depends, directly or transitively,
// on issue, so that recording "issue blocked by blocker" would form a cycle.
func (r *IssueDependencyRepository) Blocks(ctx context.Context, issue, blocker uuid.UUID) (bool, error) {
	var cycle bool
	err := r.db.GetContext(ctx, &cycle, `WITH RECURSIVE chain(id, depth) AS (
 SELECT $2::uuid, 0
 UNION SELECT d.blocker_id, c.depth+1 FROM chain c JOIN `+issueBlockerEdges+` d ON d.blocked_id=c.id WHERE c.depth<50)
 SELECT EXISTS(SELECT 1 FROM chain WHERE id=$1)`, issue, blocker)
	return cycle, err
}

// RelatedWork lists the parent, sub-issues, siblings and blockers of a task's
// issue that share its team, with the newest agent task state, branch and pull
// request for each. Restricting to the team keeps it within what the task's
// creator could already see on the issue's team.
func (r *IssueDependencyRepository) RelatedWork(ctx context.Context, issue uuid.UUID) ([]domain.RelatedWork, error) {
	work := []domain.RelatedWork{}
	err := r.db.SelectContext(ctx, &work, `WITH me AS (SELECT id, parent_id, team_id FROM issues WHERE id=$1),
 related(id, relation) AS (
  SELECT p.id, 'parent' FROM me JOIN issues p ON p.id=me.parent_id
  UNION ALL SELECT c.id, 'sub_issue' FROM me JOIN issues c ON c.parent_id=me.id
  UNION ALL SELECT s.id, 'sibling' FROM me JOIN issues s ON me.parent_id IS NOT NULL AND s.parent_id=me.parent_id AND s.id<>me.id
  UNION ALL SELECT d.blocker_id, 'blocker' FROM me JOIN `+issueBlockerEdges+` d ON d.blocked_id=me.id WHERE d.blocker_id<>me.id)
 SELECT DISTINCT ON (i.id) i.identifier_text AS identifier, i.title, r.relation, COALESCE(st.name, i.status) AS status,
  (SELECT t.state FROM agent_tasks t WHERE t.issue_id=i.id ORDER BY t.created_at DESC LIMIT 1) AS task_state,
  (SELECT g.ref_name FROM dev_machine_git_refs g WHERE g.issue_id=i.id AND g.ref_type='branch' ORDER BY g.created_at DESC LIMIT 1) AS branch,
  (SELECT g.url FROM dev_machine_git_refs g WHERE g.issue_id=i.id AND g.ref_type='pull_request' ORDER BY g.created_at DESC LIMIT 1) AS pull_request_url
 FROM related r JOIN issues i ON i.id=r.id JOIN me ON i.team_id=me.team_id LEFT JOIN team_statuses st ON st.id=i.status_id
 ORDER BY i.id, CASE r.relation WHEN 'parent' THEN 0 WHEN 'blocker' THEN 1 WHEN 'sibling' THEN 2 ELSE 3 END LIMIT 20`, issue)
	return work, err
}

// waitingReason names a task's unfinished same-team blockers; blockers in
// other teams are counted without being named.
func waitingReason(ctx context.Context, q sqlx.QueryerContext, issue uuid.UUID) (string, error) {
	var rows []struct {
		Identifier string `db:"identifier"`
		SameTeam   bool   `db:"same_team"`
	}
	if err := sqlx.SelectContext(ctx, q, &rows, `SELECT b.identifier_text AS identifier, b.team_id=me.team_id AS same_team
 FROM issues me JOIN `+issueBlockerEdges+` d ON d.blocked_id=me.id JOIN issues b ON b.id=d.blocker_id AND b.id<>me.id LEFT JOIN team_statuses bs ON bs.id=b.status_id
 WHERE me.id=$1 AND NOT `+issueDone+` ORDER BY b.identifier_text`, issue); err != nil || len(rows) == 0 {
		return "", err
	}
	named, hidden := []string{}, 0
	for _, row := range rows {
		if row.SameTeam {
			named = append(named, row.Identifier)
		} else {
			hidden++
		}
	}
	if hidden == 1 {
		named = append(named, "1 other issue")
	} else if hidden > 1 {
		named = append(named, strconv.Itoa(hidden)+" other issues")
	}
	return "Waiting for blocking issues to finish: " + strings.Join(named, ", "), nil
}
