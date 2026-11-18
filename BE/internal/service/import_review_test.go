package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/importer"
	"github.com/kuayle/kuayle-backend/internal/importer/jira"
	"github.com/kuayle/kuayle-backend/internal/repository"
)

// A ~1,000-issue import can report more failures than one page holds. Every
// page size the API accepts must page through the whole report; the fake
// repository applies the real repository's limit clamp.
func TestItemReportPagesThroughLargeReports(t *testing.T) {
	h := newHarness(t)
	for i := 0; i < 1000; i++ {
		h.repo.items = append(h.repo.items, domain.ImportJobItem{
			JobID: h.job.ID, WorkspaceID: h.ws, ExternalType: domain.ImportExternalIssue,
			ExternalID: fmt.Sprint(i), ExternalKey: fmt.Sprintf("PROJ-%d", i), Outcome: domain.ImportOutcomeFailed,
		})
	}
	for _, limit := range []int{50, 200, 201, 499, 500} {
		seen, offset := 0, 0
		for pages := 0; ; pages++ {
			items, hasMore, err := h.svc.ListItems(context.Background(), h.ws, h.job.ID, limit, offset)
			if err != nil {
				t.Fatal(err)
			}
			if len(items) != min(limit, 1000-offset) {
				t.Fatalf("limit %d offset %d: got %d items", limit, offset, len(items))
			}
			if items[0].ExternalKey != fmt.Sprintf("PROJ-%d", offset) {
				t.Fatalf("limit %d offset %d: page starts at %s", limit, offset, items[0].ExternalKey)
			}
			seen += len(items)
			offset += len(items)
			if !hasMore {
				break
			}
			if pages > 1000 {
				t.Fatal("paging never ended")
			}
		}
		if seen != 1000 {
			t.Fatalf("limit %d: paged through %d of 1000 items", limit, seen)
		}
	}
	items, hasMore, err := h.svc.ListItems(context.Background(), h.ws, h.job.ID, 9999, 0)
	if err != nil || len(items) != repository.ImportItemsPageMax || !hasMore {
		t.Fatalf("an oversized limit must clamp to a full page with more to come: %d items, more=%v, %v", len(items), hasMore, err)
	}
}

// The preview is stored in the API's own JSON shape, and saving the mapping
// recomputes which source values are still unmapped.
func TestSavingTheMappingRefreshesMappingGaps(t *testing.T) {
	h := newHarness(t)
	h.source.preview = &importer.SourcePreview{
		Counts:   map[string]int{"issues": 2},
		Statuses: []string{"To Do", "Done"},
		Users:    []importer.SourceUser{{ExternalID: "acc-9", DisplayName: "Hidden Email"}},
		Projects: []importer.SourceProject{{ExternalID: "10", Name: "Web", Key: "WEB"}, {ExternalID: "20", Name: "Ops", Key: "OPS"}},
	}
	preview, err := h.svc.Preview(context.Background(), h.ws, h.job.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"project: Ops (OPS)", "status: Done", "user: Hidden Email"} {
		if !containsString(preview.MappingGaps, want) {
			t.Fatalf("preview gaps %v miss %q", preview.MappingGaps, want)
		}
	}

	var stored map[string]json.RawMessage
	if err := json.Unmarshal(h.repo.jobs[h.job.ID].Preview, &stored); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"counts", "statuses", "users", "projects", "unmapped_fields", "mapping_gaps"} {
		if _, ok := stored[key]; !ok {
			t.Fatalf("stored preview lacks %q, the key the UI reads: %s", key, h.repo.jobs[h.job.ID].Preview)
		}
	}

	team, status := h.store.teamID, h.store.statusID
	mapping := importer.NewMapping()
	mapping.Projects["10"] = importer.ProjectTarget{TeamID: team}
	mapping.Projects["20"] = importer.ProjectTarget{TeamID: team}
	mapping.Statuses[team.String()+":to do"] = importer.StatusTarget{TeamID: team, StatusID: status}
	mapping.Statuses[team.String()+":done"] = importer.StatusTarget{TeamID: team, StatusID: status}
	mapping.Users["acc-9"] = importer.UserTarget{UserID: &h.admin}
	raw, _ := json.Marshal(mapping)
	if err := h.svc.UpdateMapping(context.Background(), h.ws, h.job.ID, raw); err != nil {
		t.Fatal(err)
	}
	var refreshed importer.SourcePreview
	if err := json.Unmarshal(h.repo.jobs[h.job.ID].Preview, &refreshed); err != nil {
		t.Fatal(err)
	}
	if len(refreshed.MappingGaps) != 0 {
		t.Fatalf("a complete mapping should leave no gaps, got %v", refreshed.MappingGaps)
	}
	if refreshed.Counts["issues"] != 2 || len(refreshed.Projects) != 2 {
		t.Fatalf("refreshing the gaps lost the preview: %+v", refreshed)
	}
}

