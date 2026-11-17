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
// explicit unassigned/placeholder choice and falls back to the importer.
type UserTarget struct {
	UserID *uuid.UUID `json:"user_id"`
}

// Mapping is the admin-configured translation from source values to Kuayle
// entities. Keys are source identifiers or, for users and statuses, the
// lower-cased source email / name.
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

func attributionName(user *SourceUser) string {
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

// ResolveAssignee returns the Kuayle user that should be assigned an imported
// issue, or nil when the issue should have no assignee. Unlike ResolveUser it
// never falls back to the importer: a missing, unmatched or explicitly
// unassigned source user leaves the issue unassigned. Assignees and authors
// resolve differently on purpose.
func (m Mapping) ResolveAssignee(user *SourceUser, members []Member) *uuid.UUID {
	if user == nil {
		return nil
	}
	email := strings.ToLower(strings.TrimSpace(user.Email))
	byID, hasID := m.Users[user.ExternalID]
	byEmail, hasEmail := m.Users[email]
	if hasID && byID.UserID != nil {
		return byID.UserID
	}
	if hasEmail && byEmail.UserID != nil {
		return byEmail.UserID
	}
	// An explicit unassigned/placeholder choice wins over an automatic email
	// match, so honour it rather than assigning a member with the same email.
	if (hasID && byID.UserID == nil) || (hasEmail && byEmail.UserID == nil) {
		return nil
	}
	if email != "" && email != UnassignedChoice {
		for _, member := range members {
			if strings.EqualFold(strings.TrimSpace(member.Email), email) {
				id := member.UserID
				return &id
			}
		}
	}
	return nil
}

// ResolveUser returns the Kuayle user that should author an imported item and
// an attribution prefix to add to its text when the original author could not
// be matched. It is for authors (reporter, comment author) only; assignees use
// ResolveAssignee. Order: explicit mapping, case-insensitive email match against
// workspace members, then the importer. The placeholder choice is treated as
// unmatched.
func (m Mapping) ResolveUser(user *SourceUser, members []Member, importerID uuid.UUID) (uuid.UUID, string) {
	if user == nil {
		return importerID, ""
	}
	if target, ok := m.Users[user.ExternalID]; ok && target.UserID != nil {
		return *target.UserID, ""
	}
	if target, ok := m.Users[strings.ToLower(strings.TrimSpace(user.Email))]; ok && target.UserID != nil {
		return *target.UserID, ""
	}
	if email := strings.ToLower(strings.TrimSpace(user.Email)); email != "" && email != UnassignedChoice {
		for _, member := range members {
			if strings.EqualFold(strings.TrimSpace(member.Email), email) {
				return member.UserID, ""
			}
		}
	}
	return importerID, "*Originally by " + attributionName(user) + "*"
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

// ExternalKey builds a stable, opaque idempotency key. It is unique per site
// and external type so two trackers (or two Jira sites) cannot collide.
func ExternalKey(source, site, extType, externalID string) string {
	sum := sha256.Sum256([]byte(source + "\x00" + site + "\x00" + extType + "\x00" + externalID))
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
	payload := struct {
		Title, Description, Status, Priority string
		Project, Parent, Epic                string
		Sprints, Labels                      []string
		Assignee                             string
		CreatedAt, UpdatedAt                 time.Time
		Comments                             []string
		AttachmentCount                      int
	}{
		Title:           issue.Title,
		Description:     issue.Description,
		Status:          issue.Status,
		Priority:        issue.Priority,
		Project:         issue.ProjectExternalID,
		Parent:          issue.ParentExternalID,
		Epic:            issue.EpicExternalID,
		Sprints:         issue.SprintIDs,
		Labels:          issue.Labels,
		Assignee:        "",
		CreatedAt:       issue.CreatedAt,
		UpdatedAt:       issue.UpdatedAt,
		Comments:        commentHashes,
		AttachmentCount: len(issue.Attachments),
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

// UnmappedFor reports source values that the mapping does not cover, so the
// dry-run preview can warn the admin before any write happens.
func (m Mapping) UnmappedFor(preview *SourcePreview) []string {
	seen := map[string]bool{}
	add := func(format string, args ...any) {
		text := fmt.Sprintf(format, args...)
		if !seen[text] {
			seen[text] = true
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
		_, prefix := m.ResolveUser(&user, nil, uuid.New())
		if prefix != "" {
			add("user: %s", attributionName(&user))
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
