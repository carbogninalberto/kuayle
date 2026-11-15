package service

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/realtime"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// --- Recording emitter ---

type emittedWebhook struct {
	workspace uuid.UUID
	event     string
	issue     domain.Issue
	comment   *domain.Comment
	actor     uuid.UUID
}

// webhookRecorder records what write paths announce, copying the issue and
// comment at call time as the real emitter snapshots them. Write paths may run
// on several goroutines, so its methods hold a mutex.
type webhookRecorder struct {
	mu      sync.Mutex
	sent    []emittedWebhook
	batches int // IssueEvents calls
}

func (r *webhookRecorder) IssueEvent(_ context.Context, ws uuid.UUID, event string, issue *domain.Issue, actor uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, emittedWebhook{workspace: ws, event: event, issue: *issue, actor: actor})
}

func (r *webhookRecorder) IssueEvents(_ context.Context, ws uuid.UUID, event string, issues []domain.Issue, actor uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.batches++
	for _, issue := range issues {
		r.sent = append(r.sent, emittedWebhook{workspace: ws, event: event, issue: issue, actor: actor})
	}
}

func (r *webhookRecorder) CommentCreated(_ context.Context, ws uuid.UUID, issue *domain.Issue, comment *domain.Comment, actor uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c := *comment
	r.sent = append(r.sent, emittedWebhook{workspace: ws, event: WebhookEventCommentCreated, issue: *issue, comment: &c, actor: actor})
}

func (r *webhookRecorder) CommentEvent(_ context.Context, ws uuid.UUID, event string, issue *domain.Issue, comment *domain.Comment, actor uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c := *comment
	r.sent = append(r.sent, emittedWebhook{workspace: ws, event: event, issue: *issue, comment: &c, actor: actor})
}

func (r *webhookRecorder) events() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	names := make([]string, len(r.sent))
	for i, sent := range r.sent {
		names[i] = sent.event
	}
	return names
}

// --- In-memory repositories ---

// webhookIssueRepo is an in-memory issue store. Reads return copies, as the
// database does.
type webhookIssueRepo struct {
	issues       map[uuid.UUID]domain.Issue
	updateErr    error
	deleteErr    error
	triagedFirst bool        // a concurrent request triaged first
	bulkChanged  []uuid.UUID // issues a bulk update reports as changed
	bulkWritten  int
	bulkDeleted  []uuid.UUID // issues a bulk delete reports as deleted
}

func newWebhookIssueRepo(issues ...domain.Issue) *webhookIssueRepo {
	r := &webhookIssueRepo{issues: map[uuid.UUID]domain.Issue{}}
	for _, issue := range issues {
		r.issues[issue.ID] = issue
	}
	return r
}

