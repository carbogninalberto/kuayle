package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/importer"
	"github.com/kuayle/kuayle-backend/internal/repository"
)

// ---- fakes ---------------------------------------------------------------

type fakeImportRepo struct {
	jobs         map[uuid.UUID]*domain.ImportJob
	items        []domain.ImportJobItem
	refs         map[string]*domain.ImportExternalRef
	counters     map[uuid.UUID]repository.ImportCounterDelta
	heartbeats   map[uuid.UUID]int
	updatedAt    map[uuid.UUID]time.Time
	cancel       bool
	getRefErr    error
	upsertRefErr error
	finishCall   []struct {
		status string
		err    *string
	}
	wipes int
}

func newFakeImportRepo() *fakeImportRepo {
	return &fakeImportRepo{
		jobs: map[uuid.UUID]*domain.ImportJob{}, refs: map[string]*domain.ImportExternalRef{},
		counters: map[uuid.UUID]repository.ImportCounterDelta{}, heartbeats: map[uuid.UUID]int{},
		updatedAt: map[uuid.UUID]time.Time{},
	}
}

func refMapKey(ws uuid.UUID, source, site, typ, id string) string {
	return fmt.Sprintf("%s|%s|%s|%s|%s", ws, source, site, typ, id)
}

func (f *fakeImportRepo) Create(_ context.Context, job *domain.ImportJob) error {
	job.ID = uuid.New()
	job.Status = domain.ImportStatusDraft
	copyJob := *job
	f.jobs[job.ID] = &copyJob
	return nil
}
func (f *fakeImportRepo) Get(_ context.Context, ws, id uuid.UUID) (*domain.ImportJob, error) {
	job, ok := f.jobs[id]
	if !ok || job.WorkspaceID != ws {
		return nil, repository.ErrImportJobNotFound
	}
	copyJob := *job
	return &copyJob, nil
}
func (f *fakeImportRepo) List(_ context.Context, ws uuid.UUID, _, _ int) ([]domain.ImportJob, error) {
	out := []domain.ImportJob{}
	for _, job := range f.jobs {
		if job.WorkspaceID == ws {
			out = append(out, *job)
		}
	}
	return out, nil
}
func (f *fakeImportRepo) UpdateMapping(_ context.Context, ws, id uuid.UUID, raw json.RawMessage) error {
	job := f.jobs[id]
	if job == nil || job.WorkspaceID != ws {
		return repository.ErrImportJobNotFound
	}
	job.Mapping = raw
	return nil
}
func (f *fakeImportRepo) SetPreview(_ context.Context, ws, id uuid.UUID, raw json.RawMessage) error {
	job := f.jobs[id]
	if job == nil || job.WorkspaceID != ws {
		return repository.ErrImportJobNotFound
	}
	job.Preview = raw
	job.Status = domain.ImportStatusPreviewed
	return nil
}
func (f *fakeImportRepo) Start(_ context.Context, ws, id uuid.UUID, total int) error {
	job := f.jobs[id]
	if job == nil || job.WorkspaceID != ws {
		return repository.ErrImportJobNotFound
	}
	job.Status = domain.ImportStatusQueued
	job.Total = total
	return nil
}
func (f *fakeImportRepo) ClaimNext(_ context.Context) (*domain.ImportJob, error) { return nil, nil }
func (f *fakeImportRepo) IncrementCounters(_ context.Context, id uuid.UUID, d repository.ImportCounterDelta) error {
	c := f.counters[id]
	c.Processed += d.Processed
	c.Created += d.Created
	c.Updated += d.Updated
	c.Skipped += d.Skipped
	c.Failed += d.Failed
	f.counters[id] = c
	return nil
}
func (f *fakeImportRepo) AddItem(_ context.Context, item *domain.ImportJobItem) error {
	f.items = append(f.items, *item)
	return nil
}
func (f *fakeImportRepo) ListItems(_ context.Context, ws, id uuid.UUID, _, _ int) ([]domain.ImportJobItem, error) {
	out := []domain.ImportJobItem{}
	for _, item := range f.items {
		if item.WorkspaceID == ws && item.JobID == id {
			out = append(out, item)
		}
	}
	return out, nil
}
func (f *fakeImportRepo) Finish(_ context.Context, id uuid.UUID, status string, jobError *string) error {
	f.finishCall = append(f.finishCall, struct {
		status string
		err    *string
	}{status, jobError})
	if job := f.jobs[id]; job != nil {
		job.Status = status
	}
	return nil
}
func (f *fakeImportRepo) WipeCredentials(_ context.Context, _ uuid.UUID) error {
	f.wipes++
	return nil
}
func (f *fakeImportRepo) RequestCancel(_ context.Context, ws, id uuid.UUID) error {
	job := f.jobs[id]
	if job == nil || job.WorkspaceID != ws {
		return repository.ErrImportJobNotFound
	}
	f.cancel = true
	return nil
}
func (f *fakeImportRepo) IsCancelRequested(_ context.Context, _ uuid.UUID) (bool, error) {
	return f.cancel, nil
}
func (f *fakeImportRepo) Heartbeat(_ context.Context, jobID uuid.UUID) error {
	f.heartbeats[jobID]++
	f.updatedAt[jobID] = time.Now()
	return nil
}
func (f *fakeImportRepo) FailStaleRunning(_ context.Context, before time.Time) (int64, error) {
	var n int64
	for _, job := range f.jobs {
		if job.Status != domain.ImportStatusRunning {
			continue
		}
		updated, ok := f.updatedAt[job.ID]
		if !ok || updated.Before(before) {
			job.Status = domain.ImportStatusFailed
			n++
		}
	}
	return n, nil
}
func (f *fakeImportRepo) GetRef(_ context.Context, ws uuid.UUID, source, site, typ, id string) (*domain.ImportExternalRef, error) {
	if f.getRefErr != nil {
		return nil, f.getRefErr
	}
	return f.refs[refMapKey(ws, source, site, typ, id)], nil
}
func (f *fakeImportRepo) UpsertRef(_ context.Context, _ *sqlx.Tx, ref *domain.ImportExternalRef) error {
	if f.upsertRefErr != nil {
		return f.upsertRefErr
	}
	f.refs[refMapKey(ref.WorkspaceID, ref.Source, ref.SourceSite, ref.ExternalType, ref.ExternalID)] = ref
	return nil
}
func (f *fakeImportRepo) SetIssueTimestamps(context.Context, *sqlx.Tx, uuid.UUID, uuid.UUID, time.Time, time.Time) error {
	return nil
}
func (f *fakeImportRepo) SetCommentTimestamps(context.Context, *sqlx.Tx, uuid.UUID, uuid.UUID, time.Time, time.Time) error {
	return nil
}

