package importer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/kuayle/kuayle-backend/internal/domain"
)

const (
	EpicModeProject  = "project"
	EpicModeParent   = "parent"
	SprintModeCycle  = "cycle"
	SprintModeIgnore = "ignore"

	// UnassignedChoice is the explicit "unassigned / placeholder" option in
	// the mapping UI. It is treated as an unmatched user.
	UnassignedChoice = "unassigned"
)

// ProjectTarget is where issues from one source project are written.
type ProjectTarget struct {
	TeamID    uuid.UUID  `json:"team_id"`
	ProjectID *uuid.UUID `json:"project_id"`
}

// StatusTarget maps a source status name to a team status.
type StatusTarget struct {
	TeamID   uuid.UUID `json:"team_id"`
	StatusID uuid.UUID `json:"status_id"`
}

// UserTarget maps a source user to a workspace member. A nil UserID is the
// explicit unassigned/placeholder choice: an author falls back to the importer
// with the original name in the text, an assignee is left empty.
type UserTarget struct {
	UserID *uuid.UUID `json:"user_id"`
}

// Mapping is the admin-configured translation from source values to Kuayle
// entities. Project keys are source project ids; status keys are
// "<team uuid>:<lower-cased status name>"; user keys are the lower-cased source
// email, or the source user id when the source hides the email.
type Mapping struct {
	Projects   map[string]ProjectTarget `json:"projects"`
	Statuses   map[string]StatusTarget  `json:"statuses"`
	Users      map[string]UserTarget    `json:"users"`
	Priorities map[string]int           `json:"priorities"`
	EpicMode   string                   `json:"epic_mode"`
	SprintMode string                   `json:"sprint_mode"`
}

func NewMapping() Mapping {
	return Mapping{
		Projects:   map[string]ProjectTarget{},
		Statuses:   map[string]StatusTarget{},
		Users:      map[string]UserTarget{},
		Priorities: map[string]int{},
		EpicMode:   EpicModeProject,
		SprintMode: SprintModeCycle,
	}
}

// Member is a workspace member that a source user may be matched to.
type Member struct {
	UserID      uuid.UUID
	Email       string
	DisplayName string
}

// AttributionName is the name shown for a source user who could not be
// matched to a workspace member.
func AttributionName(user *SourceUser) string {
	if user == nil {
		return "Unknown"
	}
	if strings.TrimSpace(user.DisplayName) != "" {
		return strings.TrimSpace(user.DisplayName)
	}
	if strings.TrimSpace(user.Email) != "" {
		return strings.TrimSpace(user.Email)
	}
	if user.ExternalID != "" {
		return user.ExternalID
	}
	return "Unknown"
}

// UserKey is the mapping key for a source user: the lower-cased email, or the
// source id when the source hides the email (common on Jira Cloud). The UI
// uses the same rule, so every previewed user can be mapped.
func UserKey(user SourceUser) string {
	if email := strings.ToLower(strings.TrimSpace(user.Email)); email != "" {
		return email
	}
	return user.ExternalID
}

// resolveMember finds the member a source user maps to. explicit reports an
// admin choice for this user (including the placeholder, which returns nil).
// An empty email or id never matches, so users without one cannot collide on
// an empty key.
func (m Mapping) resolveMember(user *SourceUser, members []Member) (member *uuid.UUID, explicit bool) {
	if user == nil {
		return nil, false
	}
	email := strings.ToLower(strings.TrimSpace(user.Email))
	var byID, byEmail UserTarget
	hasID, hasEmail := false, false
	if user.ExternalID != "" {
		byID, hasID = m.Users[user.ExternalID]
	}
	if email != "" {
		byEmail, hasEmail = m.Users[email]
	}
	if hasID && byID.UserID != nil {
		return byID.UserID, true
	}
	if hasEmail && byEmail.UserID != nil {
		return byEmail.UserID, true
	}
	// An explicit placeholder choice wins over an automatic email match.
	if hasID || hasEmail {
		return nil, true
	}
	if email != "" && email != UnassignedChoice {
		for _, candidate := range members {
			if strings.EqualFold(strings.TrimSpace(candidate.Email), email) {
				id := candidate.UserID
				return &id, false
			}
		}
	}
	return nil, false
}

// ResolveAssignee returns the Kuayle user that should be assigned an imported
// issue, or nil when the issue should have no assignee. Unlike ResolveUser it
// never falls back to the importer: a missing, unmatched or explicitly
// unassigned source user leaves the issue unassigned. Assignees and authors
// resolve differently on purpose.
func (m Mapping) ResolveAssignee(user *SourceUser, members []Member) *uuid.UUID {
	member, _ := m.resolveMember(user, members)
	return member
}