func (r *webhookIssueRepo) Create(_ context.Context, _ *sqlx.Tx, issue *domain.Issue) error {
	r.issues[issue.ID] = *issue
	return nil
}
func (r *webhookIssueRepo) NextNumber(context.Context, *sqlx.Tx, uuid.UUID) (int, error) {
	return len(r.issues) + 1, nil
}
func (r *webhookIssueRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.Issue, error) {
	issue, ok := r.issues[id]
	if !ok {
		return nil, nil
	}
	return &issue, nil
}
func (r *webhookIssueRepo) GetByIdentifier(_ context.Context, ws uuid.UUID, identifier string) (*domain.Issue, error) {
	for _, issue := range r.issues {
		if issue.WorkspaceID == ws && issue.Identifier == identifier {
			return &issue, nil
		}
	}
	return nil, nil
}
func (r *webhookIssueRepo) List(context.Context, uuid.UUID, dto.IssueFilterParams) ([]domain.Issue, int, error) {
	return nil, 0, nil
}
func (r *webhookIssueRepo) Update(_ context.Context, issue *domain.Issue) error {
	if r.updateErr != nil {
		return r.updateErr
	}
	r.issues[issue.ID] = *issue
	return nil
}
func (r *webhookIssueRepo) UpdateContent(ctx context.Context, issue *domain.Issue, _, _ bool) error {
	return r.Update(ctx, issue)
}
func (r *webhookIssueRepo) UpdateUntriaged(_ context.Context, issue *domain.Issue) error {
	if r.updateErr != nil {
		return r.updateErr
	}
	stored, ok := r.issues[issue.ID]
	if r.triagedFirst || (ok && stored.Triaged) {
		return repository.ErrIssueAlreadyTriaged
	}
	r.issues[issue.ID] = *issue
	return nil
}
func (r *webhookIssueRepo) Delete(_ context.Context, id uuid.UUID) error {
	if r.deleteErr != nil {
		return r.deleteErr
	}
	delete(r.issues, id)
	return nil
}
func (r *webhookIssueRepo) SetLabels(context.Context, uuid.UUID, []uuid.UUID) error { return nil }
func (r *webhookIssueRepo) ValidateLabelScope(context.Context, *uuid.UUID, []uuid.UUID) error {
	return nil
}
func (r *webhookIssueRepo) GetLabels(context.Context, uuid.UUID) ([]domain.Label, error) {
	return nil, nil
}
func (r *webhookIssueRepo) GetLabelsForIssues(context.Context, []uuid.UUID) (map[uuid.UUID][]domain.Label, error) {
	return map[uuid.UUID][]domain.Label{}, nil
}
func (r *webhookIssueRepo) SetAssignees(_ context.Context, issueID uuid.UUID, userIDs []uuid.UUID) error {
	// Like the database: the first assignee becomes assignee_id (NULL when the
	// list is empty) and the write bumps updated_at again.
	issue, ok := r.issues[issueID]
	if !ok {
		return nil
	}
	if len(userIDs) > 0 {
		first := userIDs[0]
		issue.AssigneeID = &first
	} else {
		issue.AssigneeID = nil
	}
	issue.UpdatedAt = issue.UpdatedAt.Add(time.Second)
	r.issues[issueID] = issue
	return nil
}
func (r *webhookIssueRepo) GetAssignees(context.Context, uuid.UUID) ([]uuid.UUID, error) {
	return nil, nil
}
func (r *webhookIssueRepo) GetAssigneesForIssues(context.Context, []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
	return map[uuid.UUID][]uuid.UUID{}, nil
}
func (r *webhookIssueRepo) ListSubIssues(_ context.Context, parentID uuid.UUID) ([]domain.Issue, error) {
	var subs []domain.Issue
	for _, issue := range r.issues {
		if issue.ParentID != nil && *issue.ParentID == parentID {
			subs = append(subs, issue)
		}
	}
	return subs, nil
}
func (r *webhookIssueRepo) CountSubIssues(ctx context.Context, parentID uuid.UUID) (int, int, error) {
	subs, _ := r.ListSubIssues(ctx, parentID)
	done := 0
	for _, sub := range subs {
		if sub.Status == domain.IssueStatusDone || sub.Status == domain.IssueStatusCancelled {
			done++
		}
	}
	return len(subs), done, nil
}
func (r *webhookIssueRepo) CountSubIssuesForIssues(context.Context, []uuid.UUID) (map[uuid.UUID]domain.SubIssueCount, error) {
	return map[uuid.UUID]domain.SubIssueCount{}, nil
}
func (r *webhookIssueRepo) WouldCreateCycle(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return false, nil
}
func (r *webhookIssueRepo) CycleIsActive(context.Context, uuid.UUID) (bool, error) { return false, nil }
func (r *webhookIssueRepo) BulkUpdate(ctx context.Context, ws uuid.UUID, ids []uuid.UUID, status *string, priority *int, assignee *uuid.UUID, statusID *uuid.UUID, cycle *uuid.UUID, cycleSet bool) (int, error) {
	n, _, err := r.BulkUpdateChanged(ctx, ws, ids, status, priority, assignee, statusID, cycle, cycleSet)
	return n, err
}

// BulkUpdateChanged applies the priority to every issue and reports the
// configured issues as changed.
func (r *webhookIssueRepo) BulkUpdateChanged(_ context.Context, _ uuid.UUID, _ []uuid.UUID, _ *string, priority *int, _ *uuid.UUID, _ *uuid.UUID, _ *uuid.UUID, _ bool) (int, []domain.Issue, error) {
	if r.updateErr != nil {
		return 0, nil, r.updateErr
	}
	changed := make([]domain.Issue, 0, len(r.bulkChanged))
	for _, id := range r.bulkChanged {
		issue := r.issues[id]
		if priority != nil {
			issue.Priority = domain.IssuePriority(*priority)
		}
		r.issues[id] = issue
		changed = append(changed, issue)
	}
	return r.bulkWritten, changed, nil
}
func (r *webhookIssueRepo) BulkDelete(ctx context.Context, ws uuid.UUID, ids []uuid.UUID) (int, error) {
	deleted, err := r.BulkDeleteReturning(ctx, ws, ids)
	return len(deleted), err
}
func (r *webhookIssueRepo) BulkDeleteReturning(context.Context, uuid.UUID, []uuid.UUID) ([]domain.Issue, error) {
	deleted := make([]domain.Issue, 0, len(r.bulkDeleted))
	for _, id := range r.bulkDeleted {
		if issue, ok := r.issues[id]; ok {
			deleted = append(deleted, issue)
		}
		delete(r.issues, id)
	}
	return deleted, nil
}
func (r *webhookIssueRepo) BulkSetProject(context.Context, uuid.UUID, []uuid.UUID, *uuid.UUID, uuid.UUID) ([]domain.Issue, error) {
	return nil, nil
}
func (r *webhookIssueRepo) BulkSetParent(context.Context, uuid.UUID, []uuid.UUID, *uuid.UUID, uuid.UUID) ([]domain.Issue, error) {
	return nil, nil
}
func (r *webhookIssueRepo) BeginTx(context.Context) (*sqlx.Tx, error) {
	return nil, errors.New("no database in unit tests")
}