type importFakeStore struct {
	ws           uuid.UUID
	teamID       uuid.UUID
	statusID     uuid.UUID
	importerID   uuid.UUID
	members      []importer.Member
	issues       map[uuid.UUID]*domain.Issue
	issueOrder   []uuid.UUID
	comments     []*domain.Comment
	relations    []*domain.IssueRelation
	parents      map[uuid.UUID]uuid.UUID
	labels       []string
	cycles       []string
	projects     []string
	assetCount   int
	assets       map[uuid.UUID]*domain.Asset
	repo         *fakeImportRepo
	failCreate   int
	number       int
	cycleErrName string
	assigneeSets []struct {
		issueID uuid.UUID
		userIDs []uuid.UUID
	}
}

func newImportFakeStore(ws, teamID, statusID, importerID uuid.UUID) *importFakeStore {
	return &importFakeStore{
		ws: ws, teamID: teamID, statusID: statusID, importerID: importerID,
		members: []importer.Member{{UserID: importerID, Email: "importer@example.com"}},
		issues:  map[uuid.UUID]*domain.Issue{}, parents: map[uuid.UUID]uuid.UUID{}, assets: map[uuid.UUID]*domain.Asset{}, number: 0,
	}
}

func (f *importFakeStore) Teams(context.Context, uuid.UUID) ([]domain.Team, error) {
	return []domain.Team{{ID: f.teamID, WorkspaceID: f.ws}}, nil
}
func (f *importFakeStore) Statuses(context.Context, uuid.UUID, uuid.UUID) ([]domain.TeamStatus, error) {
	return []domain.TeamStatus{{ID: f.statusID, TeamID: f.teamID, Category: domain.StatusCategoryUnstarted, IsDefault: true}}, nil
}
func (f *importFakeStore) Members(context.Context, uuid.UUID) ([]importer.Member, error) {
	return f.members, nil
}
func (f *importFakeStore) EnsureLabel(_ context.Context, _ uuid.UUID, _ *uuid.UUID, name string) (uuid.UUID, error) {
	f.labels = append(f.labels, name)
	return uuid.New(), nil
}
func (f *importFakeStore) EnsureCycle(_ context.Context, _, teamID uuid.UUID, name string) (uuid.UUID, error) {
	if teamID == uuid.Nil {
		return uuid.Nil, fmt.Errorf("cannot create cycle without a team")
	}
	if name == f.cycleErrName {
		return uuid.Nil, fmt.Errorf("cannot create cycle %s", name)
	}
	f.cycles = append(f.cycles, name)
	return uuid.New(), nil
}
func (f *importFakeStore) EnsureProject(_ context.Context, _, teamID uuid.UUID, name string) (uuid.UUID, error) {
	if teamID == uuid.Nil {
		return uuid.Nil, fmt.Errorf("cannot create project without a team")
	}
	f.projects = append(f.projects, name)
	return uuid.New(), nil
}
func (f *importFakeStore) CreateIssue(_ context.Context, ws, teamID uuid.UUID, issue *domain.Issue, _, _ time.Time, ref *domain.ImportExternalRef) error {
	if f.failCreate > 0 {
		f.failCreate--
		return errors.New("simulated create failure")
	}
	f.number++
	issue.ID = uuid.New()
	issue.WorkspaceID = ws
	issue.TeamID = teamID
	issue.Identifier = fmt.Sprintf("TEAM-%d", f.number)
	issue.Number = f.number
	if ref != nil {
		ref.KuayleID = issue.ID
		if f.repo != nil {
			if err := f.repo.UpsertRef(context.Background(), nil, ref); err != nil {
				return err
			}
		}
	}
	copyIssue := *issue
	f.issues[issue.ID] = &copyIssue
	f.issueOrder = append(f.issueOrder, issue.ID)
	return nil
}
func (f *importFakeStore) GetIssue(_ context.Context, ws, id uuid.UUID) (*domain.Issue, error) {
	issue := f.issues[id]
	if issue == nil || issue.WorkspaceID != ws {
		return nil, nil
	}
	copyIssue := *issue
	return &copyIssue, nil
}
func (f *importFakeStore) UpdateIssue(_ context.Context, _ uuid.UUID, issue *domain.Issue, _ time.Time) error {
	copyIssue := *issue
	f.issues[issue.ID] = &copyIssue
	return nil
}
func (f *importFakeStore) SetParent(_ context.Context, _, issueID, parentID, _ uuid.UUID) error {
	f.parents[issueID] = parentID
	return nil
}
func (f *importFakeStore) SetLabels(context.Context, uuid.UUID, uuid.UUID, []uuid.UUID) error {
	return nil
}
func (f *importFakeStore) SetAssignees(_ context.Context, _, issueID uuid.UUID, userIDs []uuid.UUID) error {
	f.assigneeSets = append(f.assigneeSets, struct {
		issueID uuid.UUID
		userIDs []uuid.UUID
	}{issueID, userIDs})
	return nil
}
func (f *importFakeStore) CreateComment(_ context.Context, _ uuid.UUID, comment *domain.Comment, _, _ time.Time) error {
	comment.ID = uuid.New()
	copyComment := *comment
	f.comments = append(f.comments, &copyComment)
	return nil
}
func (f *importFakeStore) CreateRelation(_ context.Context, _ uuid.UUID, issueID, relatedID uuid.UUID, typ domain.IssueRelationType) (uuid.UUID, error) {
	id := uuid.New()
	f.relations = append(f.relations, &domain.IssueRelation{ID: id, IssueID: issueID, RelatedIssueID: relatedID, Type: typ})
	return id, nil
}
func (f *importFakeStore) StoreAttachment(_ context.Context, _, _, _ uuid.UUID, filename string, _ io.Reader, _ int64, _ string) (*domain.Asset, string, error) {
	f.assetCount++
	asset := &domain.Asset{ID: uuid.New(), Filename: filename}
	f.assets[asset.ID] = asset
	return asset, "https://example.com/asset/" + asset.ID.String(), nil
}

