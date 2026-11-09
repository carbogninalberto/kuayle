package service

import (
	"context"
	"errors"
	"fmt"
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
)

func bulkParentRequest(parent string, ids ...uuid.UUID) dto.BulkUpdateIssueRequest {
	raw := make([]string, len(ids))
	for i, id := range ids {
		raw[i] = id.String()
	}
	return dto.BulkUpdateIssueRequest{IssueIDs: raw, ParentID: &parent}
}

// The repository sets the parent in one transaction; the service publishes one
// issue.updated per changed issue and counts only those.
func TestIssueService_BulkUpdate_SetsParentOncePerIssue(t *testing.T) {
	issueRepo := new(mockIssueRepo)
	history := new(mockIssueHistoryRepo)
	notifRepo := new(mockNotificationRepo)
	hub := realtime.NewHub(func(context.Context, uuid.UUID, uuid.UUID) (bool, bool, error) { return true, false, nil })
	svc := NewIssueService(issueRepo, new(mockTeamRepo), new(mockTeamStatusRepo), history, hub, NewNotificationService(notifRepo))

	ctx := context.Background()
	wsID, userID, parentID := uuid.New(), uuid.New(), uuid.New()
	changedID, sameID := uuid.New(), uuid.New()
	changed := domain.Issue{ID: changedID, WorkspaceID: wsID, ParentID: &parentID}
	// Repeated IDs reach the repository once, in request order.
	issueRepo.On("BulkSetParent", ctx, wsID, []uuid.UUID{changedID, sameID}, &parentID, userID).Return([]domain.Issue{changed}, nil).Once()
	next := workspaceEvents(t, hub, wsID, userID)

	updated, err := svc.BulkUpdate(ctx, wsID, userID, bulkParentRequest(parentID.String(), changedID, sameID, changedID))

	require.NoError(t, err)
	assert.Equal(t, 1, updated, "only issues whose parent changed are counted")
	issueRepo.AssertExpectations(t)
	// The repository reads, validates and records history itself.
	issueRepo.AssertNotCalled(t, "GetByID", mock.Anything, mock.Anything)
	issueRepo.AssertNotCalled(t, "Update", mock.Anything, mock.Anything)
	history.AssertNotCalled(t, "Create", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	notifRepo.AssertNotCalled(t, "CreateOrRefresh", mock.Anything, mock.Anything, mock.Anything)
	eventType, payload, err := next(2 * time.Second)
	require.NoError(t, err)
	assert.Equal(t, "issue.updated", eventType)
	assert.Contains(t, payload, changedID.String())
	_, _, err = next(150 * time.Millisecond)
	assert.Error(t, err, "no further events, as before")
}

func TestIssueService_BulkUpdate_RemovesParent(t *testing.T) {
	issueRepo := new(mockIssueRepo)
	svc := NewIssueService(issueRepo, new(mockTeamRepo), new(mockTeamStatusRepo), new(mockIssueHistoryRepo), realtime.NewHub(), newTestNotifSvc())

	ctx := context.Background()
	wsID, userID, issueID := uuid.New(), uuid.New(), uuid.New()
	issueRepo.On("BulkSetParent", ctx, wsID, []uuid.UUID{issueID}, (*uuid.UUID)(nil), userID).
		Return([]domain.Issue{{ID: issueID, WorkspaceID: wsID}}, nil).Once()

	updated, err := svc.BulkUpdate(ctx, wsID, userID, bulkParentRequest("", issueID))

	require.NoError(t, err)
	assert.Equal(t, 1, updated)
	issueRepo.AssertExpectations(t)
}

func TestIssueService_BulkUpdate_ParentRefusalsAreClientErrors(t *testing.T) {
	for repoErr, message := range map[error]string{
		repository.ErrBulkIssueNotFound:     "issue not found",
		repository.ErrBulkParentNotFound:    "parent issue not found",
		repository.ErrBulkParentWorkspace:   "parent issue must belong to the same workspace",
		repository.ErrBulkParentUnavailable: "resource reference is unavailable or crosses a private-team boundary",
		repository.ErrBulkOwnParent:         "issue cannot be its own parent",
		repository.ErrBulkParentCycle:       "parent would create a sub-issue cycle",
	} {
		issueRepo := new(mockIssueRepo)
		svc := NewIssueService(issueRepo, new(mockTeamRepo), new(mockTeamStatusRepo), new(mockIssueHistoryRepo), realtime.NewHub(), newTestNotifSvc())
		issueRepo.On("BulkSetParent", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil, repoErr)

		updated, err := svc.BulkUpdate(context.Background(), uuid.New(), uuid.New(), bulkParentRequest(uuid.NewString(), uuid.New()))

		assert.EqualError(t, err, message)
		assert.Zero(t, updated)
	}
}

func TestIssueService_BulkUpdate_ParentRejectsInvalidInput(t *testing.T) {
	issueRepo := new(mockIssueRepo)
	svc := NewIssueService(issueRepo, new(mockTeamRepo), new(mockTeamStatusRepo), new(mockIssueHistoryRepo), realtime.NewHub(), newTestNotifSvc())

	_, err := svc.BulkUpdate(context.Background(), uuid.New(), uuid.New(), bulkParentRequest("not-a-uuid", uuid.New()))
	assert.EqualError(t, err, "invalid parent_id")

	ids := make([]uuid.UUID, maxBulkProjectIssues+1)
	for i := range ids {
		ids[i] = uuid.New()
	}
	_, err = svc.BulkUpdate(context.Background(), uuid.New(), uuid.New(), bulkParentRequest("", ids...))
	assert.EqualError(t, err, fmt.Sprintf("at most %d issues can be moved at once", maxBulkProjectIssues))

	issueRepo.AssertNotCalled(t, "BulkSetParent", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// The change is one transaction: when it fails nothing changed, so nothing is
// published.
func TestIssueService_BulkUpdate_ParentFailureSignalsNothing(t *testing.T) {
	issueRepo := new(mockIssueRepo)
	hub := realtime.NewHub(func(context.Context, uuid.UUID, uuid.UUID) (bool, bool, error) { return true, false, nil })
	svc := NewIssueService(issueRepo, new(mockTeamRepo), new(mockTeamStatusRepo), new(mockIssueHistoryRepo), hub, newTestNotifSvc())
	ctx := context.Background()
	wsID, actorID := uuid.New(), uuid.New()
	failure := errors.New("update failed")
	issueRepo.On("BulkSetParent", ctx, wsID, mock.Anything, mock.Anything, actorID).Return(nil, failure)
	next := workspaceEvents(t, hub, wsID, actorID)

	updated, err := svc.BulkUpdate(ctx, wsID, actorID, bulkParentRequest(uuid.NewString(), uuid.New(), uuid.New()))

	assert.ErrorIs(t, err, failure)
	assert.Zero(t, updated)
	_, _, err = next(150 * time.Millisecond)
	assert.Error(t, err, "no events")
}