type webhookTeamRepo struct {
	repository.TeamRepo
	teams map[uuid.UUID]domain.Team
}

func (r *webhookTeamRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.Team, error) {
	team, ok := r.teams[id]
	if !ok {
		return nil, nil
	}
	return &team, nil
}

type webhookStatusRepo struct {
	repository.TeamStatusRepo
	statuses []domain.TeamStatus
}

func (r *webhookStatusRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.TeamStatus, error) {
	for i := range r.statuses {
		if r.statuses[i].ID == id {
			return &r.statuses[i], nil
		}
	}
	return nil, nil
}
func (r *webhookStatusRepo) GetByTeamAndSlug(_ context.Context, team uuid.UUID, slug string) (*domain.TeamStatus, error) {
	for i := range r.statuses {
		if r.statuses[i].TeamID == team && r.statuses[i].Slug == slug {
			return &r.statuses[i], nil
		}
	}
	return nil, nil
}
func (r *webhookStatusRepo) ListByTeam(_ context.Context, team uuid.UUID) ([]domain.TeamStatus, error) {
	var out []domain.TeamStatus
	for _, status := range r.statuses {
		if status.TeamID == team {
			out = append(out, status)
		}
	}
	return out, nil
}

type webhookHistoryRepo struct{ repository.IssueHistoryRepo }

func (webhookHistoryRepo) Create(context.Context, uuid.UUID, uuid.UUID, string, *string, *string) error {
	return nil
}

type webhookProjectRepo struct{ repository.ProjectRepo }

func (webhookProjectRepo) Create(context.Context, *domain.Project) error { return nil }

// --- Fixture ---

type issueWebhookFixture struct {
	ws, team, actor uuid.UUID
	todo, done      domain.TeamStatus
	issues          *webhookIssueRepo
	teams           *webhookTeamRepo
	svc             *IssueService
	sent            *webhookRecorder
}

func newIssueWebhookFixture(t *testing.T, issues ...func(f *issueWebhookFixture) domain.Issue) *issueWebhookFixture {
	t.Helper()
	f := &issueWebhookFixture{ws: uuid.New(), team: uuid.New(), actor: uuid.New(), sent: &webhookRecorder{}}
	f.todo = domain.TeamStatus{ID: uuid.New(), TeamID: f.team, Name: "Todo", Slug: "todo", Category: domain.StatusCategoryUnstarted}
	f.done = domain.TeamStatus{ID: uuid.New(), TeamID: f.team, Name: "Done", Slug: "done", Category: domain.StatusCategoryCompleted}
	f.issues = newWebhookIssueRepo()
	f.teams = &webhookTeamRepo{teams: map[uuid.UUID]domain.Team{f.team: {ID: f.team, WorkspaceID: f.ws}}}
	f.svc = NewIssueService(f.issues, f.teams, &webhookStatusRepo{statuses: []domain.TeamStatus{f.todo, f.done}},
		webhookHistoryRepo{}, realtime.NewHub(), newTestNotifSvc(), webhookProjectRepo{})
	f.svc.SetWebhooks(f.sent)
	return f
}

// issue stores a todo issue the actor created (so no one is notified).
func (f *issueWebhookFixture) issue(identifier string, parent *uuid.UUID) domain.Issue {
	issue := domain.Issue{ID: uuid.New(), WorkspaceID: f.ws, TeamID: f.team, Identifier: identifier, Title: identifier,
		Status: domain.IssueStatusTodo, StatusID: &f.todo.ID, CreatorID: f.actor, ParentID: parent, Triaged: true}
	f.issues.issues[issue.ID] = issue
	return issue
}

// --- IssueService ---

func TestIssueWebhooksUpdateSendsTheUpdatedIssue(t *testing.T) {
	f := newIssueWebhookFixture(t)
	f.issue("ENG-1", nil)
	title := "Renamed"

	_, err := f.svc.Update(context.Background(), f.ws, f.actor, "ENG-1", dto.UpdateIssueRequest{Title: &title})
	require.NoError(t, err)

	require.Len(t, f.sent.sent, 1)
	sent := f.sent.sent[0]
	require.Equal(t, WebhookEventIssueUpdated, sent.event)
	require.Equal(t, f.ws, sent.workspace)
	require.Equal(t, f.actor, sent.actor)
	require.Equal(t, "Renamed", sent.issue.Title)
}