// ResolveUser returns the Kuayle user that should author an imported item and,
// when the original author could not be matched, that author's name for an
// attribution line. It is for authors (reporter, comment author) only;
// assignees use ResolveAssignee. Order: explicit mapping, case-insensitive
// email match against workspace members, then the importer. The placeholder
// choice is treated as unmatched.
func (m Mapping) ResolveUser(user *SourceUser, members []Member, importerID uuid.UUID) (uuid.UUID, string) {
	if user == nil {
		return importerID, ""
	}
	if member, _ := m.resolveMember(user, members); member != nil {
		return *member, ""
	}
	return importerID, AttributionName(user)
}

// statusKey is the per-team key for a source status name. Two projects mapped
// to different teams can map the same source status to different team statuses.
func statusKey(teamID uuid.UUID, status string) string {
	return teamID.String() + ":" + strings.ToLower(strings.TrimSpace(status))
}

// ResolveStatus returns the mapped status for a source status in the given
// team, or false when the source value has no mapping for that team. A target
// that points at another team is never returned.
func (m Mapping) ResolveStatus(teamID uuid.UUID, status string) (StatusTarget, bool) {
	if target, ok := m.Statuses[statusKey(teamID, status)]; ok && target.TeamID == teamID {
		return target, true
	}
	return StatusTarget{}, false
}

// ResolvePriority maps a source priority name to a Kuayle priority, or false
// when it is unmapped.
func (m Mapping) ResolvePriority(priority string) (domain.IssuePriority, bool) {
	value, ok := m.Priorities[strings.ToLower(strings.TrimSpace(priority))]
	if !ok {
		return domain.PriorityNone, false
	}
	return domain.IssuePriority(value), true
}

// TargetHash folds the Kuayle values an issue resolved to (team, status,
// priority, assignee, project, cycle, labels, epic mode) into its source
// content hash. A re-run after the admin changes the mapping then updates
// issues whose source did not change, instead of skipping them.
func TargetHash(contentHash string, targets ...string) string {
	sum := sha256.Sum256([]byte(contentHash + "\x00" + strings.Join(targets, "\x00")))
	return hex.EncodeToString(sum[:])
}

