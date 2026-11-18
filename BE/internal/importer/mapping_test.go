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
	if prefix != "Original Author" {
		t.Fatalf("attribution prefix missing author name: %q", prefix)
	}
}

func TestResolveUserPlaceholderChoice(t *testing.T) {
	m := NewMapping()
	importer := uuid.New()
	m.Users["someone@example.com"] = UserTarget{}
	got, prefix := m.ResolveUser(&SourceUser{Email: "Someone@Example.com", DisplayName: "Someone"}, nil, importer)
	if got != importer || prefix != "Someone" {
		t.Fatalf("placeholder must be treated as unmatched: got %s prefix %q", got, prefix)
	}
}

func TestResolveAssigneeNeverFallsBackToImporter(t *testing.T) {
	m := NewMapping()
	member := uuid.New()
	members := []Member{{UserID: member, Email: "Jane.Doe@Example.com"}}

	if got := m.ResolveAssignee(nil, members); got != nil {
		t.Fatalf("nil assignee must be unassigned, got %s", got)
	}
	if got := m.ResolveAssignee(&SourceUser{Email: "ghost@example.com"}, members); got != nil {
		t.Fatalf("unmatched assignee must be unassigned, got %s", got)
	}
	m.Users["someone@example.com"] = UserTarget{}
	if got := m.ResolveAssignee(&SourceUser{Email: "Someone@Example.com"}, members); got != nil {
		t.Fatalf("explicit unassigned choice must be honoured, got %s", got)
	}
	if got := m.ResolveAssignee(&SourceUser{Email: "jane.doe@EXAMPLE.com"}, members); got == nil || *got != member {
		t.Fatalf("case-insensitive email match expected %s, got %v", member, got)
	}
	if got := m.ResolveAssignee(&SourceUser{ExternalID: "ext-1", Email: "jane.doe@example.com"}, members); got == nil || *got != member {
		t.Fatalf("explicit external-id mapping expected %s, got %v", member, got)
	}
	target := uuid.New()
	m.Users["ext-2"] = UserTarget{UserID: &target}
	if got := m.ResolveAssignee(&SourceUser{ExternalID: "ext-2"}, members); got == nil || *got != target {
		t.Fatalf("explicit user mapping expected %s, got %v", target, got)
	}
}

func TestUnmappedStatusAndPriorityAreReported(t *testing.T) {
	m := NewMapping()
	team := uuid.New()
	m.Projects["p"] = ProjectTarget{TeamID: team}
	preview := &SourcePreview{Statuses: []string{"In Progress", "Done"}, Priorities: []string{"Highest"}}
	m.Statuses[team.String()+":done"] = StatusTarget{TeamID: team}
	unmapped := m.UnmappedFor(preview, nil)
	if len(unmapped) != 2 {
		t.Fatalf("expected two unmapped values, got %v", unmapped)
	}
	if unmapped[0] != "priority: Highest" || unmapped[1] != "status: In Progress" {
		t.Fatalf("unexpected unmapped report: %v", unmapped)
	}
}

func TestResolveStatusPerTeam(t *testing.T) {
	teamA, teamB := uuid.New(), uuid.New()
	statusA, statusB := uuid.New(), uuid.New()
	m := NewMapping()
	m.Statuses[teamA.String()+":in progress"] = StatusTarget{TeamID: teamA, StatusID: statusA}
	m.Statuses[teamB.String()+":in progress"] = StatusTarget{TeamID: teamB, StatusID: statusB}

	if got, ok := m.ResolveStatus(teamA, "In Progress"); !ok || got.StatusID != statusA || got.TeamID != teamA {
		t.Fatalf("team A status not resolved: %+v ok=%v", got, ok)
	}
	if got, ok := m.ResolveStatus(teamB, "IN PROGRESS"); !ok || got.StatusID != statusB || got.TeamID != teamB {
		t.Fatalf("team B status not resolved: %+v ok=%v", got, ok)
	}
	// A key belonging to another team must never be resolved for this team,
	// even if the status names match.
	if _, ok := m.ResolveStatus(uuid.New(), "In Progress"); ok {
		t.Fatal("a status from another team was resolved")
	}
	// An unconfigured status is reported as unmapped.
	if _, ok := m.ResolveStatus(teamA, "Done"); ok {
		t.Fatal("an unconfigured status must not resolve")
	}
}

func TestUnmappedForReportsPerTeam(t *testing.T) {
	teamA, teamB := uuid.New(), uuid.New()
	m := NewMapping()
	m.Projects["p1"] = ProjectTarget{TeamID: teamA}
	m.Projects["p2"] = ProjectTarget{TeamID: teamB}
	m.Statuses[teamA.String()+":done"] = StatusTarget{TeamID: teamA}
	preview := &SourcePreview{Statuses: []string{"In Progress", "Done"}}
	unmapped := m.UnmappedFor(preview, nil)
	// "Done" is mapped for team A but not team B, so it is still reported
	// once; "In Progress" is unmapped for both. Deduplication keeps one entry.
	if len(unmapped) != 2 || unmapped[0] != "status: Done" || unmapped[1] != "status: In Progress" {
		t.Fatalf("unexpected per-team unmapped report: %v", unmapped)
	}

	// With no project mapped, every status is reported.
	empty := NewMapping()
	empty.Statuses[teamA.String()+":done"] = StatusTarget{TeamID: teamA}
	got := empty.UnmappedFor(preview, nil)
	if len(got) != 2 || got[0] != "status: Done" || got[1] != "status: In Progress" {
		t.Fatalf("no-project unmapped report should list every status: %v", got)
	}
}