// SetAssignees writes assignee_id and bumps updated_at in its own transaction,
// so the announced issue must carry the stored values, not the older copy.
func TestIssueWebhooksUpdateWithAssigneesSendsTheStoredUpdatedAt(t *testing.T) {
	f := newIssueWebhookFixture(t)
	issue := f.issue("ENG-1", nil)
	issue.UpdatedAt = time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC)
	f.issues.issues[issue.ID] = issue
	// The actor's own assignment sends no notification.
	assignee := f.actor

	updated, err := f.svc.Update(context.Background(), f.ws, f.actor, "ENG-1", dto.UpdateIssueRequest{AssigneeIDs: []string{assignee.String()}})
	require.NoError(t, err)

	stored := f.issues.issues[issue.ID]
	require.True(t, stored.UpdatedAt.After(issue.UpdatedAt), "the assignee write bumps updated_at")
	require.Equal(t, &assignee, updated.AssigneeID)
	require.Equal(t, stored.UpdatedAt, updated.UpdatedAt, "the returned issue carries the stored updated_at")
	require.Len(t, f.sent.sent, 1)
	require.Equal(t, WebhookEventIssueUpdated, f.sent.sent[0].event)
	require.Equal(t, &assignee, f.sent.sent[0].issue.AssigneeID)
	require.Equal(t, stored.UpdatedAt, f.sent.sent[0].issue.UpdatedAt, "the webhook carries the stored updated_at")
}

func TestIssueWebhooksFailedOrEmptyUpdateSendsNothing(t *testing.T) {
	f := newIssueWebhookFixture(t)
	f.issue("ENG-1", nil)
	title := "Renamed"

	f.issues.updateErr = errors.New("write refused")
	_, err := f.svc.Update(context.Background(), f.ws, f.actor, "ENG-1", dto.UpdateIssueRequest{Title: &title})
	require.Error(t, err)
	require.Empty(t, f.sent.sent, "a failed write is not announced")

	f.issues.updateErr = nil
	same := "ENG-1"
	_, err = f.svc.Update(context.Background(), f.ws, f.actor, "ENG-1", dto.UpdateIssueRequest{Title: &same})
	require.NoError(t, err)
	require.Empty(t, f.sent.sent, "a request that changes nothing is not an update")

	_, err = f.svc.Update(context.Background(), f.ws, f.actor, "ENG-404", dto.UpdateIssueRequest{Title: &title})
	require.Error(t, err)
	require.Empty(t, f.sent.sent)
}

func TestIssueWebhooksDeleteSendsThePreviousIssueAndNotTheParent(t *testing.T) {
	f := newIssueWebhookFixture(t)
	parent := f.issue("ENG-1", nil)
	child := f.issue("ENG-2", &parent.ID)

	require.NoError(t, f.svc.Delete(context.Background(), f.ws, f.actor, "ENG-2"))

	require.Equal(t, []string{WebhookEventIssueDeleted}, f.sent.events(), "the parent's own data did not change")
	require.Equal(t, child.ID, f.sent.sent[0].issue.ID)
	require.Equal(t, "ENG-2", f.sent.sent[0].issue.Identifier)
	require.Equal(t, f.actor, f.sent.sent[0].actor)
	_, stillThere := f.issues.issues[child.ID]
	require.False(t, stillThere)
}

func TestIssueWebhooksTriageSendsTriaged(t *testing.T) {
	f := newIssueWebhookFixture(t)
	issue := f.issue("ENG-1", nil)
	issue.Triaged = false
	f.issues.issues[issue.ID] = issue

	_, err := f.svc.Triage(context.Background(), f.ws, f.actor, "ENG-1", true)
	require.NoError(t, err)

	require.Equal(t, []string{WebhookEventIssueTriaged}, f.sent.events())
	require.True(t, f.sent.sent[0].issue.Triaged)
	require.Equal(t, f.actor, f.sent.sent[0].actor)

	// Triaging again is refused and announces nothing.
	_, err = f.svc.Triage(context.Background(), f.ws, f.actor, "ENG-1", true)
	require.Error(t, err)
	require.Len(t, f.sent.sent, 1)
}

func TestIssueWebhooksDeleteLostToAConcurrentDeleteSendsNothing(t *testing.T) {
	f := newIssueWebhookFixture(t)
	f.issue("ENG-1", nil)
	f.issues.deleteErr = sql.ErrNoRows

	err := f.svc.Delete(context.Background(), f.ws, f.actor, "ENG-1")
	require.ErrorContains(t, err, "issue not found")
	require.Empty(t, f.sent.sent, "a delete that removed nothing is not announced")
}

func TestIssueWebhooksTriageLostToAConcurrentTriageSendsNothing(t *testing.T) {
	f := newIssueWebhookFixture(t)
	issue := f.issue("ENG-1", nil)
	issue.Triaged = false
	f.issues.issues[issue.ID] = issue
	f.issues.triagedFirst = true

	_, err := f.svc.Triage(context.Background(), f.ws, f.actor, "ENG-1", false)
	require.ErrorContains(t, err, "issue is already triaged")
	require.Empty(t, f.sent.sent, "a lost triage is not announced")
	require.Equal(t, issue.Status, f.issues.issues[issue.ID].Status, "the stored issue keeps the winner's status")
}