// A job that already started cannot be previewed again, and the admin learns
// that before the whole source is read. A previewed job can be re-previewed.
func TestPreviewChecksEditabilityBeforeReadingTheSource(t *testing.T) {
	h := newHarness(t)
	if _, err := h.svc.Preview(context.Background(), h.ws, h.job.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.Preview(context.Background(), h.ws, h.job.ID); err != nil {
		t.Fatalf("a previewed job must accept a new preview: %v", err)
	}
	if h.source.previewCall != 2 {
		t.Fatalf("expected two source previews, got %d", h.source.previewCall)
	}
	for _, status := range []string{domain.ImportStatusQueued, domain.ImportStatusRunning, domain.ImportStatusCompleted, domain.ImportStatusCancelled} {
		h.repo.jobs[h.job.ID].Status = status
		if _, err := h.svc.Preview(context.Background(), h.ws, h.job.ID); !errors.Is(err, ErrImportNotEditable) {
			t.Fatalf("%s job: expected ErrImportNotEditable, got %v", status, err)
		}
	}
	if h.source.previewCall != 2 {
		t.Fatalf("the source was read for a job that cannot be previewed: %d calls", h.source.previewCall)
	}
}

type failingPreviewSource struct {
	fakeSource
	err error
}

func (f *failingPreviewSource) Preview(context.Context) (*importer.SourcePreview, error) {
	return nil, f.err
}

func TestPreviewSourceFailureIsASafeSourceError(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"rejected credentials", fmt.Errorf("search: %w", &jira.StatusError{Status: 401}), "rejected the credentials (HTTP 401)"},
		{"unknown failure", errors.New("dial tcp 10.0.0.1:443: token=secret"), genericSourceFailure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			source := &failingPreviewSource{err: tc.err}
			h.svc.newSource = func(context.Context, json.RawMessage, json.RawMessage) (importer.Source, error) { return source, nil }
			_, err := h.svc.Preview(context.Background(), h.ws, h.job.ID)
			var sourceErr *ImportSourceError
			if !errors.As(err, &sourceErr) {
				t.Fatalf("expected an ImportSourceError, got %T %v", err, err)
			}
			if !strings.Contains(sourceErr.Message, tc.want) || strings.Contains(sourceErr.Message, "secret") {
				t.Fatalf("unexpected message %q", sourceErr.Message)
			}
			if h.repo.jobs[h.job.ID].Status != domain.ImportStatusDraft {
				t.Fatal("a failed preview must leave the job a draft")
			}
		})
	}
}

func TestFetchFailureRecordsASafeJobError(t *testing.T) {
	h := newHarness(t)
	h.previewAndStart(t)
	failing := &failingFetchSource{err: fmt.Errorf("search: %w", &jira.StatusError{Status: 403})}
	h.svc.newSource = func(context.Context, json.RawMessage, json.RawMessage) (importer.Source, error) { return failing, nil }
	h.svc.process(context.Background(), h.job)
	assertFinish(t, h.repo, domain.ImportStatusFailed)
	last := h.repo.finishCall[len(h.repo.finishCall)-1]
	if last.err == nil || !strings.Contains(*last.err, "rejected the credentials (HTTP 403)") {
		t.Fatalf("job error should explain the source failure, got %v", last.err)
	}
}

type failingFetchSource struct {
	fakeSource
	err error
}

func (f *failingFetchSource) Fetch(context.Context, importer.EmitFunc) error { return f.err }

