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
	if len(first.SprintIDs) != 1 || first.SprintIDs[0] != "Sprint 5" {
		t.Fatalf("sprint not transformed: %v", first.SprintIDs)
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