func TestIssueWebhooksTriageDeclineSendsOnlyTriaged(t *testing.T) {
	f := newIssueWebhookFixture(t)
	issue := f.issue("ENG-1", nil)
	issue.Triaged = false
	f.issues.issues[issue.ID] = issue

	triaged, err := f.svc.Triage(context.Background(), f.ws, f.actor, "ENG-1", false)
	require.NoError(t, err)

	require.Equal(t, []string{WebhookEventIssueTriaged}, f.sent.events(), "the move to cancelled is part of the triage, not a separate update")
	require.Equal(t, issue.ID, f.sent.sent[0].issue.ID)
	require.Equal(t, domain.IssueStatusCancelled, f.sent.sent[0].issue.Status)
	require.Equal(t, domain.IssueStatusCancelled, triaged.Status)
	require.True(t, f.sent.sent[0].issue.Triaged)
	require.Equal(t, f.actor, f.sent.sent[0].actor)
}

func TestIssueWebhooksBulkUpdateSendsOneEventPerChangedIssue(t *testing.T) {
	f := newIssueWebhookFixture(t)
	first, second, unchanged := f.issue("ENG-1", nil), f.issue("ENG-2", nil), f.issue("ENG-3", nil)
	f.issues.bulkChanged = []uuid.UUID{first.ID, second.ID}
	f.issues.bulkWritten = 3
	priority := 2

	n, err := f.svc.BulkUpdate(context.Background(), f.ws, f.actor, dto.BulkUpdateIssueRequest{
		IssueIDs: []string{first.ID.String(), second.ID.String(), unchanged.ID.String()}, Priority: &priority})
	require.NoError(t, err)
	require.Equal(t, 3, n)

	require.Equal(t, []string{WebhookEventIssueUpdated, WebhookEventIssueUpdated}, f.sent.events())
	require.Equal(t, 1, f.sent.batches, "one listing for the whole bulk update")
	got := map[uuid.UUID]domain.Issue{}
	for _, sent := range f.sent.sent {
		require.Equal(t, f.actor, sent.actor)
		got[sent.issue.ID] = sent.issue
	}
	require.Contains(t, got, first.ID)
	require.Contains(t, got, second.ID)
	require.Equal(t, domain.IssuePriority(2), got[first.ID].Priority, "the issue after the update")
}

func TestIssueWebhooksBulkUpdateFailureSendsNothing(t *testing.T) {
	f := newIssueWebhookFixture(t)
	first := f.issue("ENG-1", nil)
	f.issues.updateErr = errors.New("refused")
	priority := 2

	_, err := f.svc.BulkUpdate(context.Background(), f.ws, f.actor, dto.BulkUpdateIssueRequest{IssueIDs: []string{first.ID.String()}, Priority: &priority})
	require.Error(t, err)
	require.Empty(t, f.sent.sent)
}

func TestIssueWebhooksBulkDeleteSendsOneEventPerDeletedIssue(t *testing.T) {
	f := newIssueWebhookFixture(t)
	first, second, gone := f.issue("ENG-1", nil), f.issue("ENG-2", nil), f.issue("ENG-3", nil)
	// ENG-3 disappears concurrently: the repository deletes only two.
	f.issues.bulkDeleted = []uuid.UUID{first.ID, second.ID}

	n, err := f.svc.BulkDelete(context.Background(), f.ws, f.actor, true, dto.BulkDeleteIssueRequest{
		IssueIDs: []string{first.ID.String(), second.ID.String(), gone.ID.String()}})
	require.NoError(t, err)
	require.Equal(t, 2, n)

	require.Equal(t, []string{WebhookEventIssueDeleted, WebhookEventIssueDeleted}, f.sent.events())
	require.Equal(t, 1, f.sent.batches, "one listing for the whole bulk delete")
	require.Equal(t, "ENG-1", f.sent.sent[0].issue.Identifier, "the row as it was when deleted")
	require.Equal(t, "ENG-2", f.sent.sent[1].issue.Identifier)
	for _, sent := range f.sent.sent {
		require.Equal(t, f.actor, sent.actor)
	}
}

func TestIssueWebhooksStatusAutomationSendsTheAutoClosedParent(t *testing.T) {
	f := newIssueWebhookFixture(t)
	f.teams.teams[f.team] = domain.Team{ID: f.team, WorkspaceID: f.ws, ParentAutoCloseEnabled: true}
	parent := f.issue("ENG-1", nil)
	f.issue("ENG-2", &parent.ID)
	doneID := f.done.ID.String()

	_, err := f.svc.Update(context.Background(), f.ws, f.actor, "ENG-2", dto.UpdateIssueRequest{StatusID: &doneID})
	require.NoError(t, err)

	require.Equal(t, []string{WebhookEventIssueUpdated, WebhookEventIssueUpdated}, f.sent.events())
	require.Equal(t, "ENG-2", f.sent.sent[0].issue.Identifier)
	auto := f.sent.sent[1]
	require.Equal(t, parent.ID, auto.issue.ID)
	require.Equal(t, domain.IssueStatusDone, auto.issue.Status)
	require.Equal(t, f.actor, auto.actor, "automation acts for the user whose change caused it")
}

