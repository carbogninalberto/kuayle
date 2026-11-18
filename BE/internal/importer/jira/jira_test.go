package jira

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/importer"
)

const testToken = "SECRET-TOKEN-DO-NOT-LEAK"

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func writeFixture(t *testing.T, w http.ResponseWriter, name string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(fixture(t, name))
}

func testClient(t *testing.T, srv *httptest.Server, cloud bool) *Client {
	t.Helper()
	c, err := NewClient(Config{
		BaseURL:             srv.URL,
		Cloud:               cloud,
		Email:               "importer@example.com",
		Token:               testToken,
		AllowPrivateNetwork: true,
		MaxAttachmentSize:   10 * 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCloudFetchPaginationAndTransform(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/rest/api/3/field":
			writeFixture(t, w, "fields.json")
		case r.URL.Path == "/rest/api/3/search/jql":
			body, _ := io.ReadAll(r.Body)
			var req map[string]any
			_ = json.Unmarshal(body, &req)
			if req["nextPageToken"] == "page2" {
				writeFixture(t, w, "cloud_search_page2.json")
			} else {
				writeFixture(t, w, "cloud_search_page1.json")
			}
		case strings.HasSuffix(r.URL.Path, "/comment"):
			_, _ = w.Write([]byte(`{"comments":[],"total":0}`))
		case strings.HasSuffix(r.URL.Path, "/changelog"):
			_, _ = w.Write([]byte(`{"values":[],"total":0}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	var got []importer.SourceIssue
	if err := testClient(t, srv, true).Fetch(context.Background(), func(issue importer.SourceIssue) error {
		got = append(got, issue)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 issues across pages, got %d", len(got))
	}
	first := got[0]
	if first.ExternalKey != "PROJ-1" || first.Title != "First issue" {
		t.Fatalf("unexpected first issue: %+v", first)
	}
	if first.ParentExternalID != "" {
		t.Fatalf("parent issue should have no parent: %q", first.ParentExternalID)
	}
	if !strings.Contains(first.Description, "Imported from Jira [PROJ-1]") {
		t.Fatalf("reference line missing: %q", first.Description)
	}
	if !strings.Contains(first.Description, "**world**") || !strings.Contains(first.Description, "[link](https://example.com)") {
		t.Fatalf("ADF marks not converted: %q", first.Description)
	}
	if !strings.Contains(first.Description, "unmapped node unknownWidget") {
		t.Fatalf("unknown ADF node not noted: %q", first.Description)
	}
	if len(first.Labels) != 2 || first.Labels[0] != "API" || first.Labels[1] != "backend" {
		t.Fatalf("components and labels not merged: %v", first.Labels)
	}
	if len(first.Attachments) != 1 || first.Attachments[0].Filename != "spec.pdf" {
		t.Fatalf("attachment not transformed: %+v", first.Attachments)
	}
	if len(first.Sprints) != 1 || first.Sprints[0].Name != "Sprint 5" {
		t.Fatalf("sprint not transformed: %v", first.Sprints)
	}
	if len(first.Relations) != 2 {
		t.Fatalf("expected two relations, got %+v", first.Relations)
	}
	blocking, blocked := first.Relations[0], first.Relations[1]
	if blocking.Type != "blocking" || blocking.TargetExternalID != "10002" {
		t.Fatalf("outward Blocks must map to blocking: %+v", blocking)
	}
	if blocked.Type != "blocked_by" || blocked.TargetExternalID != "10003" {
		t.Fatalf("inward Blocks must map to blocked_by: %+v", blocked)
	}
	if first.Reporter == nil || first.Reporter.Email != "jane@example.com" {
		t.Fatalf("reporter email not transformed: %+v", first.Reporter)
	}
	if first.Assignee == nil || first.Assignee.Email != "" || first.Assignee.DisplayName != "No Email" {
		t.Fatalf("email-less assignee not preserved: %+v", first.Assignee)
	}

	sub := got[1]
	if sub.ParentExternalID != "10001" {
		t.Fatalf("sub-task parent not mapped: %q", sub.ParentExternalID)
	}
	if sub.Reporter == nil || sub.Reporter.Email != "" {
		t.Fatalf("user without email not handled: %+v", sub.Reporter)
	}
}

func TestCloudPreviewCountsAndUnmapped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/rest/api/3/field":
			writeFixture(t, w, "fields.json")
		case r.URL.Path == "/rest/api/3/search/jql":
			body, _ := io.ReadAll(r.Body)
			var req map[string]any
			_ = json.Unmarshal(body, &req)
			if req["nextPageToken"] == "page2" {
				writeFixture(t, w, "cloud_search_page2.json")
			} else {
				writeFixture(t, w, "cloud_search_page1.json")
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	preview, err := testClient(t, srv, true).Preview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	checks := map[string]int{"issues": 2, "sub_issues": 1, "attachments": 1, "links": 2}
	for key, want := range checks {
		if preview.Counts[key] != want {
			t.Fatalf("count %s = %d, want %d", key, preview.Counts[key], want)
		}
	}
	if len(preview.Statuses) != 2 || len(preview.Priorities) != 2 {
		t.Fatalf("preview values not collected: %+v", preview)
	}
	joined := strings.Join(preview.UnmappedFields, "\n")
	if !strings.Contains(joined, "user without email") {
		t.Fatalf("email-less user not reported: %v", preview.UnmappedFields)
	}
}

func TestServerDCSearchAndEpicLink(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/rest/api/2/field":
			writeFixture(t, w, "fields.json")
		case r.URL.Path == "/rest/api/2/search":
			if r.URL.Query().Get("expand") != "changelog" {
				t.Errorf("expected changelog expansion, got %q", r.URL.Query().Get("expand"))
			}
			writeFixture(t, w, "server_search.json")
		case strings.HasSuffix(r.URL.Path, "/comment"):
			_, _ = w.Write([]byte(`{"comments":[],"total":0}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	var got []importer.SourceIssue
	if err := testClient(t, srv, false).Fetch(context.Background(), func(issue importer.SourceIssue) error {
		got = append(got, issue)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected one DC issue, got %d", len(got))
	}
	if got[0].EpicExternalID != "DC-9" {
		t.Fatalf("epic link not transformed: %q", got[0].EpicExternalID)
	}
	if !strings.Contains(got[0].HistoryMarkdown, "changed status") {
		t.Fatalf("history summary missing: %q", got[0].HistoryMarkdown)
	}
	if !strings.Contains(got[0].Description, "# Heading") {
		t.Fatalf("wiki heading not converted: %q", got[0].Description)
	}
}

func TestADFToMarkdown(t *testing.T) {
	raw := json.RawMessage(`{"type":"doc","content":[
		{"type":"heading","attrs":{"level":1},"content":[{"type":"text","text":"Title"}]},
		{"type":"paragraph","content":[{"type":"text","text":"a"},{"type":"text","text":"b","marks":[{"type":"em"}]},{"type":"hardBreak"},{"type":"text","text":"c"}]},
		{"type":"codeBlock","attrs":{"language":"go"},"content":[{"type":"text","text":"x := 1"}]},
		{"type":"blockquote","content":[{"type":"paragraph","content":[{"type":"text","text":"quoted"}]}]},
		{"type":"orderedList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"first"}]}]}]},
		{"type":"paragraph","content":[{"type":"mention","attrs":{"text":"@jane"}}]},
		{"type":"table","content":[
			{"type":"tableRow","content":[{"type":"tableHeader","content":[{"type":"text","text":"A"}]},{"type":"tableHeader","content":[{"type":"text","text":"B"}]}]},
			{"type":"tableRow","content":[{"type":"tableCell","content":[{"type":"text","text":"1"}]},{"type":"tableCell","content":[{"type":"text","text":"2"}]}]}
		]}
	]}`)
	markdown, notes := DescriptionToMarkdown(raw)
	for _, want := range []string{"# Title", "*b*", "```go", "> quoted", "1. first", "@jane", "| A | B |", "| 1 | 2 |"} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("markdown missing %q: %s", want, markdown)
		}
	}
	if len(notes) != 0 {
		t.Fatalf("unexpected unmapped notes: %v", notes)
	}
}

func TestRelationDirection(t *testing.T) {
	if relationType("Blocks", false) != "blocking" || relationType("Blocks", true) != "blocked_by" {
		t.Fatal("Blocks direction mapping is wrong")
	}
	if relationType("Duplicate", false) != "duplicate" || relationType("Relates", false) != "related" {
		t.Fatal("other link types are wrong")
	}
}

func TestRetryAfterAndNoTokenInErrors(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := testClient(t, srv, true)
	var out map[string]bool
	if err := c.getJSON(context.Background(), srv.URL+"/ok", &out); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || !out["ok"] {
		t.Fatalf("expected retry then success, calls=%d out=%v", calls.Load(), out)
	}

	fail := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer fail.Close()
	err := testClient(t, fail, true).getJSON(context.Background(), fail.URL+"/bad", &out)
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), testToken) || strings.Contains(err.Error(), "Authorization") {
		t.Fatalf("error leaked credentials: %v", err)
	}
}

func TestForeignAttachmentIsRefusedWithoutSendingCredentials(t *testing.T) {
	var foreignRequests atomic.Int32
	var foreignAuth string
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		foreignRequests.Add(1)
		foreignAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer foreign.Close()

	jira := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer jira.Close()

	_, err := testClient(t, jira, true).OpenAttachment(context.Background(), importer.SourceAttachment{
		ExternalID: "a1", Filename: "spec.pdf", Size: 5, URL: foreign.URL + "/attachment",
	})
	if !errors.Is(err, ErrAttachmentHostMismatch) {
		t.Fatalf("expected ErrAttachmentHostMismatch, got %v", err)
	}
	if foreignRequests.Load() != 0 || foreignAuth != "" {
		t.Fatalf("foreign host was contacted with credentials: requests=%d auth=%q", foreignRequests.Load(), foreignAuth)
	}
}

func TestReferenceLineUsesBrowseURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/rest/api/3/field":
			writeFixture(t, w, "fields.json")
		case r.URL.Path == "/rest/api/3/search/jql":
			body, _ := io.ReadAll(r.Body)
			var req map[string]any
			_ = json.Unmarshal(body, &req)
			if req["nextPageToken"] == "page2" {
				writeFixture(t, w, "cloud_search_page2.json")
			} else {
				writeFixture(t, w, "cloud_search_page1.json")
			}
		case strings.HasSuffix(r.URL.Path, "/comment"):
			_, _ = w.Write([]byte(`{"comments":[],"total":0}`))
		case strings.HasSuffix(r.URL.Path, "/changelog"):
			_, _ = w.Write([]byte(`{"values":[],"total":0}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	var got []importer.SourceIssue
	if err := testClient(t, srv, true).Fetch(context.Background(), func(issue importer.SourceIssue) error {
		got = append(got, issue)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	want := srv.URL + "/browse/PROJ-1"
	if got[0].URL != want {
		t.Fatalf("source URL = %q, want %q", got[0].URL, want)
	}
	if !strings.Contains(got[0].Description, "]("+want+")") {
		t.Fatalf("reference line does not link the browse URL: %q", got[0].Description)
	}
}

func TestSiteFromBaseURL(t *testing.T) {
	site, err := SiteFromBaseURL("https://MySite.ATLASSIAN.net/rest")
	if err != nil || site != "mysite.atlassian.net" {
		t.Fatalf("expected lower-cased host, got %q, %v", site, err)
	}
	for _, bad := range []string{"", "not-a-url", "https://", "://missing-scheme"} {
		if _, err := SiteFromBaseURL(bad); err == nil {
			t.Fatalf("expected an error for %q", bad)
		}
	}
	client := testClient(t, httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})), true)
	if client.Site() != strings.ToLower(strings.TrimPrefix(client.baseURL, "http://")) {
		t.Fatalf("client site and base URL host disagree: %q vs %q", client.Site(), client.baseURL)
	}
}

func TestOversizedAttachmentIsFlagged(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("oversized attachment must not be downloaded")
	}))
	defer srv.Close()
	_, err := testClient(t, srv, true).OpenAttachment(context.Background(), importer.SourceAttachment{
		Filename: "huge.bin",
		Size:     10*1024 + 1,
		URL:      srv.URL + "/attachment",
	})
	if !errors.Is(err, ErrAttachmentTooLarge) {
		t.Fatalf("expected ErrAttachmentTooLarge, got %v", err)
	}
}

// Jira Cloud returns sprint objects, older Server/DC the Java toString of the
// sprint. Both keep state and dates so an active sprint is not imported closed.
func TestSprintFieldFormats(t *testing.T) {
	fields := jiraFields{Custom: map[string]json.RawMessage{"customfield_1": json.RawMessage(`[
		{"id": 7, "name": "Sprint 7", "state": "active", "startDate": "2024-03-01T09:00:00.000Z", "endDate": "2024-03-15T09:00:00.000Z"},
		"com.atlassian.greenhopper.service.sprint.Sprint@1f2e[id=3,rapidViewId=1,state=CLOSED,name=Sprint 3, hotfixes,startDate=2024-01-01T09:00:00.000Z,endDate=2024-01-14T09:00:00.000Z,completeDate=<null>,sequence=3]",
		"com.atlassian.greenhopper.service.sprint.Sprint@9a[id=9,rapidViewId=1,state=FUTURE,name=Next,startDate=<null>,endDate=<null>,sequence=9]",
		"Plain sprint",
		{"id": 8, "name": "sprint 7", "state": "closed"}
	]`)}}
	got := sprintValues(fields, "customfield_1")
	if len(got) != 4 {
		t.Fatalf("expected 4 distinct sprints, got %+v", got)
	}
	if got[0].Name != "Sprint 7" || got[0].State != importer.SprintStateActive || got[0].ExternalID != "7" || got[0].StartDate == nil || got[0].EndDate == nil {
		t.Fatalf("object sprint not parsed: %+v", got[0])
	}
	if got[1].Name != "Sprint 3, hotfixes" || got[1].State != importer.SprintStateClosed || got[1].ExternalID != "3" || got[1].StartDate == nil {
		t.Fatalf("legacy sprint not parsed: %+v", got[1])
	}
	if got[2].Name != "Next" || got[2].State != importer.SprintStateFuture || got[2].StartDate != nil {
		t.Fatalf("future legacy sprint not parsed: %+v", got[2])
	}
	if got[3].Name != "Plain sprint" || got[3].State != "" {
		t.Fatalf("plain sprint name not kept: %+v", got[3])
	}
}

func TestDescribeErrorIsSafeAndSpecific(t *testing.T) {
	unauthorized := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"errorMessages":["secret project names"]}`))
	}))
	defer unauthorized.Close()
	_, err := testClient(t, unauthorized, true).Preview(context.Background())
	var status *StatusError
	if !errors.As(err, &status) || status.Status != http.StatusUnauthorized {
		t.Fatalf("expected a 401 status error, got %v", err)
	}
	message := DescribeError(err)
	if !strings.Contains(message, "rejected the credentials") || !strings.Contains(message, "401") {
		t.Fatalf("unexpected message: %q", message)
	}
	for _, leaked := range []string{testToken, unauthorized.URL, "secret project names"} {
		if strings.Contains(message, leaked) {
			t.Fatalf("message leaked %q: %q", leaked, message)
		}
	}

	private, err := NewClient(Config{BaseURL: "https://127.0.0.1:1", Token: testToken})
	if err != nil {
		t.Fatal(err)
	}
	_, err = private.Preview(context.Background())
	if got := DescribeError(err); !strings.Contains(got, "private network") {
		t.Fatalf("a refused private address should say so, got %q (%v)", got, err)
	}

	if _, err := NewClient(Config{BaseURL: "http://jira.example.com", Token: testToken}); !errors.Is(err, ErrInvalidBaseURL) || !strings.Contains(DescribeError(err), "base URL is invalid") {
		t.Fatalf("plain http must be an invalid base URL without the private-network opt-in, got %v", err)
	}
	for _, bad := range []string{"https://user:pass@jira.example.com", "https://jira.example.com/?x=1", "ftp://jira.example.com"} {
		if _, err := SiteFromBaseURL(bad); !errors.Is(err, ErrInvalidBaseURL) {
			t.Fatalf("expected %q to be rejected, got %v", bad, err)
		}
	}
}
