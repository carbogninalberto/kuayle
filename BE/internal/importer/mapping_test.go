package importer

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/kuayle/kuayle-backend/internal/domain"
)

func TestResolveUserByEmailCaseInsensitive(t *testing.T) {
	m := NewMapping()
	importer, member := uuid.New(), uuid.New()
	members := []Member{{UserID: member, Email: "Jane.Doe@Example.com"}}
	got, prefix := m.ResolveUser(&SourceUser{Email: "jane.doe@example.COM", DisplayName: "Jane"}, members, importer)
	if got != member {
		t.Fatalf("expected member %s, got %s", member, got)
	}
	if prefix != "" {
		t.Fatalf("matched user should not need an attribution prefix, got %q", prefix)
	}
}

func TestResolveUserExplicitOverride(t *testing.T) {
	m := NewMapping()
	importer, target, byEmail := uuid.New(), uuid.New(), uuid.New()
	m.Users["ext-42"] = UserTarget{UserID: &target}
	m.Users["jane@example.com"] = UserTarget{UserID: &byEmail}
	members := []Member{{UserID: byEmail, Email: "jane@example.com"}}
	got, prefix := m.ResolveUser(&SourceUser{ExternalID: "ext-42", Email: "jane@example.com"}, members, importer)
	if got != target || prefix != "" {
		t.Fatalf("explicit mapping must win: got %s prefix %q", got, prefix)
	}
}

func TestResolveUserUnmatchedAttributesImporterWithName(t *testing.T) {
	m := NewMapping()
	importer := uuid.New()
	got, prefix := m.ResolveUser(&SourceUser{DisplayName: "Original Author", Email: "hidden@example.com"}, nil, importer)
	if got != importer {
		t.Fatalf("expected importer, got %s", got)
	}
	if prefix != "*Originally by Original Author*" {
		t.Fatalf("attribution prefix missing author name: %q", prefix)
	}
}

func TestResolveUserPlaceholderChoice(t *testing.T) {
	m := NewMapping()
	importer := uuid.New()
	m.Users["someone@example.com"] = UserTarget{}
	got, prefix := m.ResolveUser(&SourceUser{Email: "Someone@Example.com", DisplayName: "Someone"}, nil, importer)
	if got != importer || prefix != "*Originally by Someone*" {
		t.Fatalf("placeholder must be treated as unmatched: got %s prefix %q", got, prefix)
	}
}

func TestUnmappedStatusAndPriorityAreReported(t *testing.T) {
	m := NewMapping()
	preview := &SourcePreview{Statuses: []string{"In Progress", "Done"}, Priorities: []string{"Highest"}}
	m.Statuses["done"] = StatusTarget{}
	unmapped := m.UnmappedFor(preview)
	if len(unmapped) != 2 {
		t.Fatalf("expected two unmapped values, got %v", unmapped)
	}
	if unmapped[0] != "priority: Highest" || unmapped[1] != "status: In Progress" {
		t.Fatalf("unexpected unmapped report: %v", unmapped)
	}
}

func TestExternalKeysAreStableAndScoped(t *testing.T) {
	a := ExternalKey("jira", "cloud", "issue", "100")
	if a != ExternalKey("jira", "cloud", "issue", "100") {
		t.Fatal("key must be stable")
	}
	if a == ExternalKey("jira", "server", "issue", "100") {
		t.Fatal("key must differ by site")
	}
	if a == ExternalKey("jira", "cloud", "comment", "100") {
		t.Fatal("key must differ by type")
	}
	if a == ExternalKey("linear", "cloud", "issue", "100") {
		t.Fatal("key must differ by source")
	}
}

func TestContentHashChangesWithFields(t *testing.T) {
	issue := SourceIssue{ExternalID: "1", Title: "Original", Status: "Open", CreatedAt: time.Unix(100, 0)}
	first := ContentHash(issue)
	if first != ContentHash(issue) {
		t.Fatal("hash must be stable")
	}
	issue.Title = "Changed"
	if first == ContentHash(issue) {
		t.Fatal("hash must detect a change")
	}
}

func TestValidateRejectsInvalidTargets(t *testing.T) {
	teamID, statusID, memberID := uuid.New(), uuid.New(), uuid.New()
	valid := ValidationContext{
		Teams:        map[uuid.UUID]domain.Team{teamID: {ID: teamID, WorkspaceID: uuid.New()}},
		TeamStatuses: map[uuid.UUID][]domain.TeamStatus{teamID: {{ID: statusID, TeamID: teamID}}},
		Members:      map[uuid.UUID]bool{memberID: true},
	}
	m := NewMapping()
	m.Projects["p"] = ProjectTarget{TeamID: teamID}
	m.Statuses["s"] = StatusTarget{TeamID: teamID, StatusID: statusID}
	m.Users["u"] = UserTarget{UserID: &memberID}
	if err := m.Validate(valid); err != nil {
		t.Fatalf("valid mapping rejected: %v", err)
	}

	m.Statuses["s"] = StatusTarget{TeamID: teamID, StatusID: uuid.New()}
	if err := m.Validate(valid); err == nil {
		t.Fatal("foreign status must be rejected")
	}
	m.Statuses["s"] = StatusTarget{TeamID: teamID, StatusID: statusID}
	m.Users["u"] = UserTarget{UserID: ptr(uuid.New())}
	if err := m.Validate(valid); err == nil {
		t.Fatal("non-member must be rejected")
	}
}

func ptr[T any](v T) *T { return &v }