// Report rows name the Jira key (PROJ-1) so the admin can find the item in
// Jira, also for comments and attachments of that issue.
func TestReportRowsUseTheSourceKey(t *testing.T) {
	h := newHarness(t)
	h.source.issues = []importer.SourceIssue{
		{ExternalID: "100", ExternalKey: "OPS-7", Title: "Unmapped", Status: "To Do", ProjectExternalID: "999"},
		{
			ExternalID: "101", ExternalKey: "PROJ-1", Title: "Mapped", Status: "To Do", ProjectExternalID: "10",
			Attachments: []importer.SourceAttachment{{ExternalID: "a9", Filename: "big.bin", Size: 5}},
		},
	}
	h.source.attachErr["a9"] = errors.New("attachment exceeds the size limit")
	h.previewAndStart(t)
	h.svc.process(context.Background(), h.job)

	byID := map[string]domain.ImportJobItem{}
	for _, item := range h.repo.items {
		byID[item.ExternalType+":"+item.ExternalID] = item
	}
	if got := byID["issue:100"]; got.ExternalKey != "OPS-7" {
		t.Fatalf("issue row key = %q, want OPS-7", got.ExternalKey)
	}
	if got := byID["attachment:a9"]; got.ExternalKey != "PROJ-1" || !strings.Contains(got.Reason, "big.bin") {
		t.Fatalf("attachment row should name its issue and file, got %+v", got)
	}
	if ref := h.repo.refs[refMapKey(h.ws, domain.ImportSourceJira, h.job.SourceSite, domain.ImportExternalIssue, "101")]; ref == nil || ref.ExternalKey != "PROJ-1" {
		t.Fatalf("the issue ref must keep the original key, got %+v", ref)
	}
}

// A parent-link problem is reported against the child under its own type and
// does not move the issue counters, which already counted the child.
func TestParentLinkFailureIsReportedAgainstTheChild(t *testing.T) {
	h := newHarness(t)
	h.source.issues = []importer.SourceIssue{
		{ExternalID: "child", ExternalKey: "PROJ-2", Title: "Child", Status: "To Do", ProjectExternalID: "10", ParentExternalID: "missing-parent"},
		{ExternalID: "other", ExternalKey: "PROJ-3", Title: "Other", Status: "To Do", ProjectExternalID: "10"},
	}
	h.previewAndStart(t)
	h.svc.process(context.Background(), h.job)

	counters := h.repo.counters[h.job.ID]
	if counters.Created != 2 || counters.Skipped != 0 || counters.Failed != 0 || counters.Processed != 2 {
		t.Fatalf("a parent-link problem moved the issue counters: %+v", counters)
	}
	if len(h.repo.items) != 1 {
		t.Fatalf("expected one report row, got %+v", h.repo.items)
	}
	row := h.repo.items[0]
	if row.ExternalType != domain.ImportReportParent || row.ExternalID != "child" || row.ExternalKey != "PROJ-2" || row.Outcome != domain.ImportOutcomeSkipped {
		t.Fatalf("parent row should name the child: %+v", row)
	}
}