func (f *importFakeStore) AssetURL(_ context.Context, _ uuid.UUID, assetID uuid.UUID) (string, string, error) {
	asset := f.assets[assetID]
	if asset == nil {
		return "", "", fmt.Errorf("asset %s not found", assetID)
	}
	return asset.Filename, "https://example.com/asset/" + assetID.String(), nil
}

type fakeSource struct {
	preview     *importer.SourcePreview
	issues      []importer.SourceIssue
	previewCall int
	fetchCall   int
	attachErr   map[string]error
	onEmit      func()
}

func (f *fakeSource) Preview(context.Context) (*importer.SourcePreview, error) {
	f.previewCall++
	if f.preview == nil {
		return &importer.SourcePreview{Counts: map[string]int{}}, nil
	}
	return f.preview, nil
}
func (f *fakeSource) Fetch(_ context.Context, emit importer.EmitFunc) error {
	f.fetchCall++
	for _, issue := range f.issues {
		if f.onEmit != nil {
			f.onEmit()
		}
		if err := emit(issue); err != nil {
			return err
		}
	}
	return nil
}
func (f *fakeSource) OpenAttachment(_ context.Context, a importer.SourceAttachment) (io.ReadCloser, error) {
	if err := f.attachErr[a.ExternalID]; err != nil {
		return nil, err
	}
	return io.NopCloser(strings.NewReader("bytes")), nil
}

// ---- harness -------------------------------------------------------------

type importHarness struct {
	svc    *ImportService
	repo   *fakeImportRepo
	store  *importFakeStore
	source *fakeSource
	job    *domain.ImportJob
	ws     uuid.UUID
	admin  uuid.UUID
}

