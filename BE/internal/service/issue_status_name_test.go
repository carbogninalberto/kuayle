package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/realtime"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// defaultWorkflow is a team's default statuses, in order. In Progress and In
// Review share the "started" category, so a status name must never be
// resolved through its category.
func defaultWorkflow(teamID uuid.UUID) []domain.TeamStatus {
	statuses := []domain.TeamStatus{}
	for i, s := range []struct {
		name, slug string
		category   domain.StatusCategory
	}{
		{"Backlog", "backlog", domain.StatusCategoryBacklog},
		{"Todo", "todo", domain.StatusCategoryUnstarted},
		{"In Progress", "in_progress", domain.StatusCategoryStarted},
		{"In Review", "in_review", domain.StatusCategoryStarted},
		{"Done", "done", domain.StatusCategoryCompleted},
		{"Cancelled", "cancelled", domain.StatusCategoryCancelled},
	} {
		statuses = append(statuses, domain.TeamStatus{ID: uuid.New(), TeamID: teamID, Name: s.name, Slug: s.slug, Category: s.category, Position: i})
	}
	return statuses
}

// KUA-195: update_issue with status "In Progress" on an issue in In Review
// kept In Review's status_id and stored the raw name as the legacy status.
func TestIssueService_Update_StatusNameStoresThatTeamStatus(t *testing.T) {
	for _, given := range []string{"In Progress", "  in progress ", "in_progress", "IN_PROGRESS"} {
		t.Run(given, func(t *testing.T) {
			issueRepo := new(mockIssueRepo)
			historyRepo := new(mockIssueHistoryRepo)
			statusRepo := new(mockTeamStatusRepo)
			svc := NewIssueService(issueRepo, new(mockTeamRepo), statusRepo, historyRepo, realtime.NewHub(), newTestNotifSvc())

			ctx := context.Background()
			wsID, userID, teamID := uuid.New(), uuid.New(), uuid.New()
			statuses := defaultWorkflow(teamID)
			inProgress, inReview := statuses[2], statuses[3]
			issue := &domain.Issue{ID: uuid.New(), WorkspaceID: wsID, TeamID: teamID, Identifier: "KUA-195", Title: "Bug", Status: "in_review", StatusID: &inReview.ID}

			issueRepo.On("GetByIdentifier", ctx, wsID, "KUA-195").Return(issue, nil)
			issueRepo.On("GetAssignees", ctx, issue.ID).Return([]uuid.UUID{}, nil).Maybe()
			statusRepo.On("ListByTeam", ctx, teamID).Return(statuses, nil)
			// The pre-fix lookup by slug, kept so the old code fails on the
			// assertions below rather than on the mock.
			statusRepo.On("GetByTeamAndSlug", ctx, teamID, mock.Anything).Return(nil, nil).Maybe()
			statusRepo.On("GetByID", ctx, inReview.ID).Return(&inReview, nil)
			statusRepo.On("GetByID", ctx, inProgress.ID).Return(&inProgress, nil)
			historyRepo.On("Create", ctx, issue.ID, userID, "status", mock.Anything, mock.Anything).Return(nil)
			var written domain.Issue
			issueRepo.On("UpdateContent", repository.WithIssueHistoryActor(ctx, userID), mock.AnythingOfType("*domain.Issue"), false, false).
				Run(func(args mock.Arguments) { written = *args.Get(1).(*domain.Issue) }).Return(nil)

			result, err := svc.Update(ctx, wsID, userID, "KUA-195", dto.UpdateIssueRequest{Status: &given})

			require.NoError(t, err)
			require.NotNil(t, written.StatusID)
			require.Equal(t, inProgress.ID, *written.StatusID, "the named status is stored")
			require.Equal(t, domain.IssueStatus("in_progress"), written.Status, "the legacy status is that status's slug")
			require.Equal(t, inProgress.ID, *result.StatusID)
			require.Equal(t, domain.IssueStatus("in_progress"), result.Status, "the response reflects what was stored")
			historyRepo.AssertCalled(t, "Create", ctx, issue.ID, userID, "status", ptr("In Review"), ptr("In Progress"))
		})
	}
}

