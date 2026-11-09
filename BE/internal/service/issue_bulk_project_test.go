package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/realtime"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"nhooyr.io/websocket"
)

func bulkProjectRequest(project string, ids ...uuid.UUID) dto.BulkUpdateIssueRequest {
	raw := make([]string, len(ids))
	for i, id := range ids {
		raw[i] = id.String()
	}
	return dto.BulkUpdateIssueRequest{IssueIDs: raw, ProjectID: &project}
}

// workspaceEvents registers a websocket client with the hub and returns a
// function reading the next event type and payload published to it.
func workspaceEvents(t *testing.T, hub *realtime.Hub, workspaceID, userID uuid.UUID) func(time.Duration) (string, string, error) {
	t.Helper()
	registered := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		client := hub.Register(conn, workspaceID, userID)
		close(registered)
		hub.WritePump(r.Context(), client)
	}))
	t.Cleanup(server.Close)
	conn, _, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.CloseNow() })
	<-registered
	return func(timeout time.Duration) (string, string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		_, data, err := conn.Read(ctx)
		if err != nil {
			return "", "", err
		}
		var event struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		require.NoError(t, json.Unmarshal(data, &event))
		return event.Type, string(event.Payload), nil
	}
}

func TestIssueService_BulkUpdate_SetsProjectOncePerIssue(t *testing.T) {
	issueRepo := new(mockIssueRepo)
	hub := realtime.NewHub(func(context.Context, uuid.UUID, uuid.UUID) (bool, bool, error) { return true, false, nil })
	svc := NewIssueService(issueRepo, new(mockTeamRepo), new(mockTeamStatusRepo), new(mockIssueHistoryRepo), hub, newTestNotifSvc())

	ctx := context.Background()
	wsID, userID, projectID := uuid.New(), uuid.New(), uuid.New()
	moveID, sameID := uuid.New(), uuid.New()
	moved := domain.Issue{ID: moveID, WorkspaceID: wsID, ProjectID: &projectID}
	// Repeated IDs reach the repository once, in request order.
	issueRepo.On("BulkSetProject", ctx, wsID, []uuid.UUID{moveID, sameID}, &projectID, userID).Return([]domain.Issue{moved}, nil).Once()
	issueRepo.On("GetAssigneesForIssues", ctx, []uuid.UUID{moveID}).Return(map[uuid.UUID][]uuid.UUID{}, nil)
	next := workspaceEvents(t, hub, wsID, userID)

	updated, err := svc.BulkUpdate(ctx, wsID, userID, bulkProjectRequest(projectID.String(), moveID, sameID, moveID))

	require.NoError(t, err)
	assert.Equal(t, 1, updated, "only issues whose project changed are counted")
	issueRepo.AssertExpectations(t)
	eventType, payload, err := next(2 * time.Second)
	require.NoError(t, err)
	assert.Equal(t, "issue.updated", eventType)
	assert.Contains(t, payload, moveID.String())
	eventType, payload, err = next(2 * time.Second)
	require.NoError(t, err)
	assert.Equal(t, "issues.bulk_updated", eventType)
	assert.JSONEq(t, `{"count":1}`, payload)
}

func TestIssueService_BulkUpdate_RemovesProject(t *testing.T) {
	issueRepo := new(mockIssueRepo)
	svc := NewIssueService(issueRepo, new(mockTeamRepo), new(mockTeamStatusRepo), new(mockIssueHistoryRepo), realtime.NewHub(), newTestNotifSvc())

	ctx := context.Background()
	wsID, userID, issueID := uuid.New(), uuid.New(), uuid.New()
	issueRepo.On("BulkSetProject", ctx, wsID, []uuid.UUID{issueID}, (*uuid.UUID)(nil), userID).
		Return([]domain.Issue{{ID: issueID, WorkspaceID: wsID}}, nil).Once()
	issueRepo.On("GetAssigneesForIssues", ctx, []uuid.UUID{issueID}).Return(map[uuid.UUID][]uuid.UUID{}, nil)

	updated, err := svc.BulkUpdate(ctx, wsID, userID, bulkProjectRequest("", issueID))

	require.NoError(t, err)
	assert.Equal(t, 1, updated)
	issueRepo.AssertExpectations(t)
}

