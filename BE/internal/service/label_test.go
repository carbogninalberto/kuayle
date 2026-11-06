package service

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/realtime"
	"github.com/kuayle/kuayle-backend/internal/repository"
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

func (f *fakeLabelRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.Label, error) {
	for i := range f.labels {
		if f.labels[i].ID == id {
			l := f.labels[i]
			return &l, nil
		}
	}
	return nil, nil
}

func (f *fakeLabelRepo) Update(_ context.Context, label *domain.Label) error {
	for i := range f.labels {
		if f.labels[i].ID == label.ID {
			f.labels[i] = *label
		}
	}
	return nil
}

func (f *fakeLabelRepo) ExistsByName(_ context.Context, ws uuid.UUID, project *uuid.UUID, name string, exclude uuid.UUID) (bool, error) {
	for _, l := range f.labels {
		if l.ID == exclude {
			continue
		}
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

func TestIssueService_RecordLabelChangeWhenMoveDropsLabels(t *testing.T) {
	ctx := context.Background()
	issueID, userID := uuid.New(), uuid.New()
	global, blocker := uuid.New(), uuid.New()
	repo := &mockIssueRepo{}
	history := new(mockIssueHistoryRepo)
	svc := &IssueService{issueRepo: repo, historyRepo: history}
	before := func(ids ...uuid.UUID) labelSnapshot {
		ls := make([]domain.Label, len(ids))
		names := []string{"global", "Launch blocker"}
		for i, id := range ids {
			ls[i] = domain.Label{ID: id, Name: names[i]}
		}
		repo.On("GetLabels", ctx, issueID).Return(ls, nil).Once()
		return svc.labelSnapshot(ctx, issueID)
	}

	snapshot := before(global, blocker)
	repo.On("GetLabels", ctx, issueID).Return([]domain.Label{{ID: global, Name: "global"}}, nil).Once()
	history.On("Create", ctx, issueID, userID, "labels", mock.MatchedBy(func(v *string) bool {
		return *v == "global, Launch blocker"
	}), mock.MatchedBy(func(v *string) bool { return *v == "global" })).Return(nil).Once()
	assert.True(t, svc.recordLabelChange(ctx, issueID, userID, snapshot))
	history.AssertExpectations(t)

	snapshot = before(global)
	repo.On("GetLabels", ctx, issueID).Return([]domain.Label{{ID: global, Name: "global"}}, nil).Once()
	assert.False(t, svc.recordLabelChange(ctx, issueID, userID, snapshot), "unchanged: no history call")
}

// Swapping a workspace label for a project label of the same name is a change.
func TestIssueService_RecordLabelChangeDetectsSameNameSwap(t *testing.T) {
	ctx := context.Background()
	issueID, userID := uuid.New(), uuid.New()
	repo := &mockIssueRepo{}
	history := new(mockIssueHistoryRepo)
	svc := &IssueService{issueRepo: repo, historyRepo: history}

	repo.On("GetLabels", ctx, issueID).Return([]domain.Label{{ID: uuid.New(), Name: "Bug"}}, nil).Once()
	snapshot := svc.labelSnapshot(ctx, issueID)
	repo.On("GetLabels", ctx, issueID).Return([]domain.Label{{ID: uuid.New(), Name: "Bug"}}, nil).Once()
	history.On("Create", ctx, issueID, userID, "labels", mock.Anything, mock.Anything).Return(nil).Once()
	assert.True(t, svc.recordLabelChange(ctx, issueID, userID, snapshot))
	history.AssertExpectations(t)
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

func TestLabelService_UpdateKeepsNamesUniquePerScope(t *testing.T) {
	svc, ws, projectA, projectB := newLabelFixture()
	ctx := context.Background()
	a, b := projectA.String(), projectB.String()
	create := func(name string, project *string) *domain.Label {
		l, err := svc.Create(ctx, ws, dto.CreateLabelRequest{Name: name, Color: "#000000", ProjectID: project})
		require.NoError(t, err)
		return l
	}
	todoA, bugB := create("Todo", &a), create("Todo", &b)
	create("Bug", nil)
	create("Bug", &a)
	rename := func(l *domain.Label, name string) error {
		_, err := svc.Update(ctx, ws, l.ID, dto.UpdateLabelRequest{Name: &name})
		return err
	}

	assert.ErrorIs(t, rename(todoA, "bug"), ErrLabelNameTaken, "same project, case-insensitive")
	assert.NoError(t, rename(todoA, "Todo"), "unchanged name is not a clash with itself")
	assert.NoError(t, rename(todoA, "TODO"), "changing only the case of its own name")
	assert.NoError(t, rename(bugB, "Bug"), "another project's Bug does not clash")
	assert.NoError(t, rename(todoA, "Fresh"))
}

// conflictingLabelRepo stands for a write that lost a race to the database's
// per-scope unique name index after passing ExistsByName.
type conflictingLabelRepo struct{ fakeLabelRepo }

func (r *conflictingLabelRepo) Create(context.Context, *domain.Label) error {
	return repository.ErrLabelNameTaken
}

func (r *conflictingLabelRepo) Update(context.Context, *domain.Label) error {
	return repository.ErrLabelNameTaken
}

func TestLabelService_MapsDatabaseNameConflicts(t *testing.T) {
	ctx := context.Background()
	ws := uuid.New()
	existing := domain.Label{ID: uuid.New(), WorkspaceID: ws, Name: "Todo"}
	repo := &conflictingLabelRepo{fakeLabelRepo{labels: []domain.Label{existing}}}
	svc := NewLabelService(repo, &fakeProjectRepo{})

	_, err := svc.Create(ctx, ws, dto.CreateLabelRequest{Name: "Bug", Color: "#000000"})
	assert.ErrorIs(t, err, ErrLabelNameTaken)
	name := "Bug"
	_, err = svc.Update(ctx, ws, existing.ID, dto.UpdateLabelRequest{Name: &name})
	assert.ErrorIs(t, err, ErrLabelNameTaken)
}