// CommentHash summarizes one comment's body and source update time, so an
// edited comment makes its issue's ContentHash change and be re-synced.
func CommentHash(c SourceComment) string {
	raw, _ := json.Marshal(struct {
		Body      string
		UpdatedAt time.Time
	}{c.Body, c.UpdatedAt})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// ContentHash summarizes the fields a re-run would update. Equal hashes mean
// the issue is unchanged and can be skipped; a changed hash means update.
func ContentHash(issue SourceIssue) string {
	commentHashes := make([]string, 0, len(issue.Comments))
	for _, comment := range issue.Comments {
		commentHashes = append(commentHashes, CommentHash(comment))
	}
	sprints := make([]string, 0, len(issue.Sprints))
	for _, sprint := range issue.Sprints {
		sprints = append(sprints, sprint.Name)
	}
	attachments := make([]string, 0, len(issue.Attachments))
	for _, attachment := range issue.Attachments {
		attachments = append(attachments, attachment.ExternalID)
	}
	payload := struct {
		Title, Description, Status, Priority string
		Project, Parent, Epic                string
		Sprints, Labels                      []string
		Assignee                             string
		CreatedAt, UpdatedAt                 time.Time
		Comments                             []string
		Attachments                          []string
		History                              string
	}{
		Title:       issue.Title,
		Description: issue.Description,
		Status:      issue.Status,
		Priority:    issue.Priority,
		Project:     issue.ProjectExternalID,
		Parent:      issue.ParentExternalID,
		Epic:        issue.EpicExternalID,
		Sprints:     sprints,
		Labels:      issue.Labels,
		Assignee:    "",
		CreatedAt:   issue.CreatedAt,
		UpdatedAt:   issue.UpdatedAt,
		Comments:    commentHashes,
		Attachments: attachments,
		History:     issue.HistoryMarkdown,
	}
	if issue.Assignee != nil {
		payload.Assignee = strings.ToLower(strings.TrimSpace(issue.Assignee.Email))
		if payload.Assignee == "" {
			payload.Assignee = issue.Assignee.ExternalID
		}
	}
	raw, _ := json.Marshal(payload)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// ValidationContext holds the live workspace state needed to check mapping
// targets. The service builds it from repositories with the admin's access
// context, so a private team the admin cannot see is simply absent.
type ValidationContext struct {
	Teams        map[uuid.UUID]domain.Team
	TeamStatuses map[uuid.UUID][]domain.TeamStatus
	Members      map[uuid.UUID]bool
}

// Validate rejects mapping targets that would escape the workspace, reference
// a foreign team status, or assign to a non-member.
func (m Mapping) Validate(ctx ValidationContext) error {
	for key, target := range m.Projects {
		team, ok := ctx.Teams[target.TeamID]
		if !ok {
			return fmt.Errorf("project %q: team is not in this workspace or is not visible", key)
		}
		if target.ProjectID != nil && team.WorkspaceID == uuid.Nil {
			return fmt.Errorf("project %q: team has no workspace", key)
		}
	}
	for key, target := range m.Statuses {
		prefix := strings.SplitN(key, ":", 2)[0]
		if prefix != target.TeamID.String() {
			return fmt.Errorf("status %q: key does not belong to the target team", key)
		}
		statuses := ctx.TeamStatuses[target.TeamID]
		found := false
		for _, status := range statuses {
			if status.ID == target.StatusID {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("status %q: status does not belong to the target team", key)
		}
	}
	userKeys := make([]string, 0, len(m.Users))
	for key := range m.Users {
		userKeys = append(userKeys, key)
	}
	sort.Strings(userKeys)
	for _, key := range userKeys {
		if strings.TrimSpace(key) == "" {
			return errors.New("a user mapping needs the source email or user id")
		}
		if id := m.Users[key].UserID; id != nil && !ctx.Members[*id] {
			return fmt.Errorf("user %q: member is not in this workspace", key)
		}
	}
	for key, value := range m.Priorities {
		if value < int(domain.PriorityNone) || value > int(domain.PriorityLow) {
			return fmt.Errorf("priority %q: value %d is not a Kuayle priority", key, value)
		}
	}
	if m.EpicMode != "" && m.EpicMode != EpicModeProject && m.EpicMode != EpicModeParent {
		return fmt.Errorf("epic mode %q is not supported", m.EpicMode)
	}
	if m.SprintMode != "" && m.SprintMode != SprintModeCycle && m.SprintMode != SprintModeIgnore {
		return fmt.Errorf("sprint mode %q is not supported", m.SprintMode)
	}
	return nil
}

// UnmappedFor reports previewed source values the mapping does not cover yet,
// so the admin sees what is left before starting. It runs after a preview and
// again whenever the mapping is saved. members lets an email that matches a
// workspace member count as mapped.
func (m Mapping) UnmappedFor(preview *SourcePreview, members []Member) []string {
	seen := map[string]bool{}
	add := func(format string, args ...any) {
		text := fmt.Sprintf(format, args...)
		if !seen[text] {
			seen[text] = true
		}
	}
	// Issues of an unmapped project fail with "no project mapping".
	for _, project := range preview.Projects {
		if target, ok := m.Projects[project.ExternalID]; !ok || target.TeamID == uuid.Nil {
			add("project: %s (%s)", project.Name, project.Key)
		}
	}
	// A status must be mapped for every team an issue can land in. If no
	// project is mapped yet, report every status so the preview stays useful.
	teams := map[uuid.UUID]bool{}
	for _, target := range m.Projects {
		if target.TeamID != uuid.Nil {
			teams[target.TeamID] = true
		}
	}
	if len(teams) == 0 {
		for _, status := range preview.Statuses {
			add("status: %s", status)
		}
	} else {
		for teamID := range teams {
			for _, status := range preview.Statuses {
				if _, ok := m.ResolveStatus(teamID, status); !ok {
					add("status: %s", status)
				}
			}
		}
	}
	for _, priority := range preview.Priorities {
		if _, ok := m.ResolvePriority(priority); !ok {
			add("priority: %s", priority)
		}
	}
	for _, user := range preview.Users {
		if member, _ := m.resolveMember(&user, members); member == nil {
			add("user: %s", AttributionName(&user))
		}
	}
	out := make([]string, 0, len(seen))
	for text := range seen {
		out = append(out, text)
	}
	sort.Strings(out)
	return out
}

// ErrNoProjectMapping is returned by the write phase for an issue whose
// source project has no configured target.
var ErrNoProjectMapping = errors.New("no project mapping")
