package service

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/realtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type fakeLabelRepo struct {
	repository.LabelRepo
	labels []domain.Label
}

func (f *fakeLabelRepo) Create(_ context.Context, l *domain.Label) error {
	f.labels = append(f.labels, *l)
	return nil
}

func (f *fakeLabelRepo) ListByWorkspace(_ context.Context, ws uuid.UUID, project *uuid.UUID) ([]domain.Label, error) {
	var out []domain.Label
	for _, l := range f.labels {
		if l.WorkspaceID == ws && (project == nil || l.ProjectID == nil || *l.ProjectID == *project) {
			out = append(out, l)
		}
	}
	return out, nil
}

func (f *fakeLabelRepo) ExistsByName(_ context.Context, ws uuid.UUID, project *uuid.UUID, name string) (bool, error) {
	for _, l := range f.labels {
		sameScope := (l.ProjectID == nil && project == nil) || (l.ProjectID != nil && project != nil && *l.ProjectID == *project)
		if l.WorkspaceID == ws && sameScope && strings.EqualFold(l.Name, name) {
			return true, nil
		}
	}
	return false, nil
}

type fakeProjectRepo struct {
	repository.ProjectRepo
	projects map[uuid.UUID]*domain.Project
}

func (f *fakeProjectRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.Project, error) {
	return f.projects[id], nil
}

func newLabelFixture() (*LabelService, uuid.UUID, uuid.UUID, uuid.UUID) {
	ws, projectA, projectB := uuid.New(), uuid.New(), uuid.New()
	projects := &fakeProjectRepo{projects: map[uuid.UUID]*domain.Project{
		projectA: {ID: projectA, WorkspaceID: ws},
		projectB: {ID: projectB, WorkspaceID: ws},
	}}
	return NewLabelService(&fakeLabelRepo{}, projects), ws, projectA, projectB
}

func TestLabelService_CreateScopesNameUniquenessToProject(t *testing.T) {
	svc, ws, projectA, projectB := newLabelFixture()
	ctx := context.Background()
	a, b := projectA.String(), projectB.String()

	_, err := svc.Create(ctx, ws, dto.CreateLabelRequest{Name: "Bug", Color: "#ff0000"})
	require.NoError(t, err)

	_, err = svc.Create(ctx, ws, dto.CreateLabelRequest{Name: "bug", Color: "#ff0000"})
	assert.Error(t, err, "same name twice in the workspace scope")

	scoped, err := svc.Create(ctx, ws, dto.CreateLabelRequest{Name: "Bug", Color: "#ff0000", ProjectID: &a})
	require.NoError(t, err, "a project may reuse a workspace label's name")
	assert.Equal(t, projectA, *scoped.ProjectID)

	_, err = svc.Create(ctx, ws, dto.CreateLabelRequest{Name: "BUG", Color: "#ff0000", ProjectID: &a})
	assert.Error(t, err, "same name twice in one project")

	_, err = svc.Create(ctx, ws, dto.CreateLabelRequest{Name: "Bug", Color: "#ff0000", ProjectID: &b})
	assert.NoError(t, err, "the same name in another project")
}

func TestLabelService_CreateRejectsProjectOutsideWorkspace(t *testing.T) {
	svc, ws, _, _ := newLabelFixture()
	for name, project := range map[string]string{"unknown": uuid.NewString(), "malformed": "nope"} {
		t.Run(name, func(t *testing.T) {
			_, err := svc.Create(context.Background(), ws, dto.CreateLabelRequest{Name: "X", Color: "#000000", ProjectID: &project})
			assert.ErrorIs(t, err, ErrLabelProject)
		})
	}

	otherWorkspace := uuid.New()
	foreign := uuid.New()
	svc.projectRepo.(*fakeProjectRepo).projects[foreign] = &domain.Project{ID: foreign, WorkspaceID: otherWorkspace}
	id := foreign.String()
	_, err := svc.Create(context.Background(), ws, dto.CreateLabelRequest{Name: "X", Color: "#000000", ProjectID: &id})
	assert.ErrorIs(t, err, ErrLabelProject, "a project of another workspace")
}

func TestLabelService_ListIncludesWorkspaceAndProjectLabels(t *testing.T) {
	svc, ws, projectA, projectB := newLabelFixture()
	ctx := context.Background()
	a, b := projectA.String(), projectB.String()
	for _, req := range []dto.CreateLabelRequest{
		{Name: "global", Color: "#000000"},
		{Name: "only-a", Color: "#000000", ProjectID: &a},
		{Name: "only-b", Color: "#000000", ProjectID: &b},
	} {
		_, err := svc.Create(ctx, ws, req)
		require.NoError(t, err)
	}

	names := func(labels []domain.Label) []string {
		out := make([]string, len(labels))
		for i, l := range labels {
			out[i] = l.Name
		}
		return out
	}

	all, err := svc.ListByWorkspace(ctx, ws, nil)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"global", "only-a", "only-b"}, names(all))

	forA, err := svc.ListByWorkspace(ctx, ws, &a)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"global", "only-a"}, names(forA))

	unknown := uuid.NewString()
	_, err = svc.ListByWorkspace(ctx, ws, &unknown)
	assert.ErrorIs(t, err, ErrLabelProject)
}

