package service

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
)

const assistantIssueFieldParams = `"assignees":{"type":"array","items":{"type":"string"},"description":"Workspace member names or email addresses. Use [\"me\"] for the requesting user. Replaces the assignees on update. Use [] to clear them."},
"due_date":{"type":"string","format":"date","description":"Calendar date in YYYY-MM-DD. Convert relative dates using the current date in the user's time zone."},
"cycle":{"type":"string","description":"Name or ID of a cycle in the issue's team."},
"project":{"type":"string","description":"Name or ID of a project in the issue's team."},
"labels":{"type":"array","items":{"type":"string"},"description":"Workspace label names or label names from the issue's project. Replaces the labels on update. Use [] to clear them."}`

type assistantIssueMembers interface {
	ListMembersWithUsers(context.Context, uuid.UUID) ([]domain.WorkspaceMemberWithUser, error)
}

type assistantIssueCycles interface {
	ListByTeam(context.Context, uuid.UUID) ([]domain.Cycle, error)
}

type assistantIssueProjects interface {
	ListByTeam(context.Context, uuid.UUID) ([]domain.Project, error)
}

// SetIssueFieldLookups wires the scoped lists used by the issue write tools.
func (t *AssistantTools) SetIssueFieldLookups(members assistantIssueMembers, cycles assistantIssueCycles, projects assistantIssueProjects) {
	t.issueMembers, t.issueCycles, t.issueProjects = members, cycles, projects
}

type assistantIssueFields struct {
	Assignees []string `json:"assignees"`
	DueDate   *string  `json:"due_date"`
	Cycle     *string  `json:"cycle"`
	Project   *string  `json:"project"`
	Labels    []string `json:"labels"`
}

func (f assistantIssueFields) empty() bool {
	return f.Assignees == nil && f.DueDate == nil && f.Cycle == nil && f.Project == nil && f.Labels == nil
}

func decodeIssueWriteInput(raw string, target any) error {
	if err := decodeToolInput(raw, target); err != nil {
		return err
	}
	// Distinguish omitted fields from explicit null values, which Go's string
	// pointers and slices both decode as nil.
	var fields map[string]json.RawMessage
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &fields); err != nil {
			return toolFailure("invalid arguments")
		}
	}
	for name, value := range fields {
		if string(value) != "null" {
			continue
		}
		switch strings.ToLower(name) {
		case "assignees", "due_date", "cycle", "project", "labels":
			return toolFailure("%s cannot be null", name)
		}
	}
	return nil
}

func (t *AssistantTools) resolveIssueFields(ctx context.Context, ws, user, team uuid.UUID, project *uuid.UUID, fields assistantIssueFields, req *dto.UpdateIssueRequest) error {
	req.DueDate = fields.DueDate
	if fields.DueDate != nil {
		date, err := time.Parse(time.DateOnly, *fields.DueDate)
		if err != nil || date.Year() == 0 || date.Format(time.DateOnly) != *fields.DueDate {
			return toolFailure("due_date %q must be a valid date in YYYY-MM-DD format", truncateRunes(*fields.DueDate, 100))
		}
	}

	var err error
	if req.AssigneeIDs, err = t.resolveIssueAssignees(ctx, ws, user, fields.Assignees); err != nil {
		return err
	}
	if fields.Cycle != nil {
		if t.issueCycles == nil {
			return toolFailure("cycle lookup is unavailable")
		}
		cycles, err := t.issueCycles.ListByTeam(ctx, team)
		if err != nil {
			return err
		}
		options := make([]issueFieldOption, 0, len(cycles))
		for _, cycle := range cycles {
			options = append(options, issueFieldOption{cycle.ID, []string{cycle.Name, cycle.ID.String()}})
		}
		id, err := resolveIssueFieldOption("cycle", *fields.Cycle, options)
		if err != nil {
			return err
		}
		value := id.String()
		req.CycleID = &value
	}
	if fields.Project != nil {
		if t.issueProjects == nil {
			return toolFailure("project lookup is unavailable")
		}
		projects, err := t.issueProjects.ListByTeam(ctx, team)
		if err != nil {
			return err
		}
		options := make([]issueFieldOption, 0, len(projects))
		for _, candidate := range projects {
			if candidate.WorkspaceID == ws {
				options = append(options, issueFieldOption{candidate.ID, []string{candidate.Name, candidate.ID.String()}})
			}
		}
		id, err := resolveIssueFieldOption("project", *fields.Project, options)
		if err != nil {
			return err
		}
		value := id.String()
		req.ProjectID = &value
		project = &id
	}
	req.LabelIDs, err = t.resolveIssueLabels(ctx, ws, project, fields.Labels)
	return err
}