func TestIssueWebhooksConvertToProjectUpdatesEveryMovedIssue(t *testing.T) {
	f := newIssueWebhookFixture(t)
	issue := f.issue("ENG-1", nil)
	child := f.issue("ENG-2", &issue.ID)

	project, err := f.svc.ConvertToProject(context.Background(), f.ws, f.actor, "ENG-1")
	require.NoError(t, err)

	require.Equal(t, []string{WebhookEventIssueUpdated, WebhookEventIssueUpdated}, f.sent.events(), "the converted issue stays an issue")
	require.Equal(t, 1, f.sent.batches, "one listing for the whole conversion")
	got := map[uuid.UUID]domain.Issue{}
	for _, sent := range f.sent.sent {
		got[sent.issue.ID] = sent.issue
	}
	require.Equal(t, project.ID, *got[issue.ID].ProjectID)
	require.Equal(t, project.ID, *got[child.ID].ProjectID)
	require.Nil(t, got[child.ID].ParentID)
}

func TestIssueWebhooksAreOptional(t *testing.T) {
	f := newIssueWebhookFixture(t)
	f.svc.SetWebhooks(nil)
	f.issue("ENG-1", nil)
	title := "Renamed"

	_, err := f.svc.Update(context.Background(), f.ws, f.actor, "ENG-1", dto.UpdateIssueRequest{Title: &title})
	require.NoError(t, err)
	require.NoError(t, f.svc.Delete(context.Background(), f.ws, f.actor, "ENG-1"))

	unwired := NewIssueService(f.issues, f.teams, &webhookStatusRepo{}, webhookHistoryRepo{}, realtime.NewHub(), newTestNotifSvc())
	f.issue("ENG-3", nil)
	_, err = unwired.Update(context.Background(), f.ws, f.actor, "ENG-3", dto.UpdateIssueRequest{Title: &title})
	require.NoError(t, err)
	require.Empty(t, f.sent.sent)
}

// --- CommentService ---

type webhookCommentRepo struct {
	repository.CommentRepo
	stored  []domain.Comment
	failure error
}

func (r *webhookCommentRepo) Create(_ context.Context, comment *domain.Comment) error {
	if r.failure != nil {
		return r.failure
	}
	r.stored = append(r.stored, *comment)
	return nil
}

func (r *webhookCommentRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.Comment, error) {
	for _, comment := range r.stored {
		if comment.ID == id {
			return &comment, nil
		}
	}
	return nil, sql.ErrNoRows
}

// setResolved changes the stored comment's resolution like the database: it
// returns the comment only when its state changed.
func (r *webhookCommentRepo) setResolved(id uuid.UUID, resolved bool) (*domain.Comment, error) {
	if r.failure != nil {
		return nil, r.failure
	}
	for i := range r.stored {
		if r.stored[i].ID != id || (r.stored[i].ResolvedAt != nil) == resolved {
			continue
		}
		if resolved {
			now := time.Now().UTC()
			r.stored[i].ResolvedAt = &now
		} else {
			r.stored[i].ResolvedAt = nil
		}
		comment := r.stored[i]
		return &comment, nil
	}
	return nil, nil
}

func (r *webhookCommentRepo) Resolve(_ context.Context, id uuid.UUID) (*domain.Comment, error) {
	return r.setResolved(id, true)
}

func (r *webhookCommentRepo) Reopen(_ context.Context, id uuid.UUID) (*domain.Comment, error) {
	return r.setResolved(id, false)
}

