package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// Coordinate short database transactions with the existing privacy graph
// barrier before acquiring row locks. Never hold this guard during external I/O.
func beginConversationTransaction(ctx context.Context, db *sqlx.DB) (*sqlx.Tx, error) {
	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock_shared(610046,0)`); err != nil {
		tx.Rollback()
		return nil, err
	}
	return tx, nil
}

// Lock the issue before its conversation, matching issue deletion's cascade.
// Re-read the conversation under its cursor lock; no code mutates its scope.
func lockConversation(ctx context.Context, tx *sqlx.Tx, ws, id, user uuid.UUID, skip bool) (*domain.Conversation, error) {
	var c domain.Conversation
	err := tx.GetContext(ctx, &c, `SELECT * FROM conversations WHERE id=$1 AND workspace_id=$2 AND creator_id=$3`, id, ws, user)
	if err != nil {
		return nil, err
	}
	suffix := ""
	if skip {
		suffix = " SKIP LOCKED"
	}
	if c.IssueID != nil {
		var issue uuid.UUID
		err = tx.GetContext(ctx, &issue, `SELECT id FROM issues WHERE id=$1 AND workspace_id=$2 AND team_id=$3 FOR SHARE`+suffix, c.IssueID, c.WorkspaceID, c.TeamID)
		if err != nil {
			return nil, err
		}
	}
	err = tx.GetContext(ctx, &c, `SELECT * FROM conversations WHERE id=$1 AND workspace_id=$2 AND creator_id=$3 FOR UPDATE`+suffix, id, ws, user)
	return &c, err
}

// alias is always a developer-owned SQL identifier. The actor comes from the
// persisted conversation, after the caller has established creator ownership.
func conversationScopePredicate(alias string) string {
	return `(` + alias + `.issue_id IS NULL OR EXISTS(SELECT 1 FROM issues scope_issue
 WHERE scope_issue.id=` + alias + `.issue_id AND scope_issue.workspace_id=` + alias + `.workspace_id
 AND scope_issue.team_id=` + alias + `.team_id
 AND kuayle_team_access(scope_issue.team_id,` + alias + `.creator_id,` + alias + `.workspace_id)))`
}
func conversationScope(ctx context.Context, tx *sqlx.Tx, c domain.Conversation) error {
	if c.IssueID == nil {
		return nil
	}
	var members []uuid.UUID
	if err := tx.SelectContext(ctx, &members, `SELECT user_id FROM team_members WHERE team_id=$1 AND user_id=$2 FOR SHARE`, c.TeamID, c.CreatorID); err != nil {
		return err
	}
	var allowed bool
	err := tx.GetContext(ctx, &allowed, `SELECT EXISTS(SELECT 1 FROM conversations c WHERE c.id=$1 AND `+conversationScopePredicate("c")+`)`, c.ID)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrConversationNotFound
	}
	return nil
}

// OpenIssue reuses the actor's conversation for the issue's current team. A
// separate scope after an issue move keeps earlier private messages isolated.
func (r *ConversationRepository) OpenIssue(ctx context.Context, ws, issue uuid.UUID) (*domain.Conversation, error) {
	user, err := conversationActor(ctx, ws)
	if err != nil {
		return nil, err
	}
	tx, err := beginConversationTransaction(ctx, r.db)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = conversationMember(ctx, tx, ws, user); err != nil {
		return nil, err
	}
	var source struct {
		TeamID uuid.UUID `db:"team_id"`
		Title  string    `db:"title"`
	}
	err = tx.GetContext(ctx, &source, `SELECT team_id,left(identifier_text||' · '||title,200) AS title FROM issues WHERE id=$1 AND workspace_id=$2 AND `+teamVisible(ctx, "team_id")+` FOR SHARE`, issue, ws)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrConversationNotFound
	}
	if err != nil {
		return nil, err
	}
	// No UPDATE on conflict: an existing chat's timestamps and stream stay intact.
	_, err = tx.ExecContext(ctx, `INSERT INTO conversations(workspace_id,creator_id,title,issue_id,team_id) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, ws, user, source.Title, issue, source.TeamID)
	if err != nil {
		return nil, err
	}
	var c domain.Conversation
	err = tx.GetContext(ctx, &c, `SELECT * FROM conversations WHERE workspace_id=$1 AND creator_id=$2 AND issue_id=$3 AND team_id=$4`, ws, user, issue, source.TeamID)
	if err != nil {
		return nil, err
	}
	if err = conversationScope(ctx, tx, c); err != nil {
		return nil, err
	}
	if err = fillConversationProject(ctx, tx, &c); err != nil {
		return nil, err
	}
	return &c, tx.Commit()
}
func (r *ConversationRepository) GenerationIssue(ctx context.Context, a domain.ConversationAttempt) (*domain.ConversationIssue, error) {
	tx, c, err := r.authorized(ctx, a.Conversation.WorkspaceID, a.Conversation.ID)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = fenceConversationAttempt(ctx, tx, a); err != nil {
		return nil, err
	}
	return conversationIssue(ctx, tx, *c)
}

func conversationIssue(ctx context.Context, tx *sqlx.Tx, c domain.Conversation) (*domain.ConversationIssue, error) {
	if c.IssueID == nil {
		return nil, nil
	}
	var issue domain.ConversationIssue
	err := tx.GetContext(ctx, &issue, `SELECT id,identifier_text,left(title,500) AS title,left(COALESCE(description,''),8000) AS description,status FROM issues WHERE id=$1 AND workspace_id=$2 AND team_id=$3 AND `+teamVisible(ctx, "team_id"), c.IssueID, c.WorkspaceID, c.TeamID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrConversationNotFound
	}
	return &issue, err
}
