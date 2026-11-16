package service

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/realtime"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// editableCommentRepo adds edits and soft deletes to webhookCommentRepo the
// way the database does them: deleted comments leave the live list until
// they are restored.
type editableCommentRepo struct {
	*webhookCommentRepo
	deleted map[uuid.UUID]domain.Comment
}

func newEditableCommentRepo(comments ...domain.Comment) *editableCommentRepo {
	return &editableCommentRepo{webhookCommentRepo: &webhookCommentRepo{stored: comments}, deleted: map[uuid.UUID]domain.Comment{}}
}

func (r *editableCommentRepo) Update(_ context.Context, issueID, id, authorID uuid.UUID, body string) (*domain.Comment, error) {
	if r.failure != nil {
		return nil, r.failure
	}
	for i := range r.stored {
		c := &r.stored[i]
		if c.ID != id || c.IssueID != issueID || c.UserID != authorID || c.Body == body {
			continue
		}
		now := time.Now().UTC()
		c.Body, c.EditedAt = body, &now
		changed := *c
		return &changed, nil
	}
	return nil, nil
}

func (r *editableCommentRepo) SoftDelete(_ context.Context, issueID, id, actorID uuid.UUID) (*domain.Comment, error) {
	if r.failure != nil {
		return nil, r.failure
	}
	for i, c := range r.stored {
		if c.ID != id || c.IssueID != issueID {
			continue
		}
		now := time.Now().UTC()
		c.DeletedAt, c.DeletedBy = &now, &actorID
		r.deleted[id] = c
		r.stored = append(r.stored[:i], r.stored[i+1:]...)
		return &c, nil
	}
	return nil, nil
}

func (r *editableCommentRepo) Restore(_ context.Context, issueID, id uuid.UUID, deletedSince time.Time) (*domain.Comment, error) {
	c, ok := r.deleted[id]
	if !ok || c.IssueID != issueID {
		return nil, sql.ErrNoRows
	}
	if c.DeletedAt.Before(deletedSince) {
		return nil, repository.ErrRestoreExpired
	}
	delete(r.deleted, id)
	c.DeletedAt, c.DeletedBy = nil, nil
	r.stored = append(r.stored, c)
	return &c, nil
}

func newCommentEditFixture(t *testing.T) (*issueWebhookFixture, domain.Issue, domain.Comment, *editableCommentRepo, *CommentService, *webhookRecorder) {
	t.Helper()
	f := newIssueWebhookFixture(t)
	issue := f.issue("ENG-1", nil)
	comment := domain.Comment{ID: uuid.New(), IssueID: issue.ID, UserID: f.actor, Body: "<p>Fix it</p>"}
	comments := newEditableCommentRepo(comment)
	svc := NewCommentService(comments, f.issues, realtime.NewHub(), newTestNotifSvc())
	sent := &webhookRecorder{}
	svc.SetWebhooks(sent)
	return f, issue, comment, comments, svc, sent
}

// KUA-200: an author's edit sends comment.updated with the new body; an
// edit that changes nothing, or that someone else attempts, sends nothing.
func TestCommentEditSendsCommentUpdatedOnlyWhenTheBodyChanges(t *testing.T) {
	f, issue, comment, comments, svc, sent := newCommentEditFixture(t)
	ctx := context.Background()

	edited, err := svc.Update(ctx, &issue, comment.ID, f.actor, "<p>Fixed it</p>")
	require.NoError(t, err)
	require.Equal(t, "<p>Fixed it</p>", edited.Body)
	require.NotNil(t, edited.EditedAt)
	require.Equal(t, []string{WebhookEventCommentUpdated}, sent.events())
	require.Equal(t, "<p>Fixed it</p>", sent.sent[0].comment.Body)
	require.Equal(t, f.actor, sent.sent[0].actor)
	require.Equal(t, "ENG-1", sent.sent[0].issue.Identifier)

	same, err := svc.Update(ctx, &issue, comment.ID, f.actor, "<p>Fixed it</p>")
	require.NoError(t, err)
	require.Equal(t, "<p>Fixed it</p>", same.Body)
	require.Len(t, sent.sent, 1, "the same body changes nothing")

	_, err = svc.Update(ctx, &issue, comment.ID, uuid.New(), "<p>Hijacked</p>")
	require.ErrorIs(t, err, ErrCommentForbidden, "only the author edits")
	_, err = svc.Update(ctx, &issue, comment.ID, f.actor, "   ")
	require.ErrorIs(t, err, ErrCommentEmpty)
	other := f.issue("ENG-2", nil)
	_, err = svc.Update(ctx, &other, comment.ID, f.actor, "<p>Elsewhere</p>")
	require.ErrorIs(t, err, ErrCommentNotFound, "a comment is edited through its own issue")

	comments.failure = errors.New("refused")
	_, err = svc.Update(ctx, &issue, comment.ID, f.actor, "<p>Lost</p>")
	require.Error(t, err)
	require.Len(t, sent.sent, 1, "a failed edit is not announced")
}

// KUA-196/200: a delete sends comment.deleted with the comment as it was
// before deletion; a second delete, a failed one and a restore send nothing.
func TestCommentDeleteSendsCommentDeletedOnceAndRestoreSendsNothing(t *testing.T) {
	f, issue, comment, comments, svc, sent := newCommentEditFixture(t)
	ctx := context.Background()
	admin := uuid.New()

	require.NoError(t, svc.Delete(ctx, &issue, comment.ID, admin))
	require.Equal(t, []string{WebhookEventCommentDeleted}, sent.events())
	got := sent.sent[0]
	require.Equal(t, f.ws, got.workspace)
	require.Equal(t, comment.ID, got.comment.ID)
	require.Equal(t, "<p>Fix it</p>", got.comment.Body, "the payload is the comment before deletion")
	require.Nil(t, got.comment.DeletedAt)
	require.Equal(t, admin, got.actor)

	require.ErrorIs(t, svc.Delete(ctx, &issue, comment.ID, admin), ErrCommentNotFound)
	require.Len(t, sent.sent, 1, "a delete that lost the race sends nothing")

	restored, err := svc.Restore(ctx, &issue, comment.ID)
	require.NoError(t, err)
	require.Equal(t, comment.ID, restored.ID)
	require.Len(t, sent.sent, 1, "restoring sends no webhook")

	comments.failure = errors.New("refused")
	require.Error(t, svc.Delete(ctx, &issue, comment.ID, admin))
	require.Len(t, sent.sent, 1, "a failed delete is not announced")
}

// Restoring honours the issue grace period the service was given.
func TestCommentRestoreUsesTheDeleteGracePeriod(t *testing.T) {
	_, issue, comment, comments, svc, _ := newCommentEditFixture(t)
	ctx := context.Background()
	require.Equal(t, DefaultIssueDeleteGracePeriod, svc.DeleteGracePeriod())
	svc.SetDeleteGracePeriod(time.Hour)
	require.Equal(t, time.Hour, svc.DeleteGracePeriod())

	require.NoError(t, svc.Delete(ctx, &issue, comment.ID, uuid.New()))
	deleted := comments.deleted[comment.ID]
	old := time.Now().Add(-2 * time.Hour)
	deleted.DeletedAt = &old
	comments.deleted[comment.ID] = deleted

	_, err := svc.Restore(ctx, &issue, comment.ID)
	require.ErrorIs(t, err, ErrCommentRestoreExpired)
	_, err = svc.Restore(ctx, &issue, uuid.New())
	require.ErrorIs(t, err, ErrCommentNotFound)
}