func newHarness(t *testing.T) *importHarness {
	t.Helper()
	ws, admin, team, status := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	repo, store := newFakeImportRepo(), newImportFakeStore(ws, team, status, admin)
	store.repo = repo
	source := &fakeSource{attachErr: map[string]error{}}
	mapping := importer.NewMapping()
	mapping.Projects["10"] = importer.ProjectTarget{TeamID: team}
	mapping.Statuses["to do"] = importer.StatusTarget{TeamID: team, StatusID: status}
	mapping.Priorities["high"] = 2
	rawMapping, _ := json.Marshal(mapping)
	creds, _ := json.Marshal(map[string]string{"base_url": "https://example.atlassian.net", "token": " secret "})
	factory := func(context.Context, json.RawMessage, json.RawMessage) (importer.Source, error) { return source, nil }
	svc := NewImportService(repo, store, factory, cryptoKeyForTest(), 1024)
	job, err := svc.Create(context.Background(), ws, admin, ImportCreateInput{
		Source: domain.ImportSourceJira, Config: json.RawMessage(`{"jql":""}`),
		Credentials: creds,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.UpdateMapping(context.Background(), ws, job.ID, rawMapping); err != nil {
		t.Fatal(err)
	}
	return &importHarness{svc: svc, repo: repo, store: store, source: source, job: job, ws: ws, admin: admin}
}

func cryptoKeyForTest() []byte {
	return []byte("0123456789abcdef0123456789abcdef")
}

func (h *importHarness) previewAndStart(t *testing.T) {
	t.Helper()
	h.source.preview = &importer.SourcePreview{Counts: map[string]int{"issues": len(h.source.issues)}}
	if _, err := h.svc.Preview(context.Background(), h.ws, h.job.ID); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.Start(context.Background(), h.ws, h.job.ID); err != nil {
		t.Fatal(err)
	}
	job, _ := h.svc.Get(context.Background(), h.ws, h.job.ID)
	h.job = job
}

func sampleIssues(admin uuid.UUID) []importer.SourceIssue {
	created := time.Unix(1000, 0)
	return []importer.SourceIssue{
		{
			ExternalID: "A", ExternalKey: "PROJ-1", URL: "https://jira/PROJ-1", Title: "Parent",
			Description: "parent body", Status: "To Do", Priority: "High", ProjectExternalID: "10",
			CreatedAt: created, UpdatedAt: created,
			Reporter: &importer.SourceUser{ExternalID: "u1", Email: "original@example.com", DisplayName: "Original Author"},
			Assignee: &importer.SourceUser{ExternalID: "u1", Email: "original@example.com", DisplayName: "Original Author"},
			Labels:   []string{"backend"}, SprintIDs: []string{"Sprint 1"},
			Comments:        []importer.SourceComment{{ExternalID: "c1", Author: importer.SourceUser{ExternalID: "u9", DisplayName: "Ghost"}, Body: "hello", CreatedAt: created, UpdatedAt: created}},
			Attachments:     []importer.SourceAttachment{{ExternalID: "a1", Filename: "spec.pdf", Size: 5}},
			Relations:       []importer.SourceRelation{{ExternalID: "r1", SourceExternalID: "A", TargetExternalID: "B", Type: domain.RelationRelated}},
			HistoryMarkdown: "## History\n- changed status",
		},
		{
			ExternalID: "B", ExternalKey: "PROJ-2", Title: "Subtask", Status: "To Do", Priority: "High",
			ProjectExternalID: "10", ParentExternalID: "A", CreatedAt: created, UpdatedAt: created,
			Reporter: &importer.SourceUser{ExternalID: "u1", Email: "importer@example.com"},
		},
	}
}

// ---- tests ---------------------------------------------------------------

func TestCreateDerivesSourceSiteFromCredentials(t *testing.T) {
	ws, admin := uuid.New(), uuid.New()
	repo := newFakeImportRepo()
	store := newImportFakeStore(ws, uuid.New(), uuid.New(), admin)
	svc := NewImportService(repo, store, func(context.Context, json.RawMessage, json.RawMessage) (importer.Source, error) {
		return &fakeSource{}, nil
	}, cryptoKeyForTest(), 1024)

	create := func(baseURL string) (*domain.ImportJob, error) {
		creds, _ := json.Marshal(map[string]string{"base_url": baseURL, "email": "a@example.com", "token": "t"})
		return svc.Create(context.Background(), ws, admin, ImportCreateInput{
			Source: domain.ImportSourceJira, Config: json.RawMessage(`{}`), Credentials: creds,
		})
	}
	first, err := create("https://ALPHA.atlassian.net")
	if err != nil {
		t.Fatal(err)
	}
	second, err := create("https://beta.atlassian.net")
	if err != nil {
		t.Fatal(err)
	}
	if first.SourceSite != "alpha.atlassian.net" {
		t.Fatalf("expected lower-cased host, got %q", first.SourceSite)
	}
	if second.SourceSite != "beta.atlassian.net" {
		t.Fatalf("expected lower-cased host, got %q", second.SourceSite)
	}
	if first.SourceSite == second.SourceSite {
		t.Fatal("two Cloud sites must not share an idempotency site key")
	}

	creds, _ := json.Marshal(map[string]string{"base_url": "not-a-url", "token": "t"})
	_, err = svc.Create(context.Background(), ws, admin, ImportCreateInput{
		Source: domain.ImportSourceJira, Config: json.RawMessage(`{}`), Credentials: creds,
	})
	var validation *ImportValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("malformed base URL must be a validation error, got %v", err)
	}
}

func TestPreviewWritesNothing(t *testing.T) {
	h := newHarness(t)
	h.source.issues = sampleIssues(h.admin)
	h.source.preview = &importer.SourcePreview{Counts: map[string]int{"issues": 2}}
	if _, err := h.svc.Preview(context.Background(), h.ws, h.job.ID); err != nil {
		t.Fatal(err)
	}
	if len(h.store.issues) != 0 || len(h.store.comments) != 0 || h.store.assetCount != 0 {
		t.Fatal("preview wrote Kuayle entities")
	}
	if h.repo.jobs[h.job.ID].Status != domain.ImportStatusPreviewed {
		t.Fatal("preview did not set the previewed status")
	}
}

func TestFirstRunCreatesEverythingAndWipesCredentials(t *testing.T) {
	h := newHarness(t)
	h.source.issues = sampleIssues(h.admin)
	h.previewAndStart(t)
	h.svc.process(context.Background(), h.job)

	if len(h.store.issues) != 2 {
		t.Fatalf("expected 2 issues, got %d", len(h.store.issues))
	}
	if len(h.store.comments) < 2 {
		t.Fatalf("expected a comment and a history summary, got %d", len(h.store.comments))
	}
	if h.store.assetCount != 1 || len(h.store.relations) != 1 || len(h.store.parents) != 1 {
		t.Fatalf("attachments/relations/parents: %d/%d/%d", h.store.assetCount, len(h.store.relations), len(h.store.parents))
	}
	if len(h.store.cycles) != 1 || len(h.store.labels) != 1 {
		t.Fatalf("cycles/labels: %v/%v", h.store.cycles, h.store.labels)
	}
	// Kuayle numbering is sequential and independent of the Jira key.
	for i, id := range h.store.issueOrder {
		if h.store.issues[id].Number != i+1 {
			t.Fatalf("issue numbering is not sequential: %+v", h.store.issues[id])
		}
	}
	assertFinish(t, h.repo, domain.ImportStatusCompleted)
	if h.repo.wipes == 0 {
		t.Fatal("credentials were not wiped")
	}
}

func TestRerunSkipsThenUpdatesChanged(t *testing.T) {
	h := newHarness(t)
	h.source.issues = sampleIssues(h.admin)
	h.previewAndStart(t)
	h.svc.process(context.Background(), h.job)

	before := len(h.store.issues)
	firstCounters := h.repo.counters[h.job.ID]
	h.svc.process(context.Background(), h.job)
	if len(h.store.issues) != before {
		t.Fatal("re-run duplicated issues")
	}
	secondCounters := h.repo.counters[h.job.ID]
	if secondCounters.Created != firstCounters.Created {
		t.Fatal("re-run created issues instead of skipping")
	}
	if secondCounters.Skipped != 2 {
		t.Fatalf("re-run should skip two issues, got %d", secondCounters.Skipped)
	}

	h.source.issues[0].Title = "Changed title"
	h.svc.process(context.Background(), h.job)
	if h.repo.counters[h.job.ID].Updated <= secondCounters.Updated {
		t.Fatal("changed issue was not updated")
	}
	if len(h.store.issues) != before {
		t.Fatal("update created a duplicate")
	}
}

func TestUnmappedProjectWithSprintsFailsOnlyThatIssue(t *testing.T) {
	h := newHarness(t)
	h.source.issues = []importer.SourceIssue{
		{ExternalID: "ok", Title: "Mapped", Status: "To Do", ProjectExternalID: "10"},
		{ExternalID: "unmapped", Title: "Unmapped", Status: "To Do", ProjectExternalID: "999", SprintIDs: []string{"Sprint X"}, Labels: []string{"x"}, EpicExternalID: "EPIC-1"},
	}
	h.previewAndStart(t)
	h.svc.process(context.Background(), h.job)

	if len(h.store.issues) != 1 {
		t.Fatalf("expected only the mapped issue to be created, got %d", len(h.store.issues))
	}
	found := false
	for _, item := range h.repo.items {
		if item.ExternalID == "unmapped" && item.Outcome == domain.ImportOutcomeFailed {
			found = true
			if !strings.Contains(item.Reason, "no project mapping") {
				t.Fatalf("unexpected failure reason: %q", item.Reason)
			}
		}
	}
	if !found {
		t.Fatal("unmapped issue was not reported as a failed item")
	}
	assertFinish(t, h.repo, domain.ImportStatusCompleted)
}

func TestFailingCycleFailsOnlyThatIssue(t *testing.T) {
	h := newHarness(t)
	h.store.cycleErrName = "Bad Sprint"
	h.source.issues = []importer.SourceIssue{
		{ExternalID: "good", Title: "Good", Status: "To Do", ProjectExternalID: "10", SprintIDs: []string{"Good Sprint"}},
		{ExternalID: "bad", Title: "Bad", Status: "To Do", ProjectExternalID: "10", SprintIDs: []string{"Bad Sprint"}},
	}
	h.previewAndStart(t)
	h.svc.process(context.Background(), h.job)

	if len(h.store.issues) != 1 {
		t.Fatalf("expected only the issue with a valid cycle to be created, got %d", len(h.store.issues))
	}
	found := false
	for _, item := range h.repo.items {
		if item.ExternalID == "bad" && item.Outcome == domain.ImportOutcomeFailed && strings.Contains(item.Reason, "Bad Sprint") {
			found = true
		}
	}
	if !found {
		t.Fatalf("cycle error was not reported per item: %+v", h.repo.items)
	}
	assertFinish(t, h.repo, domain.ImportStatusCompleted)
}

func (h *importHarness) issueByExternalID(t *testing.T, extID string) *domain.Issue {
	t.Helper()
	ref := h.repo.refs[refMapKey(h.ws, domain.ImportSourceJira, h.job.SourceSite, domain.ImportExternalIssue, extID)]
	if ref == nil {
		t.Fatalf("no import ref for issue %s", extID)
	}
	issue := h.store.issues[ref.KuayleID]
	if issue == nil {
		t.Fatalf("no stored issue for %s", extID)
	}
	return issue
}

func TestHeartbeatAdvancesDuringLongFetch(t *testing.T) {
	h := newHarness(t)
	clock := time.Unix(1000, 0)
	h.svc.now = func() time.Time { return clock }
	h.source.issues = sampleIssues(h.admin)
	h.source.onEmit = func() { clock = clock.Add(31 * time.Second) }
	h.previewAndStart(t)
	h.svc.process(context.Background(), h.job)

	if h.repo.heartbeats[h.job.ID] == 0 {
		t.Fatal("long fetch never sent a heartbeat")
	}
	if h.repo.updatedAt[h.job.ID].IsZero() {
		t.Fatal("heartbeat did not advance the job's updated_at")
	}
}

func TestStaleRecoveryUsesHeartbeatNotStartTime(t *testing.T) {
	h := newHarness(t)
	h.previewAndStart(t)
	job := h.repo.jobs[h.job.ID]
	job.Status = domain.ImportStatusRunning
	job.StartedAt = &[]time.Time{time.Now().Add(-time.Hour)}[0]
	recent := time.Now()
	h.repo.updatedAt[h.job.ID] = recent

	n, err := h.repo.FailStaleRunning(context.Background(), recent.Add(-time.Minute))
	if err != nil || n != 0 {
		t.Fatalf("a job with a recent heartbeat must stay running, n=%d err=%v", n, err)
	}
	if h.repo.jobs[h.job.ID].Status != domain.ImportStatusRunning {
		t.Fatal("a live job was marked stale because of its old started_at")
	}
	n, err = h.repo.FailStaleRunning(context.Background(), recent.Add(time.Minute))
	if err != nil || n != 1 {
		t.Fatalf("an old heartbeat must make the job stale, n=%d err=%v", n, err)
	}
}

func TestRefErrorsFailTheItemWithoutCreatingIssues(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(*importHarness)
	}{
		{"get ref", func(h *importHarness) { h.repo.getRefErr = errors.New("ref store unavailable") }},
		{"upsert ref", func(h *importHarness) { h.repo.upsertRefErr = errors.New("ref store unavailable") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.source.issues = []importer.SourceIssue{
				{ExternalID: "only", Title: "Only", Status: "To Do", ProjectExternalID: "10"},
			}
			tc.set(h)
			h.previewAndStart(t)
			h.svc.process(context.Background(), h.job)

			if len(h.store.issues) != 0 {
				t.Fatalf("issue was created despite a ref error: %d", len(h.store.issues))
			}
			found := false
			for _, item := range h.repo.items {
				if item.ExternalID == "only" && item.Outcome == domain.ImportOutcomeFailed {
					found = true
				}
			}
			if !found {
				t.Fatalf("ref error was not reported as a failed item: %+v", h.repo.items)
			}
			assertFinish(t, h.repo, domain.ImportStatusCompleted)
		})
	}
}

