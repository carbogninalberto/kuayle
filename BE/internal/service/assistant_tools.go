package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/sanitize"
)

const (
	assistantMaxToolRounds    = 4
	assistantMaxCreatesPerRun = 3
	assistantToolOutputBytes  = 24 << 10
)

// errAssistantTool is a user-safe tool failure. Other errors are reported
// generically so internal details never reach the model or the conversation.
type errAssistantTool struct{ message string }

func (e *errAssistantTool) Error() string { return e.message }

func toolFailure(format string, args ...any) error {
	return &errAssistantTool{message: fmt.Sprintf(format, args...)}
}

type assistantIssueCreator interface {
	Create(ctx context.Context, workspaceID, creatorID uuid.UUID, req dto.CreateIssueRequest) (*domain.Issue, error)
}

type assistantMemberRepo interface {
	GetMember(ctx context.Context, workspaceID, userID uuid.UUID) (*domain.WorkspaceMember, error)
}

// AssistantTools exposes authorized Kuayle operations to the assistant. The
// model only proposes calls; every call re-checks the conversation creator's
// current role and team visibility in Go, exactly like the HTTP routes.
type AssistantTools struct {
	issues  repository.IssueRepo
	teams   repository.TeamRepo
	members assistantMemberRepo
	creator assistantIssueCreator
}

func NewAssistantTools(issues repository.IssueRepo, teams repository.TeamRepo, members assistantMemberRepo, creator assistantIssueCreator) *AssistantTools {
	return &AssistantTools{issues: issues, teams: teams, members: members, creator: creator}
}

type assistantTool struct {
	name, description, parameters string
	write                         bool
	// approval tools only prepare a request; nothing runs until the user approves.
	approval bool
}

var assistantToolCatalog = []assistantTool{
	{name: "list_teams", description: "List the teams the user can access, with the key used to create issues.", parameters: `{"type":"object","properties":{},"additionalProperties":false}`},
	{name: "search_issues", description: "Search issues the user can access by text in the title or identifier.", parameters: `{"type":"object","properties":{"query":{"type":"string","maxLength":200},"limit":{"type":"integer","minimum":1,"maximum":20}},"required":["query"],"additionalProperties":false}`},
	{name: "get_issue", description: "Read one issue by identifier, such as ENG-12.", parameters: `{"type":"object","properties":{"identifier":{"type":"string","maxLength":32}},"required":["identifier"],"additionalProperties":false}`},
	{name: "delegate_issue", description: "Ask the user to approve starting a coding agent on an existing issue. The agent only starts after the user approves in Kuayle; never claim work has started.", approval: true, parameters: `{"type":"object","properties":{"identifier":{"type":"string","maxLength":32},"instructions":{"type":"string","minLength":1,"maxLength":16000}},"required":["identifier","instructions"],"additionalProperties":false}`},
	{name: "create_issue", description: "Create a new issue. Only call this when the user explicitly asked to create or file an issue.", write: true, parameters: `{"type":"object","properties":{"team_key":{"type":"string","maxLength":16},"title":{"type":"string","minLength":1,"maxLength":500},"description":{"type":"string","maxLength":8000},"priority":{"type":"integer","minimum":0,"maximum":4}},"required":["team_key","title"],"additionalProperties":false}`},
}

func (t *AssistantTools) specs() []chatToolSpec {
	specs := make([]chatToolSpec, len(assistantToolCatalog))
	for i, tool := range assistantToolCatalog {
		specs[i].Type = "function"
		specs[i].Function.Name = tool.name
		specs[i].Function.Description = tool.description
		specs[i].Function.Parameters = json.RawMessage(tool.parameters)
	}
	return specs
}

func lookupAssistantTool(name string) (assistantTool, bool) {
	for _, tool := range assistantToolCatalog {
		if tool.name == name {
			return tool, true
		}
	}
	return assistantTool{}, false
}

// decodeToolInput rejects unknown fields and trailing data; an empty argument
// string is treated as an empty object, as some providers send for no-arg tools.
func decodeToolInput(raw string, target any) error {
	if strings.TrimSpace(raw) == "" {
		raw = "{}"
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return toolFailure("invalid arguments")
	}
	if decoder.More() {
		return toolFailure("invalid arguments")
	}
	return nil
}

// normalizedToolInput is the stored, displayed copy of the arguments.
func normalizedToolInput(raw string) json.RawMessage {
	var compact bytes.Buffer
	if json.Compact(&compact, []byte(raw)) != nil || compact.Len() == 0 || compact.Len() > assistantMaxToolInputBytes {
		return json.RawMessage(`{}`)
	}
	return compact.Bytes()
}

