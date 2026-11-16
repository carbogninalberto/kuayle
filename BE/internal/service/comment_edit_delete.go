package service

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/realtime"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/sanitize"
)

var (
	// ErrCommentNotFound: no live comment with that ID is visible on the issue.
	ErrCommentNotFound = errors.New("comment not found")
	// ErrCommentForbidden: only a comment's author may edit it.
	ErrCommentForbidden = errors.New("only the comment's author can edit it")
	// ErrCommentEmpty: the edited body has no content left after sanitizing.
	ErrCommentEmpty = errors.New("comment body is empty")
	// ErrCommentRestoreExpired: the deleted comment is past its restore period.
	ErrCommentRestoreExpired = repository.ErrRestoreExpired
)

// SetDeleteGracePeriod sets how long deleted comments stay restorable. It
// is the issue grace period, so comments and issues purge alike.
func (s *CommentService) SetDeleteGracePeriod(grace time.Duration) { s.deleteGrace = grace }

// DeleteGracePeriod is how long deleted comments stay restorable.
func (s *CommentService) DeleteGracePeriod() time.Duration {
	if s.deleteGrace <= 0 {
		return DefaultIssueDeleteGracePeriod
	}
	return s.deleteGrace
}

// Update replaces the body of actorID's own comment on issue and returns it
// as stored. A changed body marks the comment edited and sends
// comment.updated; the same body changes nothing and sends nothing. Mentions
// added by the edit notify nobody.
func (s *CommentService) Update(ctx context.Context, issue *domain.Issue, id, actorID uuid.UUID, body string) (*domain.Comment, error) {
	body = sanitize.SanitizeEditorContent(body)
	if strings.TrimSpace(body) == "" {
		return nil, ErrCommentEmpty
	}
	current, err := s.commentOn(ctx, issue, id)
	if err != nil {
		return nil, err
	}
	if current.UserID != actorID {
		return nil, ErrCommentForbidden
	}
	changed, err := s.commentRepo.Update(ctx, issue.ID, id, actorID, body)
	if err != nil {
		return nil, err
	}
	if changed == nil {
		// Same body, or deleted meanwhile: report what is stored now.
		return s.commentOn(ctx, issue, id)
	}
	s.broadcastComment(issue, "comment.updated", changed.ID)
	if s.webhooks != nil {
		s.webhooks.CommentEvent(ctx, issue.WorkspaceID, WebhookEventCommentUpdated, issue, changed, actorID)
	}
	return changed, nil
}

// Delete soft-deletes a comment on issue for actorID, hiding its replies with
// it, and sends comment.deleted with the comment as it was before deletion.
// It returns ErrCommentNotFound when there is no live comment to delete, so
// a delete that lost a race sends nothing.
func (s *CommentService) Delete(ctx context.Context, issue *domain.Issue, id, actorID uuid.UUID) error {
	deleted, err := s.commentRepo.SoftDelete(ctx, issue.ID, id, actorID)
	if err != nil {
		return err
	}
	if deleted == nil {
		return ErrCommentNotFound
	}
	deleted.DeletedAt, deleted.DeletedBy = nil, nil
	s.broadcastComment(issue, "comment.deleted", deleted.ID)
	if s.webhooks != nil {
		s.webhooks.CommentEvent(ctx, issue.WorkspaceID, WebhookEventCommentDeleted, issue, deleted, actorID)
	}
	return nil
}

// Restore brings back a comment deleted within the grace period, with its
// replies. Like issue restore it sends no webhook; open issue views refresh.
func (s *CommentService) Restore(ctx context.Context, issue *domain.Issue, id uuid.UUID) (*domain.Comment, error) {
	restored, err := s.commentRepo.Restore(ctx, issue.ID, id, time.Now().Add(-s.DeleteGracePeriod()))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCommentNotFound
	}
	if err != nil {
		return nil, err
	}
	s.broadcastComment(issue, "comment.updated", restored.ID)
	return restored, nil
}

// commentOn returns the live comment id when it belongs to issue.
func (s *CommentService) commentOn(ctx context.Context, issue *domain.Issue, id uuid.UUID) (*domain.Comment, error) {
	comment, err := s.commentRepo.GetByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && (comment == nil || comment.IssueID != issue.ID)) {
		return nil, ErrCommentNotFound
	}
	return comment, err
}

// broadcastComment tells open views of the issue that a comment changed.
func (s *CommentService) broadcastComment(issue *domain.Issue, event string, commentID uuid.UUID) {
	if s.hub == nil {
		return
	}
	s.hub.Broadcast(issue.WorkspaceID, realtime.Event{
		Type: event,
		Payload: map[string]string{
			"issue_id":   issue.ID.String(),
			"comment_id": commentID.String(),
			"identifier": issue.Identifier,
		},
	})
}
