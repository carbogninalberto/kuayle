package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// conversationProjectSQL is a conversation's effective project: a free chat's
// own tag, or an issue chat's current issue project. alias is a developer-owned
// SQL identifier.
func conversationProjectSQL(alias string) string {
	return `(CASE WHEN ` + alias + `.issue_id IS NULL THEN ` + alias + `.project_id ELSE
 (SELECT project_issue.project_id FROM issues project_issue WHERE project_issue.id=` + alias + `.issue_id AND project_issue.workspace_id=` + alias + `.workspace_id) END)`
}

// conversationProjectVisible holds when the effective project exists in the
// conversation's workspace and the request actor can see it. A project the
// actor can no longer see is treated as no project, never disclosed.
func conversationProjectVisible(ctx context.Context, alias string) string {
	return `EXISTS(SELECT 1 FROM projects tag_project WHERE tag_project.id=` + conversationProjectSQL(alias) + `
 AND tag_project.workspace_id=` + alias + `.workspace_id AND ` + projectVisible(ctx, "tag_project.workspace_id", "tag_project.team_id") + `)`
}

func conversationFilterSQL(ctx context.Context, filter domain.ConversationFilter, alias string, arg string) string {
	switch {
	case filter.ProjectID != nil:
		return ` AND ` + conversationProjectSQL(alias) + `=` + arg + ` AND ` + conversationProjectVisible(ctx, alias)
	case filter.NoProject:
		return ` AND NOT ` + conversationProjectVisible(ctx, alias)
	}
	return ``
}

// fillConversationProjects replaces each conversation's stored project with its
// effective, visible project (or none) for the request actor.
func fillConversationProjects(ctx context.Context, q sqlx.QueryerContext, list []domain.Conversation) error {
	if len(list) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(list))
	for i := range list {
		ids[i] = list[i].ID
		list[i].ProjectID, list[i].Project = nil, nil
	}
	var rows []struct {
		ConversationID uuid.UUID `db:"conversation_id"`
		domain.ConversationProject
	}
	err := sqlx.SelectContext(ctx, q, &rows, `SELECT c.id AS conversation_id,p.id,p.name,p.team_id FROM conversations c
 JOIN projects p ON p.id=`+conversationProjectSQL("c")+` AND p.workspace_id=c.workspace_id
 WHERE c.id=ANY($1) AND `+projectVisible(ctx, "p.workspace_id", "p.team_id"), ids)
	if err != nil {
		return err
	}
	byConversation := make(map[uuid.UUID]domain.ConversationProject, len(rows))
	for _, row := range rows {
		byConversation[row.ConversationID] = row.ConversationProject
	}
	for i := range list {
		if project, ok := byConversation[list[i].ID]; ok {
			list[i].ProjectID, list[i].Project = &project.ID, &project
		}
	}
	return nil
}

func fillConversationProject(ctx context.Context, q sqlx.QueryerContext, c *domain.Conversation) error {
	list := []domain.Conversation{*c}
	if err := fillConversationProjects(ctx, q, list); err != nil {
		return err
	}
	*c = list[0]
	return nil
}

// visibleProject reports whether project belongs to the workspace and the
// request actor can see it.
func visibleProject(ctx context.Context, tx *sqlx.Tx, ws, project uuid.UUID) error {
	var ok bool
	if err := tx.GetContext(ctx, &ok, `SELECT EXISTS(SELECT 1 FROM projects WHERE id=$1 AND workspace_id=$2 AND `+projectVisible(ctx, "projects.workspace_id", "projects.team_id")+`)`, project, ws); err != nil {
		return err
	}
	if !ok {
		return ErrConversationNotFound
	}
	return nil
}

// SetConversationProject tags the actor's free chat with one of the visible
// projects of its workspace, or clears the tag when project is nil. An issue
// chat always follows its issue's project.
func (r *ConversationRepository) SetConversationProject(ctx context.Context, ws, id uuid.UUID, project *uuid.UUID) (*domain.Conversation, error) {
	tx, c, err := r.authorized(ctx, ws, id)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if c.IssueID != nil {
		return nil, ErrConversationConflict
	}
	if project != nil {
		if err = visibleProject(ctx, tx, ws, *project); err != nil {
			return nil, err
		}
	}
	if err = tx.GetContext(ctx, c, `UPDATE conversations SET project_id=$2 WHERE id=$1 RETURNING *`, id, project); err != nil {
		return nil, err
	}
	if err = fillConversationProject(ctx, tx, c); err != nil {
		return nil, err
	}
	return c, tx.Commit()
}