// run executes one call for the conversation creator. It returns the result
// shown to the user and the model, and an issue reference for created issues.
func (t *AssistantTools) run(ctx context.Context, ws, user uuid.UUID, name, arguments string) (any, *uuid.UUID, error) {
	tool, ok := lookupAssistantTool(name)
	if !ok {
		return nil, nil, toolFailure("unknown tool")
	}
	member, err := t.members.GetMember(ctx, ws, user)
	if err != nil || member == nil || member.Role == domain.RoleGuest || !domain.HasPermission(member.Role, domain.PermIssuesRead) {
		return nil, nil, toolFailure("you no longer have access to this workspace")
	}
	if tool.write && !domain.HasPermission(member.Role, domain.PermIssueCreate) {
		return nil, nil, toolFailure("your role cannot create issues")
	}
	switch name {
	case "list_teams":
		if err := decodeToolInput(arguments, &struct{}{}); err != nil {
			return nil, nil, err
		}
		teams, err := t.teams.ListByWorkspace(ctx, ws)
		if err != nil {
			return nil, nil, err
		}
		result := make([]map[string]string, 0, len(teams))
		for _, team := range teams {
			result = append(result, map[string]string{"key": team.Key, "name": team.Name})
		}
		return map[string]any{"teams": result}, nil, nil
	case "search_issues":
		var input struct {
			Query string `json:"query"`
			Limit int    `json:"limit"`
		}
		if err := decodeToolInput(arguments, &input); err != nil {
			return nil, nil, err
		}
		input.Query = strings.TrimSpace(input.Query)
		if input.Query == "" || utf8.RuneCountInString(input.Query) > 200 {
			return nil, nil, toolFailure("query must be 1 to 200 characters")
		}
		if input.Limit <= 0 || input.Limit > 20 {
			input.Limit = 10
		}
		params := dto.IssueFilterParams{Search: input.Query, Sort: "updated_at", Order: "desc"}
		params.Page, params.PerPage = 1, input.Limit
		issues, _, err := t.issues.List(ctx, ws, params)
		if err != nil {
			return nil, nil, err
		}
		result := make([]map[string]any, 0, len(issues))
		for _, issue := range issues {
			result = append(result, issueSummary(issue))
		}
		return map[string]any{"issues": result}, nil, nil
	case "get_issue":
		var input struct {
			Identifier string `json:"identifier"`
		}
		if err := decodeToolInput(arguments, &input); err != nil {
			return nil, nil, err
		}
		issue, err := t.issues.GetByIdentifier(ctx, ws, strings.ToUpper(strings.TrimSpace(input.Identifier)))
		if err != nil || issue == nil {
			return nil, nil, toolFailure("issue not found")
		}
		result := issueSummary(*issue)
		if issue.Description != nil {
			result["description"] = truncateRunes(sanitize.StripHTML(*issue.Description), 4000)
		}
		return result, nil, nil
	case "create_issue":
		var input struct {
			TeamKey     string `json:"team_key"`
			Title       string `json:"title"`
			Description string `json:"description"`
			Priority    *int   `json:"priority"`
		}
		if err := decodeToolInput(arguments, &input); err != nil {
			return nil, nil, err
		}
		input.Title = strings.TrimSpace(input.Title)
		if input.Title == "" || utf8.RuneCountInString(input.Title) > 500 || utf8.RuneCountInString(input.Description) > 8000 || (input.Priority != nil && (*input.Priority < 0 || *input.Priority > 4)) {
			return nil, nil, toolFailure("invalid issue fields")
		}
		teams, err := t.teams.ListByWorkspace(ctx, ws)
		if err != nil {
			return nil, nil, err
		}
		var teamID uuid.UUID
		for _, team := range teams {
			if strings.EqualFold(team.Key, strings.TrimSpace(input.TeamKey)) {
				teamID = team.ID
			}
		}
		if teamID == uuid.Nil {
			return nil, nil, toolFailure("team %q is not available; call list_teams", truncateRunes(input.TeamKey, 16))
		}
		request := dto.CreateIssueRequest{TeamID: teamID.String(), Title: input.Title, Priority: input.Priority}
		if input.Description != "" {
			request.Description = &input.Description
		}
		issue, err := t.creator.Create(ctx, ws, user, request)
		if err != nil {
			return nil, nil, toolFailure("the issue could not be created")
		}
		return issueSummary(*issue), &issue.ID, nil
	}
	return nil, nil, toolFailure("unknown tool")
}

func issueSummary(issue domain.Issue) map[string]any {
	return map[string]any{"identifier": issue.Identifier, "title": issue.Title, "status": issue.Status, "priority": issue.Priority}
}

func truncateRunes(value string, limit int) string {
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	return string([]rune(value)[:limit])
}

// toolErrorText keeps internal errors out of the conversation.
func toolErrorText(err error) string {
	var safe *errAssistantTool
	if errors.As(err, &safe) {
		return safe.message
	}
	return "the tool failed"
}

// prepareDelegation validates a delegation request for approval. It reads the
// issue with the user's own visibility and binds the request to it.
func (t *AssistantTools) prepareDelegation(ctx context.Context, ws, user uuid.UUID, arguments string) (uuid.UUID, domain.DelegationPayload, error) {
	member, err := t.members.GetMember(ctx, ws, user)
	if err != nil || member == nil || member.Role == domain.RoleGuest {
		return uuid.Nil, domain.DelegationPayload{}, toolFailure("you no longer have access to this workspace")
	}
	var input struct {
		Identifier   string `json:"identifier"`
		Instructions string `json:"instructions"`
	}
	if err = decodeToolInput(arguments, &input); err != nil {
		return uuid.Nil, domain.DelegationPayload{}, err
	}
	input.Instructions = strings.TrimSpace(input.Instructions)
	if input.Instructions == "" || utf8.RuneCountInString(input.Instructions) > 16000 {
		return uuid.Nil, domain.DelegationPayload{}, toolFailure("instructions must be 1 to 16000 characters")
	}
	issue, err := t.issues.GetByIdentifier(ctx, ws, strings.ToUpper(strings.TrimSpace(input.Identifier)))
	if err != nil || issue == nil {
		return uuid.Nil, domain.DelegationPayload{}, toolFailure("issue not found")
	}
	return issue.ID, domain.DelegationPayload{Identifier: issue.Identifier, Title: issue.Title, Instructions: input.Instructions}, nil
}
