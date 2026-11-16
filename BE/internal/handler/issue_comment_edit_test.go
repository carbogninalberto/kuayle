package handler

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	mw "github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/realtime"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

type commentEditFixture struct {
	h        *IssueHandler
	comments *testCommentRepo
	ws       uuid.UUID
	author   uuid.UUID
	issue    *domain.Issue
	other    *domain.Issue
	comment  domain.Comment
	service  *service.CommentService
}

func newCommentEditFixture(t *testing.T) *commentEditFixture {
	t.Helper()
	issueRepo := newTestIssueRepo()
	hub := realtime.NewHub()
	notifications := service.NewNotificationService(&testNotifRepo{})
	issues := service.NewIssueService(issueRepo, newTestTeamRepo(), &testTeamStatusRepo{}, &testHistoryRepo{}, hub, notifications)
	f := &commentEditFixture{comments: &testCommentRepo{}, ws: uuid.New(), author: uuid.New()}
	f.service = service.NewCommentService(f.comments, issueRepo, hub, notifications)
	f.h = NewIssueHandler(issues, f.service, &testUserRepo{}, &testTeamStatusRepo{}, nil, nil, nil)
	f.issue = &domain.Issue{ID: uuid.New(), WorkspaceID: f.ws, Identifier: "ENG-1", Title: "One", CreatorID: f.author}
	f.other = &domain.Issue{ID: uuid.New(), WorkspaceID: f.ws, Identifier: "ENG-2", Title: "Two", CreatorID: f.author}
	issueRepo.issues["ENG-1"], issueRepo.issues["ENG-2"] = f.issue, f.other
	f.comment = domain.Comment{ID: uuid.New(), IssueID: f.issue.ID, UserID: f.author, Body: "<p>Original</p>"}
	f.comments.comments = []domain.Comment{f.comment}
	return f
}

// call runs handler for a caller with role, through the route's permission
// check, and returns the status and decoded body.
func (f *commentEditFixture) call(t *testing.T, handler echo.HandlerFunc, perm, method, identifier, commentID, body string, role string, user uuid.UUID) (int, map[string]any) {
	t.Helper()
	c, rec := setupIssueContext(echo.New(), method, "/api/workspaces/test/issues/"+identifier+"/comments/"+commentID, body)
	c.Set("workspace", &domain.Workspace{ID: f.ws, Slug: "test"})
	c.Set("workspace_role", role)
	c.Set("user_id", user)
	c.SetPathValues(echo.PathValues{{Name: "identifier", Value: identifier}, {Name: "commentId", Value: commentID}})
	require.NoError(t, mw.RequirePermission(perm)(handler)(c))
	var decoded map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &decoded)
	return rec.Code, decoded
}

func TestUpdateCommentIsAuthorOnly(t *testing.T) {
	f := newCommentEditFixture(t)
	id := f.comment.ID.String()
	edit := func(role string, user uuid.UUID, identifier string) (int, map[string]any) {
		return f.call(t, f.h.UpdateComment, "issue:create", http.MethodPatch, identifier, id, `{"body":"<p>Edited</p>"}`, role, user)
	}

	code, _ := edit(domain.RoleMember, uuid.New(), "ENG-1")
	require.Equal(t, http.StatusForbidden, code, "another member cannot edit")
	code, _ = edit(domain.RoleAdmin, uuid.New(), "ENG-1")
	require.Equal(t, http.StatusForbidden, code, "an admin cannot edit someone else's comment")
	code, _ = edit(domain.RoleGuest, f.author, "ENG-1")
	require.Equal(t, http.StatusForbidden, code, "a guest cannot write comments")
	code, _ = edit(domain.RoleMember, f.author, "ENG-2")
	require.Equal(t, http.StatusNotFound, code, "a comment is edited through its own issue")
	require.Equal(t, "<p>Original</p>", f.comments.comments[0].Body)

	code, body := edit(domain.RoleMember, f.author, "ENG-1")
	require.Equal(t, http.StatusOK, code)
	data := body
	require.Equal(t, "<p>Edited</p>", data["body"])
	require.NotEmpty(t, data["edited_at"])

	code, _ = f.call(t, f.h.UpdateComment, "issue:create", http.MethodPatch, "ENG-1", id, `{"body":""}`, domain.RoleMember, f.author)
	require.Equal(t, http.StatusBadRequest, code, "an empty body is rejected")
}

func TestDeleteCommentIsAdminOnlyAndRestorable(t *testing.T) {
	f := newCommentEditFixture(t)
	id := f.comment.ID.String()
	remove := func(role string, identifier string) (int, map[string]any) {
		return f.call(t, f.h.DeleteComment, "issue:delete", http.MethodDelete, identifier, id, "", role, uuid.New())
	}
	restore := func(role string, identifier string) (int, map[string]any) {
		return f.call(t, f.h.RestoreComment, "issue:delete", http.MethodPost, identifier, id, "", role, uuid.New())
	}

	code, _ := remove(domain.RoleMember, "ENG-1")
	require.Equal(t, http.StatusForbidden, code, "members cannot delete comments")
	code, _ = f.call(t, f.h.DeleteComment, "issue:delete", http.MethodDelete, "ENG-1", id, "", domain.RoleMember, f.author)
	require.Equal(t, http.StatusForbidden, code, "not even their own")
	code, _ = remove(domain.RoleAdmin, "ENG-2")
	require.Equal(t, http.StatusNotFound, code, "a comment is deleted through its own issue")
	require.Len(t, f.comments.comments, 1)

	code, body := remove(domain.RoleAdmin, "ENG-1")
	require.Equal(t, http.StatusOK, code)
	data := body
	require.Equal(t, "deleted", data["status"])
	until, err := time.Parse(time.RFC3339, data["restorable_until"].(string))
	require.NoError(t, err)
	require.WithinDuration(t, time.Now().Add(service.DefaultIssueDeleteGracePeriod), until, time.Minute)
	require.Empty(t, f.comments.comments)
	code, _ = remove(domain.RoleOwner, "ENG-1")
	require.Equal(t, http.StatusNotFound, code, "a deleted comment is gone")

	code, _ = restore(domain.RoleMember, "ENG-1")
	require.Equal(t, http.StatusForbidden, code, "members cannot restore")
	code, _ = restore(domain.RoleAdmin, "ENG-2")
	require.Equal(t, http.StatusNotFound, code)
	code, body = restore(domain.RoleOwner, "ENG-1")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, id, body["id"])
	require.Len(t, f.comments.comments, 1)
}

func TestRestoreCommentPastTheGracePeriodIsGone(t *testing.T) {
	f := newCommentEditFixture(t)
	f.service.SetDeleteGracePeriod(time.Hour)
	id := f.comment.ID.String()
	code, body := f.call(t, f.h.DeleteComment, "issue:delete", http.MethodDelete, "ENG-1", id, "", domain.RoleAdmin, uuid.New())
	require.Equal(t, http.StatusOK, code)
	until, err := time.Parse(time.RFC3339, body["restorable_until"].(string))
	require.NoError(t, err)
	require.WithinDuration(t, time.Now().Add(time.Hour), until, time.Minute, "the configured grace period is reported")
	old := time.Now().Add(-2 * time.Hour)
	f.comments.deleted[0].DeletedAt = &old

	code, body = f.call(t, f.h.RestoreComment, "issue:delete", http.MethodPost, "ENG-1", id, "", domain.RoleAdmin, uuid.New())
	require.Equal(t, http.StatusGone, code)
	require.Equal(t, "GRACE_EXPIRED", body["error"].(map[string]any)["code"])
}