func TestIssueService_BulkUpdate_ProjectRefusalsAreClientErrors(t *testing.T) {
	for repoErr, message := range map[error]string{
		repository.ErrBulkIssueNotFound:      "issue not found",
		repository.ErrBulkProjectUnavailable: "resource reference is unavailable or crosses a private-team boundary",
	} {
		issueRepo := new(mockIssueRepo)
		svc := NewIssueService(issueRepo, new(mockTeamRepo), new(mockTeamStatusRepo), new(mockIssueHistoryRepo), realtime.NewHub(), newTestNotifSvc())
		issueRepo.On("BulkSetProject", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil, repoErr)

		updated, err := svc.BulkUpdate(context.Background(), uuid.New(), uuid.New(), bulkProjectRequest(uuid.NewString(), uuid.New()))

		assert.EqualError(t, err, message)
		assert.Zero(t, updated)
	}
}

func TestIssueService_BulkUpdate_ProjectRejectsTooManyIssues(t *testing.T) {
	issueRepo := new(mockIssueRepo)
	svc := NewIssueService(issueRepo, new(mockTeamRepo), new(mockTeamStatusRepo), new(mockIssueHistoryRepo), realtime.NewHub(), newTestNotifSvc())
	ids := make([]uuid.UUID, maxBulkProjectIssues+1)
	for i := range ids {
		ids[i] = uuid.New()
	}

	_, err := svc.BulkUpdate(context.Background(), uuid.New(), uuid.New(), bulkProjectRequest("", ids...))

	assert.EqualError(t, err, fmt.Sprintf("at most %d issues can be moved at once", maxBulkProjectIssues))
	issueRepo.AssertNotCalled(t, "BulkSetProject", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestIssueService_BulkUpdate_ProjectCannotBeCombined(t *testing.T) {
	svc := NewIssueService(new(mockIssueRepo), new(mockTeamRepo), new(mockTeamStatusRepo), new(mockIssueHistoryRepo), realtime.NewHub(), newTestNotifSvc())
	req := bulkProjectRequest(uuid.NewString(), uuid.New())
	priority := 1
	req.Priority = &priority

	_, err := svc.BulkUpdate(context.Background(), uuid.New(), uuid.New(), req)

	assert.EqualError(t, err, "project_id cannot be combined with other fields")
}

func TestIssueService_BulkUpdate_ProjectNotifiesRecipientsOncePerUser(t *testing.T) {
	issueRepo := new(mockIssueRepo)
	notifRepo := new(mockNotificationRepo)
	svc := NewIssueService(issueRepo, new(mockTeamRepo), new(mockTeamStatusRepo), new(mockIssueHistoryRepo), realtime.NewHub(), NewNotificationService(notifRepo))

	ctx := context.Background()
	wsID, actorID, creatorID, assigneeID, teamID, projectID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	first := domain.Issue{ID: uuid.New(), WorkspaceID: wsID, TeamID: teamID, CreatorID: creatorID}
	second := domain.Issue{ID: uuid.New(), WorkspaceID: wsID, TeamID: teamID, CreatorID: actorID}
	ids := []uuid.UUID{first.ID, second.ID}
	issueRepo.On("BulkSetProject", ctx, wsID, ids, &projectID, actorID).Return([]domain.Issue{first, second}, nil)
	issueRepo.On("GetAssigneesForIssues", ctx, ids).Return(map[uuid.UUID][]uuid.UUID{second.ID: {assigneeID, actorID}}, nil).Once()
	for _, user := range []uuid.UUID{creatorID, assigneeID} {
		notifRepo.On("CreateOrRefresh", ctx, mock.MatchedBy(func(n *domain.Notification) bool {
			return n.UserID == user && n.WorkspaceID == wsID && n.Type == "issues_updated" &&
				n.Title == "1 issues updated: project" && len(n.SourceTeamIDs) == 1 && n.SourceTeamIDs[0] == teamID.String()
		}), mock.Anything).Return(nil).Once()
	}

	updated, err := svc.BulkUpdate(ctx, wsID, actorID, bulkProjectRequest(projectID.String(), ids...))

	require.NoError(t, err)
	assert.Equal(t, 2, updated)
	notifRepo.AssertNumberOfCalls(t, "CreateOrRefresh", 2) // the actor is never notified
	notifRepo.AssertExpectations(t)
	issueRepo.AssertNotCalled(t, "GetAssignees", mock.Anything, mock.Anything)
}

// The move is one transaction: when it fails nothing moved, so nothing is
// published and nobody is notified.
func TestIssueService_BulkUpdate_ProjectFailureSignalsNothing(t *testing.T) {
	issueRepo := new(mockIssueRepo)
	notifRepo := new(mockNotificationRepo)
	hub := realtime.NewHub(func(context.Context, uuid.UUID, uuid.UUID) (bool, bool, error) { return true, false, nil })
	svc := NewIssueService(issueRepo, new(mockTeamRepo), new(mockTeamStatusRepo), new(mockIssueHistoryRepo), hub, NewNotificationService(notifRepo))
	ctx := context.Background()
	wsID, actorID := uuid.New(), uuid.New()
	failure := errors.New("update failed")
	issueRepo.On("BulkSetProject", ctx, wsID, mock.Anything, mock.Anything, actorID).Return(nil, failure)
	next := workspaceEvents(t, hub, wsID, actorID)

	updated, err := svc.BulkUpdate(ctx, wsID, actorID, bulkProjectRequest(uuid.NewString(), uuid.New(), uuid.New()))

	assert.ErrorIs(t, err, failure)
	assert.Zero(t, updated)
	notifRepo.AssertNotCalled(t, "CreateOrRefresh", mock.Anything, mock.Anything, mock.Anything)
	_, _, err = next(150 * time.Millisecond)
	assert.Error(t, err, "no events")
}

// subscribedIssueRepo adds subscribers to the mock repository.
type subscribedIssueRepo struct {
	*mockIssueRepo
	subscribers []uuid.UUID
}

func (r *subscribedIssueRepo) Subscribe(context.Context, uuid.UUID, uuid.UUID) error   { return nil }
func (r *subscribedIssueRepo) Unsubscribe(context.Context, uuid.UUID, uuid.UUID) error { return nil }
func (r *subscribedIssueRepo) IsSubscribed(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return false, nil
}
func (r *subscribedIssueRepo) GetSubscribedIssueIDs(context.Context, []uuid.UUID, uuid.UUID) (map[uuid.UUID]bool, error) {
	return nil, nil
}
func (r *subscribedIssueRepo) GetSubscribers(context.Context, uuid.UUID) ([]uuid.UUID, error) {
	return r.subscribers, nil
}
func (r *subscribedIssueRepo) GetSubscribersForIssues(_ context.Context, ids []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
	result := make(map[uuid.UUID][]uuid.UUID, len(ids))
	for _, id := range ids {
		result[id] = r.subscribers
	}
	return result, nil
}

func TestIssueService_BulkUpdate_ProjectDoesNotNotifyActorWhoOnlySubscribes(t *testing.T) {
	base := new(mockIssueRepo)
	ctx := context.Background()
	wsID, actorID, otherID, teamID, projectID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	issue := domain.Issue{ID: uuid.New(), WorkspaceID: wsID, TeamID: teamID, CreatorID: uuid.New()}
	issueRepo := &subscribedIssueRepo{mockIssueRepo: base, subscribers: []uuid.UUID{actorID, otherID}}
	notifRepo := new(mockNotificationRepo)
	svc := NewIssueService(issueRepo, new(mockTeamRepo), new(mockTeamStatusRepo), new(mockIssueHistoryRepo), realtime.NewHub(), NewNotificationService(notifRepo))

	base.On("BulkSetProject", ctx, wsID, []uuid.UUID{issue.ID}, &projectID, actorID).Return([]domain.Issue{issue}, nil)
	base.On("GetAssigneesForIssues", ctx, []uuid.UUID{issue.ID}).Return(map[uuid.UUID][]uuid.UUID{}, nil)
	notifRepo.On("CreateOrRefresh", ctx, mock.MatchedBy(func(n *domain.Notification) bool {
		return n.UserID == issue.CreatorID || n.UserID == otherID
	}), mock.Anything).Return(nil)

	updated, err := svc.BulkUpdate(ctx, wsID, actorID, bulkProjectRequest(projectID.String(), issue.ID))

	require.NoError(t, err)
	assert.Equal(t, 1, updated)
	notifRepo.AssertNumberOfCalls(t, "CreateOrRefresh", 2) // creator and the other subscriber, never the actor
}