func TestTargetHashChangesWhenTheMappingChanges(t *testing.T) {
	content := ContentHash(SourceIssue{ExternalID: "1", Title: "Same"})
	first := TargetHash(content, "team-a", "status-1", "2")
	if first != TargetHash(content, "team-a", "status-1", "2") {
		t.Fatal("target hash must be stable")
	}
	if first == TargetHash(content, "team-a", "status-2", "2") {
		t.Fatal("a different mapped status must change the hash")
	}
	if first == TargetHash(content, "team-a", "status-1", "3") {
		t.Fatal("a different mapped priority must change the hash")
	}
}

// Jira Cloud hides most emails. Users without one are keyed by their source id
// so the admin can still map them, and they never share an empty key.
func TestUsersWithoutEmailAreKeyedBySourceID(t *testing.T) {
	member := uuid.New()
	if got := UserKey(SourceUser{ExternalID: "acc-1", Email: " Jane@Example.com "}); got != "jane@example.com" {
		t.Fatalf("email key expected, got %q", got)
	}
	if got := UserKey(SourceUser{ExternalID: "acc-2"}); got != "acc-2" {
		t.Fatalf("source id key expected, got %q", got)
	}

	m := NewMapping()
	m.Users["acc-2"] = UserTarget{UserID: &member}
	if got := m.ResolveAssignee(&SourceUser{ExternalID: "acc-2"}, nil); got == nil || *got != member {
		t.Fatalf("a user without email must resolve by source id, got %v", got)
	}
	// A stray empty key must not capture every user without an email.
	m.Users[""] = UserTarget{UserID: &member}
	if got := m.ResolveAssignee(&SourceUser{ExternalID: "acc-3"}, nil); got != nil {
		t.Fatalf("an empty key matched another user: %v", got)
	}
	if err := m.Validate(ValidationContext{Members: map[uuid.UUID]bool{member: true}}); err == nil {
		t.Fatal("an empty user key must be rejected")
	}
}

func TestUnmappedForReportsProjectsAndMatchesMembers(t *testing.T) {
	team, member := uuid.New(), uuid.New()
	m := NewMapping()
	m.Projects["10"] = ProjectTarget{TeamID: team}
	m.Statuses[team.String()+":open"] = StatusTarget{TeamID: team}
	preview := &SourcePreview{
		Projects: []SourceProject{{ExternalID: "10", Name: "Web", Key: "WEB"}, {ExternalID: "20", Name: "Ops", Key: "OPS"}},
		Statuses: []string{"Open"},
		Users: []SourceUser{
			{ExternalID: "a", Email: "jane@example.com", DisplayName: "Jane"},
			{ExternalID: "b", DisplayName: "Hidden"},
		},
	}
	got := m.UnmappedFor(preview, []Member{{UserID: member, Email: "JANE@example.com"}})
	want := []string{"project: Ops (OPS)", "user: Hidden"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("unexpected gaps: %v, want %v", got, want)
	}

	m.Projects["20"] = ProjectTarget{TeamID: team}
	m.Users["b"] = UserTarget{UserID: &member}
	if got := m.UnmappedFor(preview, nil); len(got) != 1 || got[0] != "user: Jane" {
		t.Fatalf("after mapping only the unmatched email should remain without members: %v", got)
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
	statusKey := teamID.String() + ":s"
	m.Statuses[statusKey] = StatusTarget{TeamID: teamID, StatusID: statusID}
	m.Users["u"] = UserTarget{UserID: &memberID}
	if err := m.Validate(valid); err != nil {
		t.Fatalf("valid mapping rejected: %v", err)
	}

	m.Statuses[statusKey] = StatusTarget{TeamID: teamID, StatusID: uuid.New()}
	if err := m.Validate(valid); err == nil {
		t.Fatal("foreign status must be rejected")
	}
	m.Statuses[statusKey] = StatusTarget{TeamID: teamID, StatusID: statusID}
	foreignKey := uuid.New().String() + ":s"
	m.Statuses[foreignKey] = StatusTarget{TeamID: teamID, StatusID: statusID}
	if err := m.Validate(valid); err == nil {
		t.Fatal("a status key whose team prefix differs from the target team must be rejected")
	}
	delete(m.Statuses, foreignKey)
	m.Users["u"] = UserTarget{UserID: ptr(uuid.New())}
	if err := m.Validate(valid); err == nil {
		t.Fatal("non-member must be rejected")
	}
}

func TestCommentHashDetectsEditsAndContentHashTracksComments(t *testing.T) {
	comment := SourceComment{ExternalID: "c1", Body: "original", UpdatedAt: time.Unix(100, 0)}
	first := CommentHash(comment)
	if first != CommentHash(comment) {
		t.Fatal("comment hash must be stable")
	}
	issue := SourceIssue{ExternalID: "1", Comments: []SourceComment{comment}}
	issueHash := ContentHash(issue)
	comment.Body = "edited"
	if first == CommentHash(comment) {
		t.Fatal("comment hash must detect a body edit")
	}
	issue.Comments[0] = comment
	if issueHash == ContentHash(issue) {
		t.Fatal("issue hash must change when a comment is edited")
	}
}

func TestValidateChecksPriorityRange(t *testing.T) {
	m := NewMapping()
	m.Priorities["highest"] = 7
	if err := m.Validate(ValidationContext{}); err == nil {
		t.Fatal("priority 7 must be rejected")
	}
	m.Priorities["highest"] = -1
	if err := m.Validate(ValidationContext{}); err == nil {
		t.Fatal("priority -1 must be rejected")
	}
	for _, value := range []int{int(domain.PriorityNone), int(domain.PriorityLow)} {
		m.Priorities["highest"] = value
		if err := m.Validate(ValidationContext{}); err != nil {
			t.Fatalf("priority %d should be accepted: %v", value, err)
		}
	}
}

func ptr[T any](v T) *T { return &v }