func TestAttachmentLinkSurvivesUpdate(t *testing.T) {
	h := newHarness(t)
	h.source.issues = sampleIssues(h.admin)
	h.previewAndStart(t)
	h.svc.process(context.Background(), h.job)

	first := h.issueByExternalID(t, "A")
	if first.Description == nil || !strings.Contains(*first.Description, "spec.pdf") {
		t.Fatalf("first run did not link the attachment: %v", first.Description)
	}
	if h.store.assetCount != 1 {
		t.Fatalf("expected one stored asset, got %d", h.store.assetCount)
	}

	h.source.issues[0].Title = "Changed title"
	h.svc.process(context.Background(), h.job)

	after := h.issueByExternalID(t, "A")
	if after.Description == nil || !strings.Contains(*after.Description, "spec.pdf") {
		t.Fatalf("update dropped the attachment link: %v", after.Description)
	}
	if strings.Count(*after.Description, "**Attachments**") != 1 {
		t.Fatalf("expected a single attachments block: %q", *after.Description)
	}
	if h.store.assetCount != 1 {
		t.Fatalf("re-run stored a duplicate asset: %d", h.store.assetCount)
	}
}

func TestFailedCreateRelinksExistingAttachmentOnNextRun(t *testing.T) {
	h := newHarness(t)
	h.source.issues = sampleIssues(h.admin)
	h.store.failCreate = 1
	h.previewAndStart(t)
	h.svc.process(context.Background(), h.job)

	if h.store.assetCount != 1 {
		t.Fatalf("attachment should have been stored before the failed create, got %d", h.store.assetCount)
	}
	if len(h.store.issues) != 1 {
		t.Fatalf("only the second issue should have been created, got %d", len(h.store.issues))
	}
	assertFinish(t, h.repo, domain.ImportStatusCompleted)

	h.svc.process(context.Background(), h.job)

	if len(h.store.issues) != 2 {
		t.Fatalf("re-run should create the previously failed issue, got %d", len(h.store.issues))
	}
	after := h.issueByExternalID(t, "A")
	if after.Description == nil || !strings.Contains(*after.Description, "spec.pdf") {
		t.Fatalf("created issue did not link the existing asset: %v", after.Description)
	}
	if h.store.assetCount != 1 {
		t.Fatalf("re-run stored a duplicate asset: %d", h.store.assetCount)
	}
}