func TestIssueService_Update_UnknownStatusNameIsRefused(t *testing.T) {
	issueRepo := new(mockIssueRepo)
	historyRepo := new(mockIssueHistoryRepo)
	statusRepo := new(mockTeamStatusRepo)
	svc := NewIssueService(issueRepo, new(mockTeamRepo), statusRepo, historyRepo, realtime.NewHub(), newTestNotifSvc())

	ctx := context.Background()
	wsID, userID, teamID := uuid.New(), uuid.New(), uuid.New()
	statuses := defaultWorkflow(teamID)
	issue := &domain.Issue{ID: uuid.New(), WorkspaceID: wsID, TeamID: teamID, Identifier: "KUA-195", Title: "Bug", Status: "todo", StatusID: &statuses[1].ID}
	issueRepo.On("GetByIdentifier", ctx, wsID, "KUA-195").Return(issue, nil)
	issueRepo.On("UpdateContent", mock.Anything, mock.Anything, false, false).Return(nil).Maybe()
	issueRepo.On("GetAssignees", ctx, issue.ID).Return([]uuid.UUID{}, nil).Maybe()
	historyRepo.On("Create", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	statusRepo.On("ListByTeam", ctx, teamID).Return(statuses, nil)
	statusRepo.On("GetByTeamAndSlug", ctx, teamID, mock.Anything).Return(nil, nil).Maybe()
	statusRepo.On("GetByID", ctx, mock.Anything).Return(nil, nil).Maybe()
	issueRepo.On("GetByID", ctx, mock.Anything).Return(nil, nil).Maybe()

	// A category is not a status name either: it would be ambiguous.
	for _, given := range []string{"Doing", "started"} {
		_, err := svc.Update(ctx, wsID, userID, "KUA-195", dto.UpdateIssueRequest{Status: &given})

		require.Error(t, err)
		require.Contains(t, err.Error(), `"`+given+`"`)
		require.Contains(t, err.Error(), "Backlog, Todo, In Progress, In Review, Done, Cancelled")
	}
	issueRepo.AssertNotCalled(t, "UpdateContent", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	historyRepo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	require.Equal(t, statuses[1].ID, *issue.StatusID)
	require.Equal(t, domain.IssueStatus("todo"), issue.Status)
}

// Renamed statuses can share a name; the caller must then pick by status_id.
func TestIssueService_Update_AmbiguousStatusNameIsRefused(t *testing.T) {
	issueRepo := new(mockIssueRepo)
	statusRepo := new(mockTeamStatusRepo)
	svc := NewIssueService(issueRepo, new(mockTeamRepo), statusRepo, new(mockIssueHistoryRepo), realtime.NewHub(), newTestNotifSvc())

	ctx := context.Background()
	wsID, teamID := uuid.New(), uuid.New()
	statuses := defaultWorkflow(teamID)
	statuses[3].Name = "In Progress"
	issue := &domain.Issue{ID: uuid.New(), WorkspaceID: wsID, TeamID: teamID, Identifier: "KUA-195", Status: "todo", StatusID: &statuses[1].ID}
	issueRepo.On("GetByIdentifier", ctx, wsID, "KUA-195").Return(issue, nil)
	statusRepo.On("ListByTeam", ctx, teamID).Return(statuses, nil)

	given := "in progress"
	_, err := svc.Update(ctx, wsID, uuid.New(), "KUA-195", dto.UpdateIssueRequest{Status: &given})

	require.ErrorContains(t, err, "status_id")
	issueRepo.AssertNotCalled(t, "UpdateContent", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// create_issue resolves the name the same way, before it takes an issue number.
func TestIssueService_Create_UnknownStatusNameIsRefused(t *testing.T) {
	issueRepo := new(mockIssueRepo)
	teamRepo := new(mockTeamRepo)
	statusRepo := new(mockTeamStatusRepo)
	svc := NewIssueService(issueRepo, teamRepo, statusRepo, new(mockIssueHistoryRepo), realtime.NewHub(), newTestNotifSvc())

	ctx := context.Background()
	wsID, teamID := uuid.New(), uuid.New()
	teamRepo.On("GetByID", ctx, teamID).Return(&domain.Team{ID: teamID, WorkspaceID: wsID, Key: "KUA"}, nil)
	statusRepo.On("ListByTeam", ctx, teamID).Return(defaultWorkflow(teamID), nil)

	_, err := svc.Create(ctx, wsID, uuid.New(), dto.CreateIssueRequest{Title: "Bug", TeamID: teamID.String(), Status: "Doing"})

	require.ErrorContains(t, err, `"Doing"`)
	require.ErrorContains(t, err, "Backlog, Todo, In Progress, In Review, Done, Cancelled")
	issueRepo.AssertNotCalled(t, "BeginTx", mock.Anything)
}
