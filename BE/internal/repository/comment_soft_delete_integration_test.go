package repository

import (
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func (f *undoFixture) createComment(t *testing.T, issue uuid.UUID, author uuid.UUID, parent *uuid.UUID) *domain.Comment {
	t.Helper()
	comment := &domain.Comment{ID: uuid.New(), IssueID: issue, UserID: author, Body: "<p>Hello</p>", ParentID: parent}
	require.NoError(t, NewCommentRepository(f.db).Create(f.ctx, comment))
	return comment
}

func TestCommentSoftDeleteHidesTheThreadUntilRestored(t *testing.T) {
	f := newUndoFixture(t)
	repo := NewCommentRepository(f.db)
	issue := f.createIssue(t, f.team, f.todo)
	parent := f.createComment(t, issue.ID, f.outsider, nil)
	reply := f.createComment(t, issue.ID, f.owner, &parent.ID)
	other := f.createComment(t, issue.ID, f.owner, nil)

	deleted, err := repo.SoftDelete(f.ctx, issue.ID, parent.ID, f.owner)
	require.NoError(t, err)
	require.NotNil(t, deleted)
	require.Equal(t, "<p>Hello</p>", deleted.Body, "the deleted comment is returned with its body")
	again, err := repo.SoftDelete(f.ctx, issue.ID, parent.ID, f.owner)
	require.NoError(t, err)
	require.Nil(t, again, "deleting twice changes nothing")

	listed, err := repo.ListByIssue(f.ctx, issue.ID)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, other.ID, listed[0].ID)
	replies, err := repo.ListReplies(f.ctx, parent.ID)
	require.NoError(t, err)
	require.Empty(t, replies)
	_, err = repo.GetByID(f.ctx, parent.ID)
	require.ErrorIs(t, err, sql.ErrNoRows)
	_, err = repo.GetByID(f.ctx, reply.ID)
	require.ErrorIs(t, err, sql.ErrNoRows, "a reply in a deleted thread is hidden")
	resolved, err := repo.Resolve(f.ctx, reply.ID)
	require.NoError(t, err)
	require.Nil(t, resolved, "a hidden reply cannot be resolved")
	edited, err := repo.Update(f.ctx, issue.ID, reply.ID, f.owner, "<p>Changed</p>")
	require.NoError(t, err)
	require.Nil(t, edited, "a hidden reply cannot be edited")
	require.Error(t, repo.Create(f.ctx, &domain.Comment{ID: uuid.New(), IssueID: issue.ID, UserID: f.owner, Body: "x", ParentID: &parent.ID}),
		"nobody can reply to a deleted comment")

	restored, err := repo.Restore(f.ctx, issue.ID, parent.ID, time.Now().Add(-time.Hour))
	require.NoError(t, err)
	require.Equal(t, parent.ID, restored.ID)
	require.Nil(t, restored.DeletedAt)
	replies, err = repo.ListReplies(f.ctx, parent.ID)
	require.NoError(t, err)
	require.Len(t, replies, 1, "restoring brings the replies back")
	unchanged, err := repo.Restore(f.ctx, issue.ID, parent.ID, time.Now().Add(-time.Hour))
	require.NoError(t, err)
	require.Equal(t, parent.ID, unchanged.ID, "restoring a live comment returns it unchanged")
}