func TestUnmatchedAndNilAssigneesAreNotAssignedToImporter(t *testing.T) {
	h := newHarness(t)
	h.source.issues = []importer.SourceIssue{
		{ExternalID: "nil-assignee", Title: "Nil", Status: "To Do", ProjectExternalID: "10"},
		{ExternalID: "unmatched", Title: "Unmatched", Status: "To Do", ProjectExternalID: "10",
			Assignee: &importer.SourceUser{ExternalID: "ghost", Email: "ghost@example.com", DisplayName: "Ghost"}},
	}
	h.previewAndStart(t)
	h.svc.process(context.Background(), h.job)

	if len(h.store.issues) != 2 {
		t.Fatalf("expected 2 issues, got %d", len(h.store.issues))
	}
	for _, issue := range h.store.issues {
		if issue.AssigneeID != nil {
			t.Fatalf("issue %s has an assignee %s; unmatched/nil source assignees must be left unassigned", issue.Identifier, *issue.AssigneeID)
		}
	}
	for _, set := range h.store.assigneeSets {
		for _, id := range set.userIDs {
			if id == h.admin {
				t.Fatal("the importer must never be recorded as an assignee")
			}
		}
	}
}

func TestRerunClearsAssigneeWhenSourceBecomesUnmatched(t *testing.T) {
	h := newHarness(t)
	matched := uuid.New()
	h.store.members = append(h.store.members, importer.Member{UserID: matched, Email: "assignee@example.com"})
	h.source.issues = []importer.SourceIssue{
		{
			ExternalID: "A", Title: "Issue", Status: "To Do", ProjectExternalID: "10",
			Assignee: &importer.SourceUser{ExternalID: "u1", Email: "assignee@example.com"},
		},
	}
	h.previewAndStart(t)
	h.svc.process(context.Background(), h.job)

	first := h.issueByExternalID(t, "A")
	if first.AssigneeID == nil || *first.AssigneeID != matched {
		t.Fatalf("first run should assign %s, got %v", matched, first.AssigneeID)
	}

	// A later source revision has an assignee that no workspace member matches.
	// The changed email also changes the content hash, so the re-run updates.
	h.source.issues[0].Assignee = &importer.SourceUser{ExternalID: "ghost", Email: "ghost@example.com"}
	h.svc.process(context.Background(), h.job)

	after := h.issueByExternalID(t, "A")
	if after.AssigneeID != nil {
		t.Fatalf("previously matched assignee was not cleared: %s", *after.AssigneeID)
	}
	last := h.store.assigneeSets[len(h.store.assigneeSets)-1]
	if len(last.userIDs) != 0 {
		t.Fatalf("update did not clear assignees: %v", last.userIDs)
	}
}