// KUA-196: resolving and reopening are the edits a comment has; each change
// sends comment.updated with the stored comment, and a request that changes
// nothing sends nothing.
func TestCommentWebhooksResolveAndReopenSendCommentUpdated(t *testing.T) {
	f := newIssueWebhookFixture(t)
	issue := f.issue("ENG-1", nil)
	comment := domain.Comment{ID: uuid.New(), IssueID: issue.ID, UserID: f.actor, Body: "<p>Fix it</p>"}
	comments := &webhookCommentRepo{stored: []domain.Comment{comment}}
	svc := NewCommentService(comments, f.issues, realtime.NewHub(), newTestNotifSvc())
	sent := &webhookRecorder{}
	svc.SetWebhooks(sent)
	reviewer := uuid.New()

	resolved, err := svc.Resolve(context.Background(), comment.ID, reviewer)
	require.NoError(t, err)
	require.NotNil(t, resolved.ResolvedAt)
	require.Equal(t, []string{WebhookEventCommentUpdated}, sent.events())
	got := sent.sent[0]
	require.Equal(t, f.ws, got.workspace)
	require.Equal(t, "ENG-1", got.issue.Identifier)
	require.Equal(t, comment.ID, got.comment.ID)
	require.NotNil(t, got.comment.ResolvedAt, "the stored comment, resolved")
	require.Equal(t, reviewer, got.actor)

	again, err := svc.Resolve(context.Background(), comment.ID, reviewer)
	require.NoError(t, err)
	require.Equal(t, resolved.ResolvedAt, again.ResolvedAt, "resolving again keeps the first resolution")
	require.Len(t, sent.sent, 1, "resolving a resolved comment changes nothing")

	reopened, err := svc.Reopen(context.Background(), comment.ID, f.actor)
	require.NoError(t, err)
	require.Nil(t, reopened.ResolvedAt)
	require.Equal(t, []string{WebhookEventCommentUpdated, WebhookEventCommentUpdated}, sent.events())
	require.Nil(t, sent.sent[1].comment.ResolvedAt)
	require.Equal(t, f.actor, sent.sent[1].actor)
	_, err = svc.Reopen(context.Background(), comment.ID, f.actor)
	require.NoError(t, err)
	require.Len(t, sent.sent, 2, "reopening an open comment changes nothing")

	comments.failure = errors.New("refused")
	_, err = svc.Resolve(context.Background(), comment.ID, reviewer)
	require.Error(t, err)
	require.Len(t, sent.sent, 2, "a failed write is not announced")

	comments.failure = nil
	unwired := NewCommentService(comments, f.issues, realtime.NewHub(), newTestNotifSvc())
	_, err = unwired.Resolve(context.Background(), comment.ID, reviewer)
	require.NoError(t, err)
}

func TestCommentWebhooksCreatedCommentIsSentWithItsIssue(t *testing.T) {
	f := newIssueWebhookFixture(t)
	issue := f.issue("ENG-1", nil)
	comments := &webhookCommentRepo{}
	svc := NewCommentService(comments, f.issues, realtime.NewHub(), newTestNotifSvc())
	sent := &webhookRecorder{}
	svc.SetWebhooks(sent)

	comment, err := svc.Create(context.Background(), issue.ID, f.actor, dto.CreateCommentRequest{Body: "<p>Looks good</p>"})
	require.NoError(t, err)

	require.Len(t, sent.sent, 1)
	got := sent.sent[0]
	require.Equal(t, WebhookEventCommentCreated, got.event)
	require.Equal(t, f.ws, got.workspace)
	require.Equal(t, "ENG-1", got.issue.Identifier)
	require.Equal(t, comment.ID, got.comment.ID)
	require.Equal(t, "<p>Looks good</p>", got.comment.Body)
	require.Equal(t, f.actor, got.actor)

	comments.failure = errors.New("issue not visible")
	_, err = svc.Create(context.Background(), issue.ID, f.actor, dto.CreateCommentRequest{Body: "<p>Again</p>"})
	require.Error(t, err)
	require.Len(t, sent.sent, 1, "a comment that was not stored is not announced")

	unwired := NewCommentService(&webhookCommentRepo{}, f.issues, realtime.NewHub(), newTestNotifSvc())
	_, err = unwired.Create(context.Background(), issue.ID, f.actor, dto.CreateCommentRequest{Body: "<p>Quiet</p>"})
	require.NoError(t, err)
}

// --- Forge automation ---

type webhookTransitionRules struct{ rule *domain.GitHubAutoTransition }

func (r webhookTransitionRules) GetAutoTransitionByEvent(context.Context, uuid.UUID, string) (*domain.GitHubAutoTransition, error) {
	return r.rule, nil
}

func TestForgeWebhooksAutoTransitionIsSentAsTheSystem(t *testing.T) {
	f := newIssueWebhookFixture(t)
	issue := f.issue("ENG-1", nil)
	sent := &webhookRecorder{}
	linker := &forgeLinker{
		issueRepo:      f.issues,
		teamStatusRepo: &webhookStatusRepo{statuses: []domain.TeamStatus{f.todo, f.done}},
		historyRepo:    webhookHistoryRepo{},
		rules:          webhookTransitionRules{rule: &domain.GitHubAutoTransition{TargetStatus: "done", TargetStatusID: &f.done.ID}},
		webhooks:       sent,
	}

	linker.applyAutoTransition(context.Background(), f.ws, "pr_merged", &issue)

	require.Len(t, sent.sent, 1)
	require.Equal(t, WebhookEventIssueUpdated, sent.sent[0].event)
	require.Equal(t, uuid.Nil, sent.sent[0].actor)
	require.Equal(t, domain.IssueStatusDone, sent.sent[0].issue.Status)

	// The issue is already done: nothing changes and nothing is sent.
	linker.applyAutoTransition(context.Background(), f.ws, "pr_merged", &issue)
	require.Len(t, sent.sent, 1)

	f.issues.updateErr = errors.New("refused")
	other := f.issue("ENG-2", nil)
	linker.applyAutoTransition(context.Background(), f.ws, "pr_merged", &other)
	require.Len(t, sent.sent, 1, "a failed transition is not announced")
}

