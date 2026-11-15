package service

import (
	"context"
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

type undoServiceFixture struct {
	repo    *mockIssueRepo
	history *mockIssueHistoryRepo
	hub     *realtime.Hub
	svc     *IssueService
	ctx     context.Context
	ws      uuid.UUID
	user    uuid.UUID
}

func newUndoServiceFixture() *undoServiceFixture {
	f := &undoServiceFixture{repo: new(mockIssueRepo), history: new(mockIssueHistoryRepo),
		hub: realtime.NewHub(func(context.Context, uuid.UUID, uuid.UUID) (bool, bool, error) { return true, false, nil }),
		ctx: context.Background(), ws: uuid.New(), user: uuid.New()}
	f.svc = NewIssueService(f.repo, new(mockTeamRepo), new(mockTeamStatusRepo), f.history, f.hub, newTestNotifSvc())
	// The actor created the issues, so no notification is sent.
	f.repo.On("GetAssignees", mock.Anything, mock.Anything).Return([]uuid.UUID{}, nil).Maybe()
	return f
}

func (f *undoServiceFixture) issue(priority domain.IssuePriority, version int64) *domain.Issue {
	return &domain.Issue{ID: uuid.New(), WorkspaceID: f.ws, Identifier: "ENG-1", Title: "Issue", Priority: priority, CreatorID: f.user, Version: version}
}

func TestIssueService_UpdateWithUndo_ReturnsPreviousValuesAndVersion(t *testing.T) {
	f := newUndoServiceFixture()
	issue := f.issue(domain.PriorityNone, 3)
	projectID := uuid.New()
	issue.ProjectID = &projectID
	f.repo.On("GetByIdentifier", f.ctx, f.ws, "ENG-1").Return(issue, nil)
	f.repo.On("UpdateContent", repository.WithIssueHistoryActor(f.ctx, f.user), mock.AnythingOfType("*domain.Issue"), false, false).
		Run(func(args mock.Arguments) { args.Get(1).(*domain.Issue).Version = 4 }).Return(nil)
	f.history.On("Create", f.ctx, issue.ID, f.user, mock.Anything, mock.Anything, mock.Anything).Return(nil)

	priority, cycle := 2, ""
	updated, undo, err := f.svc.UpdateWithUndo(f.ctx, f.ws, f.user, "ENG-1", dto.UpdateIssueRequest{Priority: &priority, CycleID: &cycle})

	require.NoError(t, err)
	assert.EqualValues(t, 4, updated.Version)
	require.NotNil(t, undo)
	assert.EqualValues(t, 4, undo.ExpectedVersion, "the undo expects the version the edit left")
	require.NotNil(t, undo.Previous.Priority)
	assert.Equal(t, 0, *undo.Previous.Priority)
	assert.Nil(t, undo.Previous.CycleID, "an unchanged field is not part of the undo")
	assert.Nil(t, undo.Previous.ProjectID)
}

func TestIssueService_UpdateWithUndo_ContentEditsHaveNoUndo(t *testing.T) {
	f := newUndoServiceFixture()
	issue := f.issue(domain.PriorityNone, 1)
	f.repo.On("GetByIdentifier", f.ctx, f.ws, "ENG-1").Return(issue, nil)
	f.repo.On("UpdateContent", repository.WithIssueHistoryActor(f.ctx, f.user), mock.AnythingOfType("*domain.Issue"), true, false).Return(nil)
	f.history.On("Create", f.ctx, issue.ID, f.user, mock.Anything, mock.Anything, mock.Anything).Return(nil)

	title, priority := "Renamed", 1
	_, undo, err := f.svc.UpdateWithUndo(f.ctx, f.ws, f.user, "ENG-1", dto.UpdateIssueRequest{Title: &title, Priority: &priority})

	require.NoError(t, err)
	assert.Nil(t, undo, "title and description keep their own history instead")
}

func TestIssueService_Undo_RecordsHistoryAndBroadcasts(t *testing.T) {
	f := newUndoServiceFixture()
	before := f.issue(domain.PriorityHigh, 4)
	after := *before
	after.Priority, after.Version = domain.PriorityNone, 5
	f.repo.On("ListByIDs", f.ctx, f.ws, []uuid.UUID{before.ID}).Return([]domain.Issue{*before}, nil)
	f.repo.On("ApplyUndo", repository.WithIssueHistoryActor(f.ctx, f.user), f.ws, mock.MatchedBy(func(items []domain.IssueUndo) bool {
		return len(items) == 1 && items[0].IssueID == before.ID && items[0].ExpectedVersion == 4 &&
			items[0].Previous.Priority != nil && *items[0].Previous.Priority == domain.PriorityNone
	})).Return([]domain.Issue{after}, nil)
	oldPriority, newPriority := "2", "0"
	f.history.On("Create", f.ctx, before.ID, f.user, "priority", &oldPriority, &newPriority).Return(nil).Once()
	next := workspaceEvents(t, f.hub, f.ws, f.user)

	priority := 0
	undone, err := f.svc.Undo(f.ctx, f.ws, f.user, []dto.BulkIssueUndoItem{{ID: before.ID.String(), ExpectedVersion: 4, Previous: dto.IssueUndoPrevious{Priority: &priority}}})

	require.NoError(t, err)
	require.Len(t, undone, 1)
	f.history.AssertExpectations(t)
	eventType, payload, err := next(2 * time.Second)
	require.NoError(t, err)
	assert.Equal(t, "issue.updated", eventType, "other tabs update like for any edit")
	assert.Contains(t, payload, before.ID.String())
}

func TestIssueService_Undo_AfterConflictingEditChangesNothing(t *testing.T) {
	f := newUndoServiceFixture()
	before := f.issue(domain.PriorityLow, 6)
	f.repo.On("ListByIDs", f.ctx, f.ws, []uuid.UUID{before.ID}).Return([]domain.Issue{*before}, nil)
	f.repo.On("ApplyUndo", mock.Anything, f.ws, mock.Anything).Return(nil, repository.ErrEditedMeanwhile)

	priority := 0
	_, err := f.svc.Undo(f.ctx, f.ws, f.user, []dto.BulkIssueUndoItem{{ID: before.ID.String(), ExpectedVersion: 4, Previous: dto.IssueUndoPrevious{Priority: &priority}}})

	require.ErrorIs(t, err, ErrIssueEditedMeanwhile)
	f.history.AssertNotCalled(t, "Create", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestIssueService_Undo_RejectsEmptyOrDuplicateItems(t *testing.T) {
	f := newUndoServiceFixture()
	id := uuid.New().String()
	priority := 1
	_, err := f.svc.Undo(f.ctx, f.ws, f.user, []dto.BulkIssueUndoItem{{ID: id, ExpectedVersion: 1}})
	require.ErrorIs(t, err, ErrIssueUndoInvalid, "an undo must put something back")
	item := dto.BulkIssueUndoItem{ID: id, ExpectedVersion: 1, Previous: dto.IssueUndoPrevious{Priority: &priority}}
	_, err = f.svc.Undo(f.ctx, f.ws, f.user, []dto.BulkIssueUndoItem{item, item})
	require.ErrorIs(t, err, ErrIssueUndoInvalid)
	f.repo.AssertNotCalled(t, "ApplyUndo", mock.Anything, mock.Anything, mock.Anything)
}

func TestIssueService_BulkUpdateWithUndo_ReturnsOneItemPerChangedIssue(t *testing.T) {
	f := newUndoServiceFixture()
	first, second := f.issue(domain.PriorityNone, 1), f.issue(domain.PriorityLow, 7)
	ids := []uuid.UUID{first.ID, second.ID}
	firstAfter, secondAfter := *first, *second
	firstAfter.Priority, firstAfter.Version = domain.PriorityUrgent, 2
	secondAfter.Priority, secondAfter.Version = domain.PriorityUrgent, 8
	f.repo.On("ListByIDs", f.ctx, f.ws, ids).Return([]domain.Issue{*first, *second}, nil).Once()
	f.repo.On("ListByIDs", f.ctx, f.ws, ids).Return([]domain.Issue{firstAfter, secondAfter}, nil).Once()
	priority := 1
	f.repo.On("BulkUpdateChanged", f.ctx, f.ws, ids, (*string)(nil), &priority, (*uuid.UUID)(nil), (*uuid.UUID)(nil), (*uuid.UUID)(nil), false).Return(2, []domain.Issue{firstAfter, secondAfter}, nil)
	f.repo.On("GetByID", f.ctx, first.ID).Return(&firstAfter, nil)
	f.repo.On("GetByID", f.ctx, second.ID).Return(&secondAfter, nil)

	n, undo, err := f.svc.BulkUpdateWithUndo(f.ctx, f.ws, f.user, dto.BulkUpdateIssueRequest{IssueIDs: []string{first.ID.String(), second.ID.String()}, Priority: &priority})

	require.NoError(t, err)
	assert.Equal(t, 2, n)
	require.NotNil(t, undo)
	require.Len(t, undo.Items, 2, "the bulk edit is undone as one step")
	assert.Equal(t, first.ID.String(), undo.Items[0].ID)
	assert.EqualValues(t, 2, undo.Items[0].ExpectedVersion)
	assert.Equal(t, 0, *undo.Items[0].Previous.Priority)
	assert.EqualValues(t, 8, undo.Items[1].ExpectedVersion)
	assert.Equal(t, 4, *undo.Items[1].Previous.Priority)
}

func TestIssueService_RestoreIssue(t *testing.T) {
	f := newUndoServiceFixture()
	f.svc.SetDeleteGracePeriod(48 * time.Hour)
	issue := f.issue(domain.PriorityNone, 2)
	deleted := &domain.DeletedIssue{Issue: *issue, DeletedAt: time.Now()}
	f.repo.On("GetDeletedByIdentifier", f.ctx, f.ws, "ENG-1").Return(deleted, nil)

	_, err := f.svc.RestoreIssue(f.ctx, f.ws, uuid.New(), "ENG-1", false)
	require.ErrorIs(t, err, ErrIssueForbidden, "a member restores only issues they created")
	f.repo.AssertNotCalled(t, "Restore", mock.Anything, mock.Anything, mock.Anything, mock.Anything)

	f.repo.On("Restore", f.ctx, f.ws, []uuid.UUID{issue.ID}, mock.MatchedBy(func(since time.Time) bool {
		return time.Since(since) > 47*time.Hour && time.Since(since) < 49*time.Hour
	})).Return([]domain.Issue{*issue}, nil).Once()
	next := workspaceEvents(t, f.hub, f.ws, f.user)
	restored, err := f.svc.RestoreIssue(f.ctx, f.ws, f.user, "ENG-1", false)
	require.NoError(t, err)
	assert.Equal(t, issue.ID, restored.ID)
	eventType, payload, err := next(2 * time.Second)
	require.NoError(t, err)
	assert.Equal(t, "issue.restored", eventType)
	assert.Contains(t, payload, issue.ID.String())

	f.repo.On("Restore", f.ctx, f.ws, []uuid.UUID{issue.ID}, mock.Anything).Return(nil, repository.ErrRestoreExpired).Once()
	_, err = f.svc.RestoreIssue(f.ctx, f.ws, uuid.New(), "ENG-1", true)
	require.ErrorIs(t, err, ErrIssueRestoreExpired)
}