func TestUnmatchedCommentAuthorIsAttributedToImporter(t *testing.T) {
	h := newHarness(t)
	h.source.issues = sampleIssues(h.admin)
	h.previewAndStart(t)
	h.svc.process(context.Background(), h.job)
	for _, comment := range h.store.comments {
		if strings.Contains(comment.Body, "hello") {
			if comment.UserID != h.admin {
				t.Fatalf("unmatched author should be the importer, got %s", comment.UserID)
			}
			if !strings.Contains(comment.Body, "Originally by Ghost") {
				t.Fatalf("original author name missing: %q", comment.Body)
			}
			return
		}
	}
	t.Fatal("comment not imported")
}

func TestOversizedAttachmentIsSkippedWithoutFailing(t *testing.T) {
	h := newHarness(t)
	issues := sampleIssues(h.admin)
	issues[0].Attachments[0].Size = 20 * 1024 * 1024
	h.source.issues = issues
	h.source.attachErr["a1"] = fmt.Errorf("%w: spec.pdf", jiraTooLarge{})
	h.previewAndStart(t)
	h.svc.process(context.Background(), h.job)
	assertFinish(t, h.repo, domain.ImportStatusCompleted)
	if h.store.assetCount != 0 {
		t.Fatal("oversized attachment was stored")
	}
	found := false
	for _, item := range h.repo.items {
		if item.ExternalID == "a1" && item.Outcome == domain.ImportOutcomeSkipped {
			found = true
		}
	}
	if !found {
		t.Fatal("oversized attachment not reported as skipped")
	}
	if h.repo.counters[h.job.ID].Skipped != 0 {
		t.Fatalf("skipped attachment must not count as a skipped issue, got %d", h.repo.counters[h.job.ID].Skipped)
	}
}