// --- Agent tasks and messages ---

func TestAgentTaskWebhooksIssueStartIsSentForTheCreator(t *testing.T) {
	for _, changed := range []bool{true, false} {
		store := &taskStoreFake{task: domain.AgentTask{ID: uuid.New(), WorkspaceID: uuid.New(), CreatorID: uuid.New(), State: "preparing"}}
		if changed {
			store.startedIssue = &domain.Issue{ID: uuid.New(), WorkspaceID: store.task.WorkspaceID, Status: domain.IssueStatusInProgress}
		}
		sent := &webhookRecorder{}
		svc := &AgentTaskService{store: store}
		svc.SetWebhooks(sent)
		task := store.task

		require.NoError(t, svc.recordStart(context.Background(), &task))

		if !changed {
			require.Empty(t, sent.sent, "an issue the start did not move is not announced")
			continue
		}
		require.Len(t, sent.sent, 1)
		require.Equal(t, WebhookEventIssueUpdated, sent.sent[0].event)
		require.Equal(t, store.task.CreatorID, sent.sent[0].actor)
		require.Equal(t, store.startedIssue.ID, sent.sent[0].issue.ID)
	}
}

type webhookCommentReader struct {
	comments map[uuid.UUID]domain.Comment
	readers  []domain.Access
}

func (r *webhookCommentReader) GetByID(ctx context.Context, id uuid.UUID) (*domain.Comment, error) {
	access, _ := domain.AccessFromContext(ctx)
	r.readers = append(r.readers, access)
	comment, ok := r.comments[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return &comment, nil
}

func TestAgentMessageWebhooksPostedCommentIsSentByItsAuthor(t *testing.T) {
	f := newIssueWebhookFixture(t)
	issue := f.issue("ENG-4", nil)
	author := uuid.New()
	comment := domain.Comment{ID: uuid.New(), IssueID: issue.ID, UserID: author, Body: "<p>From the agent</p>"}
	posted := &repository.AgentMessageComment{MessageID: uuid.New(), WorkspaceID: f.ws, IssueID: issue.ID, Identifier: "ENG-4", CommentID: comment.ID, AuthorID: author, Posted: true}
	skipped := &repository.AgentMessageComment{MessageID: uuid.New(), WorkspaceID: f.ws, IssueID: issue.ID, Identifier: "ENG-4"}
	reader := &webhookCommentReader{comments: map[uuid.UUID]domain.Comment{comment.ID: comment}}
	sent := &webhookRecorder{}
	svc := &AgentMessageService{store: &messageCommentStoreFake{due: []*repository.AgentMessageComment{skipped, posted}}}
	svc.SetWebhooks(sent, reader, f.issues)

	count, err := svc.PostComments(context.Background(), 5)
	require.NoError(t, err)
	require.Equal(t, 1, count)

	require.Len(t, sent.sent, 1, "only the posted comment is announced")
	got := sent.sent[0]
	require.Equal(t, WebhookEventCommentCreated, got.event)
	require.Equal(t, author, got.actor)
	require.Equal(t, comment.ID, got.comment.ID)
	require.Equal(t, "ENG-4", got.issue.Identifier)
	require.Equal(t, []domain.Access{{UserID: author, WorkspaceID: f.ws}}, reader.readers, "read with the author's access")

	quiet := &AgentMessageService{store: &messageCommentStoreFake{due: []*repository.AgentMessageComment{posted}}}
	_, err = quiet.PostComments(context.Background(), 1)
	require.NoError(t, err)
}

// Undo and restore (KUA-51) are not exercised by the webhook tests.
func (r *webhookIssueRepo) ListByIDs(context.Context, uuid.UUID, []uuid.UUID) ([]domain.Issue, error) {
	return nil, nil
}
func (r *webhookIssueRepo) ApplyUndo(context.Context, uuid.UUID, []domain.IssueUndo) ([]domain.Issue, error) {
	return nil, nil
}
func (r *webhookIssueRepo) GetDeletedByIdentifier(context.Context, uuid.UUID, string) (*domain.DeletedIssue, error) {
	return nil, nil
}
func (r *webhookIssueRepo) ListDeletedByIDs(context.Context, uuid.UUID, []uuid.UUID) ([]domain.DeletedIssue, error) {
	return nil, nil
}
func (r *webhookIssueRepo) Restore(context.Context, uuid.UUID, []uuid.UUID, time.Time) ([]domain.Issue, error) {
	return nil, nil
}