func TestCommentSoftDeleteChecksIssueAndVisibility(t *testing.T) {
	f := newUndoFixture(t)
	repo := NewCommentRepository(f.db)
	issue := f.createIssue(t, f.team, f.todo)
	elsewhere := f.createIssue(t, f.team, f.todo)
	private := f.createIssue(t, f.privateTeam, f.privateTodo)
	comment := f.createComment(t, issue.ID, f.owner, nil)
	secret := f.createComment(t, private.ID, f.owner, nil)

	deleted, err := repo.SoftDelete(f.ctx, elsewhere.ID, comment.ID, f.owner)
	require.NoError(t, err)
	require.Nil(t, deleted, "a comment is only deleted through its own issue")
	deleted, err = repo.SoftDelete(f.outsiderCtx, private.ID, secret.ID, f.outsider)
	require.NoError(t, err)
	require.Nil(t, deleted, "a private team's comment is hidden from outsiders")

	deleted, err = repo.SoftDelete(f.ctx, private.ID, secret.ID, f.owner)
	require.NoError(t, err)
	require.NotNil(t, deleted, "soft-deleting passes the private boundary triggers")
	_, err = repo.Restore(f.outsiderCtx, private.ID, secret.ID, time.Now().Add(-time.Hour))
	require.ErrorIs(t, err, sql.ErrNoRows, "outsiders cannot restore a private comment")
	_, err = repo.Restore(f.ctx, elsewhere.ID, secret.ID, time.Now().Add(-time.Hour))
	require.ErrorIs(t, err, sql.ErrNoRows, "a comment is only restored through its own issue")
}

func TestCommentUpdateIsAuthorOnlyAndMarksEdited(t *testing.T) {
	f := newUndoFixture(t)
	repo := NewCommentRepository(f.db)
	issue := f.createIssue(t, f.team, f.todo)
	comment := f.createComment(t, issue.ID, f.outsider, nil)

	edited, err := repo.Update(f.ctx, issue.ID, comment.ID, f.owner, "<p>Not mine</p>")
	require.NoError(t, err)
	require.Nil(t, edited, "only the author's ID matches")
	edited, err = repo.Update(f.outsiderCtx, issue.ID, comment.ID, f.outsider, "<p>Hello</p>")
	require.NoError(t, err)
	require.Nil(t, edited, "the same body changes nothing")

	edited, err = repo.Update(f.outsiderCtx, issue.ID, comment.ID, f.outsider, "<p>Better</p>")
	require.NoError(t, err)
	require.NotNil(t, edited)
	require.Equal(t, "<p>Better</p>", edited.Body)
	require.NotNil(t, edited.EditedAt)
}

func TestCommentRestoreAndPurgeHonourTheGracePeriod(t *testing.T) {
	f := newUndoFixture(t)
	repo := NewCommentRepository(f.db)
	issue := f.createIssue(t, f.team, f.todo)
	old := f.createComment(t, issue.ID, f.owner, nil)
	oldReply := f.createComment(t, issue.ID, f.owner, &old.ID)
	recent := f.createComment(t, issue.ID, f.owner, nil)
	for _, id := range []uuid.UUID{old.ID, recent.ID} {
		_, err := repo.SoftDelete(f.ctx, issue.ID, id, f.owner)
		require.NoError(t, err)
	}
	_, err := f.db.Exec(`UPDATE comments SET deleted_at = NOW() - INTERVAL '40 days' WHERE id = $1`, old.ID)
	require.NoError(t, err)
	cutoff := time.Now().Add(-30 * 24 * time.Hour)

	_, err = repo.Restore(f.ctx, issue.ID, old.ID, cutoff)
	require.ErrorIs(t, err, ErrRestoreExpired)

	purged, err := repo.PurgeDeletedComments(f.ctx, cutoff, 1)
	require.NoError(t, err)
	require.GreaterOrEqual(t, purged, 1)
	var count int
	require.NoError(t, f.db.Get(&count, `SELECT COUNT(*) FROM comments WHERE id IN ($1, $2)`, old.ID, oldReply.ID))
	require.Zero(t, count, "the expired comment and its replies are purged")
	require.NoError(t, f.db.Get(&count, `SELECT COUNT(*) FROM comments WHERE id = $1`, recent.ID))
	require.Equal(t, 1, count, "a comment inside the grace period stays")
	restored, err := repo.Restore(f.ctx, issue.ID, recent.ID, cutoff)
	require.NoError(t, err)
	require.Equal(t, recent.ID, restored.ID)
}