type jiraTooLarge struct{}

func (jiraTooLarge) Error() string { return "attachment exceeds the size limit" }

func TestCancellationWipesCredentials(t *testing.T) {
	h := newHarness(t)
	issues := make([]importer.SourceIssue, 12)
	for i := range issues {
		issues[i] = importer.SourceIssue{ExternalID: fmt.Sprint(i), Title: "x", Status: "To Do", ProjectExternalID: "10"}
	}
	h.source.issues = issues
	h.previewAndStart(t)
	h.repo.cancel = true
	h.svc.process(context.Background(), h.job)
	assertFinish(t, h.repo, domain.ImportStatusCancelled)
	if h.repo.wipes == 0 {
		t.Fatal("credentials not wiped on cancellation")
	}
}

func TestFailureWipesCredentials(t *testing.T) {
	h := newHarness(t)
	h.previewAndStart(t)
	failing := &fakeSourceWithError{}
	factory := func(context.Context, json.RawMessage, json.RawMessage) (importer.Source, error) { return failing, nil }
	svc := NewImportService(h.repo, h.store, factory, cryptoKeyForTest(), 1024)
	svc.process(context.Background(), h.job)
	assertFinish(t, h.repo, domain.ImportStatusFailed)
	if h.repo.wipes == 0 {
		t.Fatal("credentials not wiped on failure")
	}
}

type fakeSourceWithError struct{}

func (*fakeSourceWithError) Preview(context.Context) (*importer.SourcePreview, error) {
	return nil, nil
}
func (*fakeSourceWithError) Fetch(context.Context, importer.EmitFunc) error {
	return errors.New("source unavailable")
}
func (*fakeSourceWithError) OpenAttachment(context.Context, importer.SourceAttachment) (io.ReadCloser, error) {
	return nil, errors.New("unavailable")
}

func TestJobIsScopedToItsWorkspace(t *testing.T) {
	h := newHarness(t)
	other := uuid.New()
	if _, err := h.svc.Get(context.Background(), other, h.job.ID); err == nil {
		t.Fatal("another workspace could read the job")
	}
	if err := h.svc.Cancel(context.Background(), other, h.job.ID); err == nil {
		t.Fatal("another workspace could cancel the job")
	}
	if _, err := h.svc.ListItems(context.Background(), other, h.job.ID, 10, 0); err != nil {
		t.Fatal("ListItems must be workspace scoped and return an empty list, not leak an error")
	}
}

func TestThousandIssueRunStaysIdempotent(t *testing.T) {
	h := newHarness(t)
	issues := make([]importer.SourceIssue, 1000)
	for i := range issues {
		issues[i] = importer.SourceIssue{
			ExternalID: fmt.Sprint(i), ExternalKey: fmt.Sprintf("PROJ-%d", i), Title: "Issue",
			Status: "To Do", Priority: "High", ProjectExternalID: "10",
			Comments: []importer.SourceComment{{ExternalID: fmt.Sprintf("c%d", i), Body: "comment"}},
		}
	}
	h.source.issues = issues
	h.previewAndStart(t)
	h.svc.process(context.Background(), h.job)
	if len(h.store.issues) != 1000 || len(h.store.comments) != 1000 {
		t.Fatalf("expected 1000 issues and comments, got %d/%d", len(h.store.issues), len(h.store.comments))
	}
	before := h.repo.counters[h.job.ID].Created
	h.svc.process(context.Background(), h.job)
	if h.repo.counters[h.job.ID].Created != before || len(h.store.issues) != 1000 {
		t.Fatal("second run created duplicates")
	}
	assertFinish(t, h.repo, domain.ImportStatusCompleted)
}

func assertFinish(t *testing.T, repo *fakeImportRepo, want string) {
	t.Helper()
	if len(repo.finishCall) == 0 || repo.finishCall[len(repo.finishCall)-1].status != want {
		t.Fatalf("expected final status %s, got %+v", want, repo.finishCall)
	}
}
