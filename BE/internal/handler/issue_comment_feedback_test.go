package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/realtime"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

type commentFeedbackFake struct{ comments []domain.Comment }

func (f *commentFeedbackFake) RecordComment(_ context.Context, _, _ uuid.UUID, comment domain.Comment) error {
	f.comments = append(f.comments, comment)
	return nil
}

// Only comments people post in a signed-in session may steer a coding agent;
// tokens and agents acting for a user cannot, so automation cannot loop.
func TestIssueCommentsFromSessionsBecomeTaskFeedback(t *testing.T) {
	issueRepo := newTestIssueRepo()
	hub := realtime.NewHub()
	notifications := service.NewNotificationService(&testNotifRepo{})
	issueService := service.NewIssueService(issueRepo, newTestTeamRepo(), &testTeamStatusRepo{}, &testHistoryRepo{}, hub, notifications)
	comments := service.NewCommentService(&testCommentRepo{}, issueRepo, hub, notifications)
	feedback := &commentFeedbackFake{}
	comments.SetTaskFeedback(feedback)
	h := NewIssueHandler(issueService, comments, &testUserRepo{}, &testTeamStatusRepo{}, nil, nil, nil)
	post := func(pat bool) {
		t.Helper()
		c, rec := setupIssueContext(echo.New(), http.MethodPost, "/api/workspaces/test/issues/ENG-1/comments", `{"body":"<p>Rename the flag</p>"}`)
		ws := setWorkspaceContext(c)
		user := middleware.GetUserID(c)
		issueRepo.issues["ENG-1"] = &domain.Issue{ID: uuid.New(), WorkspaceID: ws, Identifier: "ENG-1", Title: "Flag", CreatorID: user}
		if pat {
			c.Set(middleware.TokenScopesKey, []string{"issue:create"})
		}
		c.SetParamNames("identifier")
		c.SetParamValues("ENG-1")
		require.NoError(t, h.CreateComment(c))
		require.Equal(t, http.StatusCreated, rec.Code)
	}
	post(false)
	require.Len(t, feedback.comments, 1)
	require.Contains(t, feedback.comments[0].Body, "Rename the flag")
	post(true)
	require.Len(t, feedback.comments, 1, "a personal access token comment is not feedback")

	issue := issueRepo.issues["ENG-1"]
	agent := domain.WithActingAgent(domain.WithInteractiveSession(context.Background()), "assistant")
	_, err := comments.Create(agent, issue.ID, issue.CreatorID, dto.CreateCommentRequest{Body: "Looks done"})
	require.NoError(t, err)
	require.Len(t, feedback.comments, 1, "a comment an agent posts for the user is not feedback")
}