type scopeCheckingIssueRepo struct {
	*mockIssueRepo
	err error
	got *uuid.UUID
}

func (r *scopeCheckingIssueRepo) ValidateLabelScope(_ context.Context, project *uuid.UUID, _ []uuid.UUID) error {
	r.got = project
	return r.err
}

func TestIssueService_ValidateLabelScope(t *testing.T) {
	project := uuid.New()
	labelIDs := []string{uuid.NewString(), "not-a-uuid"}

	repo := &scopeCheckingIssueRepo{mockIssueRepo: &mockIssueRepo{}, err: repository.ErrLabelProjectMismatch}
	svc := &IssueService{issueRepo: repo}
	err := svc.validateLabelScope(context.Background(), &project, labelIDs)
	assert.EqualError(t, err, "a project label can only be used on issues of that project")
	assert.Equal(t, project, *repo.got)

	repo.err = nil
	assert.NoError(t, svc.validateLabelScope(context.Background(), nil, labelIDs))
	assert.Nil(t, repo.got)
}

func TestIssueService_RecordLabelNamesWhenMoveDropsLabels(t *testing.T) {
	ctx := context.Background()
	issueID, userID := uuid.New(), uuid.New()
	repo := &mockIssueRepo{}
	history := new(mockIssueHistoryRepo)
	svc := &IssueService{issueRepo: repo, historyRepo: history}

	repo.On("GetLabels", ctx, issueID).Return([]domain.Label{{Name: "global"}}, nil).Once()
	history.On("Create", ctx, issueID, userID, "labels", mock.MatchedBy(func(v *string) bool {
		return *v == "global, Launch blocker"
	}), mock.MatchedBy(func(v *string) bool { return *v == "global" })).Return(nil).Once()
	svc.recordLabelNames(ctx, issueID, userID, "global, Launch blocker")
	history.AssertExpectations(t)

	repo.On("GetLabels", ctx, issueID).Return([]domain.Label{{Name: "global"}}, nil).Once()
	svc.recordLabelNames(ctx, issueID, userID, "global") // unchanged: no history call
}

// A move that also sends label_ids (even empty) still records what the move
// removed: the snapshot is taken before the repository drops the old labels.
func TestIssueService_UpdateRecordsLabelsDroppedByMoveWithLabelIDs(t *testing.T) {
	issueRepo := new(mockIssueRepo)
	historyRepo := new(mockIssueHistoryRepo)
	svc := NewIssueService(issueRepo, new(mockTeamRepo), new(mockTeamStatusRepo), historyRepo, realtime.NewHub(), newTestNotifSvc())

	ctx := context.Background()
	wsID, userID, issueID, projectA := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	issue := &domain.Issue{ID: issueID, WorkspaceID: wsID, Identifier: "ENG-1", Title: "T", ProjectID: &projectA}

	issueRepo.On("GetByIdentifier", ctx, wsID, "ENG-1").Return(issue, nil)
	issueRepo.On("GetLabels", ctx, issueID).Return([]domain.Label{{Name: "Launch blocker"}}, nil).Once()
	issueRepo.On("UpdateContent", repository.WithIssueHistoryActor(ctx, userID), mock.AnythingOfType("*domain.Issue"), false, false).Return(nil)
	issueRepo.On("SetLabels", ctx, issueID, []uuid.UUID{}).Return(nil)
	issueRepo.On("GetLabels", ctx, issueID).Return([]domain.Label{}, nil)
	issueRepo.On("GetAssignees", ctx, issueID).Return([]uuid.UUID{}, nil)
	historyRepo.On("Create", ctx, issueID, userID, "project", mock.Anything, mock.Anything).Return(nil)
	historyRepo.On("Create", ctx, issueID, userID, "labels", mock.MatchedBy(func(v *string) bool {
		return *v == "Launch blocker"
	}), mock.MatchedBy(func(v *string) bool { return *v == "" })).Return(nil).Once()

	noProject := ""
	_, err := svc.Update(ctx, wsID, userID, "ENG-1", dto.UpdateIssueRequest{ProjectID: &noProject, LabelIDs: []string{}})
	require.NoError(t, err)
	historyRepo.AssertCalled(t, "Create", ctx, issueID, userID, "labels", mock.Anything, mock.Anything)
}