// Imported descriptions are editor HTML: Markdown converted, attribution and
// reference rendered, and attachments as the editor's attachment nodes behind
// the access-checked asset route, never a storage or presigned URL.
func TestDescriptionIsEditorHTMLWithProtectedAttachmentLinks(t *testing.T) {
	h := newHarness(t)
	h.source.issues = []importer.SourceIssue{{
		ExternalID: "A", ExternalKey: "PROJ-1", Title: "Doc", Status: "To Do", ProjectExternalID: "10",
		Description: "> Imported from Jira [PROJ-1](https://jira.example.com/browse/PROJ-1)\n\nUse the <div> wrapper",
		Reporter:    &importer.SourceUser{ExternalID: "u9", DisplayName: "Ada <Admin>"},
		Attachments: []importer.SourceAttachment{{ExternalID: "a1", Filename: `spec "v2".pdf`, Size: 5}},
	}}
	h.previewAndStart(t)
	h.svc.process(context.Background(), h.job)

	issue := h.issueByExternalID(t, "A")
	body := *issue.Description
	var assetID uuid.UUID
	for id := range h.store.assets {
		assetID = id
	}
	for _, want := range []string{
		`<p><em>Originally by Ada &lt;Admin&gt;</em></p>`,
		`<blockquote><p>Imported from Jira <a href="https://jira.example.com/browse/PROJ-1"`,
		`Use the &lt;div&gt; wrapper`,
		`data-type="attachment"`,
		`class="editor-attachment"`,
		`href="/api/workspaces/acme/assets/` + assetID.String() + `?download=1"`,
		`data-filename="spec &#34;v2&#34;.pdf"`,
		`data-size="5"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("description misses %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "example.com/asset") || strings.Contains(body, "**") || strings.Contains(body, "<div>") {
		t.Fatalf("description kept raw Markdown, a raw tag or a storage URL:\n%s", body)
	}
}

// Changing the mapping and re-running applies the new mapping to issues whose
// source did not change.
func TestRerunAppliesAChangedMapping(t *testing.T) {
	h := newHarness(t)
	h.source.issues = []importer.SourceIssue{{ExternalID: "A", Title: "Same", Status: "To Do", Priority: "High", ProjectExternalID: "10"}}
	h.previewAndStart(t)
	h.svc.process(context.Background(), h.job)
	if got := h.issueByExternalID(t, "A").Priority; got != domain.IssuePriority(2) {
		t.Fatalf("first run priority = %d", got)
	}

	h.svc.process(context.Background(), h.job)
	if h.repo.counters[h.job.ID].Skipped != 1 {
		t.Fatalf("an unchanged issue with an unchanged mapping must be skipped: %+v", h.repo.counters[h.job.ID])
	}

	mapping, _ := decodeMapping(h.repo.jobs[h.job.ID].Mapping)
	mapping.Priorities["high"] = 1
	raw, _ := json.Marshal(mapping)
	h.repo.jobs[h.job.ID].Mapping = raw
	h.job.Mapping = raw
	h.svc.process(context.Background(), h.job)
	if got := h.issueByExternalID(t, "A").Priority; got != domain.IssuePriority(1) {
		t.Fatalf("re-run did not apply the new priority mapping, priority = %d", got)
	}
	if h.repo.counters[h.job.ID].Updated != 1 {
		t.Fatalf("the mapping change should update the issue: %+v", h.repo.counters[h.job.ID])
	}
}

// An issue moved to another team in Kuayle keeps team-scoped values that
// belong to its new team; the mapped team's status is never written onto it.
func TestRerunKeepsTeamScopedValuesOfAMovedIssue(t *testing.T) {
	h := newHarness(t)
	teamA, statusA := h.store.teamID, h.store.statusID
	teamB, statusB, mappedB := uuid.New(), uuid.New(), uuid.New()
	h.store.teams = []domain.Team{{ID: teamA, WorkspaceID: h.ws}, {ID: teamB, WorkspaceID: h.ws}}
	h.store.teamStatuses = map[uuid.UUID][]domain.TeamStatus{
		teamA: {{ID: statusA, TeamID: teamA, Category: domain.StatusCategoryUnstarted, IsDefault: true}},
		teamB: {
			{ID: statusB, TeamID: teamB, Category: domain.StatusCategoryStarted, IsDefault: true},
			{ID: mappedB, TeamID: teamB, Category: domain.StatusCategoryCompleted},
		},
	}
	h.source.issues = []importer.SourceIssue{{
		ExternalID: "A", Title: "Moves", Status: "To Do", ProjectExternalID: "10", Sprints: []importer.SourceSprint{{Name: "S1"}},
	}}
	h.previewAndStart(t)
	h.svc.process(context.Background(), h.job)

	moved := h.issueByExternalID(t, "A")
	moved.TeamID = teamB
	moved.StatusID = &statusB
	moved.CycleID = nil
	h.store.issues[moved.ID] = moved

	h.source.issues[0].Title = "Moves, edited"
	h.svc.process(context.Background(), h.job)
	after := h.issueByExternalID(t, "A")
	if after.Title != "Moves, edited" {
		t.Fatalf("the moved issue was not updated: %q", after.Title)
	}
	if after.StatusID == nil || *after.StatusID != statusB || after.CycleID != nil {
		t.Fatalf("a moved issue got team A values: status %v cycle %v", after.StatusID, after.CycleID)
	}

	// A status mapped for the issue's current team is applied.
	mapping, _ := decodeMapping(h.repo.jobs[h.job.ID].Mapping)
	mapping.Statuses[teamB.String()+":to do"] = importer.StatusTarget{TeamID: teamB, StatusID: mappedB}
	raw, _ := json.Marshal(mapping)
	h.job.Mapping = raw
	h.source.issues[0].Title = "Moves again"
	h.svc.process(context.Background(), h.job)
	if got := h.issueByExternalID(t, "A"); got.StatusID == nil || *got.StatusID != mappedB || got.Status != domain.IssueStatusDone {
		t.Fatalf("a status mapped for the current team should apply: %v %s", got.StatusID, got.Status)
	}
}

// The active sprint becomes the issue's cycle and keeps its state and dates.
func TestSprintStateAndCurrentSprintReachTheCycle(t *testing.T) {
	h := newHarness(t)
	start := time.Date(2024, 3, 1, 9, 0, 0, 0, time.UTC)
	h.source.issues = []importer.SourceIssue{{
		ExternalID: "A", Title: "Carried over", Status: "To Do", ProjectExternalID: "10",
		Sprints: []importer.SourceSprint{
			{Name: "Sprint 1", State: importer.SprintStateClosed},
			{Name: "Sprint 2", State: importer.SprintStateActive, StartDate: &start},
		},
	}}
	h.previewAndStart(t)
	h.svc.process(context.Background(), h.job)

	if len(h.store.cycleSprints) != 1 || h.store.cycleSprints[0].Name != "Sprint 2" ||
		h.store.cycleSprints[0].State != importer.SprintStateActive || h.store.cycleSprints[0].StartDate == nil {
		t.Fatalf("the active sprint should become the cycle with its state and dates: %+v", h.store.cycleSprints)
	}
	if got := cycleStatus(importer.SprintStateActive); got != domain.CycleStatusActive {
		t.Fatalf("active sprint maps to %s", got)
	}
	if got := cycleStatus(importer.SprintStateFuture); got != domain.CycleStatusUpcoming {
		t.Fatalf("future sprint maps to %s", got)
	}
	if got := cycleStatus(""); got != domain.CycleStatusCompleted {
		t.Fatalf("unknown sprint state maps to %s", got)
	}
}

// The admin role is checked again when the queued job runs: a creator who was
// demoted (or removed) in the meantime cannot write through it.
func TestDemotedCreatorCannotRunAQueuedImport(t *testing.T) {
	for _, role := range []string{domain.RoleMember, domain.RoleGuest, ""} {
		h := newHarness(t)
		h.source.issues = sampleIssues(h.admin)
		h.previewAndStart(t)
		h.store.importerRole = role
		if role == "" {
			h.store.importerRole = "removed"
		}
		h.svc.process(context.Background(), h.job)
		assertFinish(t, h.repo, domain.ImportStatusFailed)
		if len(h.store.issues) != 0 || h.source.fetchCall != 0 {
			t.Fatalf("role %q: a non-admin's job wrote %d issues (fetches %d)", role, len(h.store.issues), h.source.fetchCall)
		}
		if h.repo.wipes == 0 {
			t.Fatalf("role %q: credentials were not wiped", role)
		}
	}
}

func TestHistorySummaryIsUpdatedInPlace(t *testing.T) {
	h := newHarness(t)
	h.source.issues = []importer.SourceIssue{{
		ExternalID: "A", Title: "History", Status: "To Do", ProjectExternalID: "10",
		HistoryMarkdown: "## History\n- 2024-01-01 Jane changed status",
	}}
	h.previewAndStart(t)
	h.svc.process(context.Background(), h.job)
	if len(h.store.comments) != 1 || !strings.Contains(h.store.comments[0].Body, "<h2>History</h2>") {
		t.Fatalf("history summary not imported as HTML: %+v", h.store.comments)
	}

	h.source.issues[0].HistoryMarkdown += "\n- 2024-01-02 Jane changed priority"
	h.svc.process(context.Background(), h.job)
	if len(h.store.comments) != 1 || !strings.Contains(h.store.comments[0].Body, "changed priority") {
		t.Fatalf("history summary was duplicated or not updated: %+v", h.store.comments)
	}
}