func (t *AssistantTools) resolveIssueAssignees(ctx context.Context, ws, user uuid.UUID, values []string) ([]string, error) {
	if len(values) == 0 {
		return values, nil
	}
	if t.issueMembers == nil {
		return nil, toolFailure("assignee lookup is unavailable")
	}
	members, err := t.issueMembers.ListMembersWithUsers(ctx, ws)
	if err != nil {
		return nil, err
	}
	options := make([]issueFieldOption, 0, len(members))
	for _, member := range members {
		names := slices.DeleteFunc([]string{member.DisplayName, member.Name, member.Email}, func(name string) bool {
			return strings.EqualFold(strings.TrimSpace(name), "me")
		})
		if member.UserID == user {
			names = append([]string{"me"}, names...)
		}
		options = append(options, issueFieldOption{member.UserID, names})
	}
	return resolveIssueFieldList("assignees", values, options)
}

func (t *AssistantTools) resolveIssueLabels(ctx context.Context, ws uuid.UUID, project *uuid.UUID, values []string) ([]string, error) {
	if len(values) == 0 {
		return values, nil
	}
	if t.issueLabels == nil {
		return nil, toolFailure("label lookup is unavailable")
	}
	labels, err := t.issueLabels.ListByWorkspace(ctx, ws, project)
	if err != nil {
		return nil, err
	}
	options := make([]issueFieldOption, 0, len(labels))
	for _, label := range labels {
		// A nil project filter lists all visible labels. Only workspace-wide
		// labels and labels of the issue's effective project can be attached.
		if label.ProjectID == nil || (project != nil && *label.ProjectID == *project) {
			options = append(options, issueFieldOption{label.ID, []string{label.Name}})
		}
	}
	return resolveIssueFieldList("labels", values, options)
}

type issueFieldOption struct {
	id    uuid.UUID
	names []string
}

func resolveIssueFieldList(field string, values []string, options []issueFieldOption) ([]string, error) {
	ids := make([]string, 0, len(values))
	for _, value := range values {
		id, err := resolveIssueFieldOption(field, value, options)
		if err != nil {
			return nil, err
		}
		if !slices.Contains(ids, id.String()) {
			ids = append(ids, id.String())
		}
	}
	return ids, nil
}

func resolveIssueFieldOption(field, value string, options []issueFieldOption) (uuid.UUID, error) {
	name := strings.TrimSpace(value)
	var matches []issueFieldOption
	for _, option := range options {
		if name != "" && slices.ContainsFunc(option.names, func(candidate string) bool {
			return strings.EqualFold(strings.TrimSpace(candidate), name)
		}) {
			matches = append(matches, option)
		}
	}
	if len(matches) == 1 {
		return matches[0].id, nil
	}
	reason := "did not match"
	if len(matches) > 1 {
		reason = "is ambiguous"
	}
	var choices []string
	for _, option := range options {
		var names []string
		for _, candidate := range option.names {
			if candidate == "" || (len(matches) > 1 && strings.EqualFold(strings.TrimSpace(candidate), name)) {
				continue
			}
			quoted := fmt.Sprintf("%q", truncateRunes(candidate, 254))
			if !slices.Contains(names, quoted) {
				names = append(names, quoted)
			}
		}
		if len(names) > 0 {
			choices = append(choices, strings.Join(names, " / "))
		}
		if len(choices) == 5 {
			break
		}
	}
	if len(choices) == 0 {
		choices = []string{"none available"}
	}
	return uuid.Nil, toolFailure("%s value %q %s. Valid options include %s", field, truncateRunes(value, 100), reason, strings.Join(choices, ", "))
}

// IssueService treats junction writes as best effort. Confirm the requested
// sets before reporting success, leaving the issue link available on failure.
func (t *AssistantTools) issueWriteResult(ctx context.Context, issue *domain.Issue, req dto.UpdateIssueRequest) (any, *uuid.UUID, error) {
	if req.AssigneeIDs != nil {
		assignees, err := t.issues.GetAssignees(ctx, issue.ID)
		if err != nil || !issueFieldIDsMatch(assignees, req.AssigneeIDs) {
			return nil, &issue.ID, toolFailure("could not confirm the requested assignees on %s. Check the issue before retrying", issue.Identifier)
		}
	}
	if req.LabelIDs != nil {
		labels, err := t.issues.GetLabels(ctx, issue.ID)
		ids := make([]uuid.UUID, 0, len(labels))
		for _, label := range labels {
			ids = append(ids, label.ID)
		}
		if err != nil || !issueFieldIDsMatch(ids, req.LabelIDs) {
			return nil, &issue.ID, toolFailure("could not confirm the requested labels on %s. Check the issue before retrying", issue.Identifier)
		}
	}
	return issueSummary(*issue), &issue.ID, nil
}

func issueFieldIDsMatch(actual []uuid.UUID, requested []string) bool {
	ids := make([]uuid.UUID, 0, len(requested))
	for _, id := range requested {
		ids = append(ids, uuid.MustParse(id))
	}
	return sameUUIDSet(ids, actual)
}
