package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/sanitize"
)

const (
	// assistantMaxToolRounds lets every one of a reply's tool calls run in its
	// own model round; one more round, without tools, forces the answer.
	assistantMaxToolRounds = assistantMaxToolCalls
	// assistantMaxChangesPerReply bounds the write tools (issue creations,
	// updates, dependency links and repository links) of one reply.
	assistantMaxChangesPerReply = 10
	assistantToolOutputBytes    = 24 << 10
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
	Update(ctx context.Context, workspaceID, userID uuid.UUID, identifier string, req dto.UpdateIssueRequest) (*domain.Issue, error)
}

type assistantRelationCreator interface {
	Create(ctx context.Context, workspaceID uuid.UUID, issueIdentifier string, req dto.CreateIssueRelationRequest) (*domain.IssueRelation, error)
}

type assistantDependencies interface {
	Blockers(ctx context.Context, ws uuid.UUID, issues []uuid.UUID) ([]domain.IssueBlocker, error)
	Blocks(ctx context.Context, issue, blocker uuid.UUID) (bool, error)
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
	// Planning tools are unavailable until SetPlanning wires them.
	dependencies assistantDependencies
	relations    assistantRelationCreator
	// Agent work tools are unavailable until SetAgentWork wires them.
	work assistantAgentWork
	// link_repository is unavailable until SetRepositories wires it.
	repositories assistantRepositoryStore
}

type assistantAgentWork interface {
	IssueAgentWork(ctx context.Context, ws, issue uuid.UUID) ([]repository.IssueAgentWork, error)
	RecentAgentWork(ctx context.Context, ws uuid.UUID, state string, limit int) ([]repository.AgentWorkItem, error)
	MergeTarget(ctx context.Context, ws uuid.UUID, issue, task *uuid.UUID) (*repository.MergeTarget, error)
}

// SetAgentWork enables get_agent_work, list_agent_work and merge_pull_request.
func (t *AssistantTools) SetAgentWork(work assistantAgentWork) {
	t.work = work
}

// SetPlanning enables the dependency tools (plan_issues, link_issues and
// blockers in get_issue).
func (t *AssistantTools) SetPlanning(dependencies assistantDependencies, relations assistantRelationCreator) {
	t.dependencies, t.relations = dependencies, relations
}

func NewAssistantTools(issues repository.IssueRepo, teams repository.TeamRepo, members assistantMemberRepo, creator assistantIssueCreator) *AssistantTools {
	return &AssistantTools{issues: issues, teams: teams, members: members, creator: creator}
}

type assistantTool struct {
	name, description, parameters string
	write                         bool
	// approval tools only prepare a request; nothing runs until the user approves.
	approval bool
	// agentWork tools need SetAgentWork.
	agentWork bool
	// repositories tools need SetRepositories.
	repositories bool
}

var assistantToolCatalog = []assistantTool{
	{name: "list_teams", description: "List the teams the user can access, with the key used to create issues.", parameters: `{"type":"object","properties":{},"additionalProperties":false}`},
	{name: "search_issues", description: "Search issues the user can access by text in the title or identifier.", parameters: `{"type":"object","properties":{"query":{"type":"string","maxLength":200},"limit":{"type":"integer","minimum":1,"maximum":20}},"required":["query"],"additionalProperties":false}`},
	{name: "get_issue", description: "Read one issue by identifier, such as ENG-12.", parameters: `{"type":"object","properties":{"identifier":{"type":"string","maxLength":32}},"required":["identifier"],"additionalProperties":false}`},
	{name: "delegate_issue", description: "Ask the user to approve starting a coding agent on an existing issue. The agent only starts after the user approves in Kuayle; never claim work has started.", approval: true, parameters: `{"type":"object","properties":{"identifier":{"type":"string","maxLength":32},"instructions":{"type":"string","minLength":1,"maxLength":16000}},"required":["identifier","instructions"],"additionalProperties":false}`},
	{name: "update_issue", description: "Change an existing issue's title, description, status or priority. Only call this when the user explicitly asked for that change.", write: true, parameters: `{"type":"object","properties":{"identifier":{"type":"string","maxLength":32},"title":{"type":"string","minLength":1,"maxLength":500},"description":{"type":"string","maxLength":8000},"status":{"type":"string","enum":["backlog","todo","in_progress","in_review","done","cancelled"]},"priority":{"type":"integer","minimum":0,"maximum":4}},"required":["identifier"],"additionalProperties":false}`},
	{name: "create_issue", description: "Create a new issue, optionally as a sub-issue of parent_identifier to split work between agents. Only call this when the user explicitly asked to create or file an issue.", write: true, parameters: `{"type":"object","properties":{"team_key":{"type":"string","maxLength":16},"title":{"type":"string","minLength":1,"maxLength":500},"description":{"type":"string","maxLength":8000},"priority":{"type":"integer","minimum":0,"maximum":4},"parent_identifier":{"type":"string","maxLength":32}},"required":["team_key","title"],"additionalProperties":false}`},
	{name: "plan_issues", description: "Order up to 20 issues by their blocked-by dependencies into waves that can run in parallel. Delegated issues with unfinished blockers wait automatically, so a whole plan can be delegated at once.", parameters: `{"type":"object","properties":{"identifiers":{"type":"array","minItems":1,"maxItems":20,"items":{"type":"string","maxLength":32}}},"required":["identifiers"],"additionalProperties":false}`},
	{name: "get_agent_work", agentWork: true, description: "Read the coding tasks on an issue as Kuayle recorded them: state, attempt, pull request and latest review decision. Use it before saying anything about an issue's code, pull request or merge.", parameters: `{"type":"object","properties":{"identifier":{"type":"string","maxLength":32}},"required":["identifier"],"additionalProperties":false}`},
	{name: "list_agent_work", agentWork: true, description: "List the workspace's most recently updated coding tasks on issues the user can see, newest first: issue, task state and reason, agent, attempt, pull request and latest review decision. Use it for questions about what agents are doing, what is pending or awaiting review, or overall status; filter by state when the user asks about one.", parameters: `{"type":"object","properties":{"state":{"type":"string","enum":["queued","preparing","running","waiting_input","validating","publishing","awaiting_review","blocked","completed","failed","cancelling","cancelled"]},"limit":{"type":"integer","minimum":1,"maximum":50}},"additionalProperties":false}`},
	{name: "merge_pull_request", agentWork: true, approval: true, description: "Ask the user to approve squash-merging the pull request of an issue's task that is awaiting review, at the commit Kuayle published. Nothing merges until the user approves in Kuayle; GitHub still enforces branch protection. Only call this when the user asked to merge.", parameters: `{"type":"object","properties":{"identifier":{"type":"string","maxLength":32}},"required":["identifier"],"additionalProperties":false}`},
	{name: "link_issues", description: "Record that an issue is blocked by another issue. Only call this when the user asked to plan or order work. Cycles are refused.", write: true, parameters: `{"type":"object","properties":{"identifier":{"type":"string","maxLength":32},"blocked_by":{"type":"string","maxLength":32}},"required":["identifier","blocked_by"],"additionalProperties":false}`},
	{name: "link_repository", repositories: true, write: true, description: "Set the GitHub repository (owner/name) coding agents use for an issue, or with scope \"project\" for every issue of its project, as the development settings in Kuayle do. Only repositories linked to the workspace's GitHub installation and allowed by its Dev Machine policy can be used; the base branch defaults to the repository's default branch. Only call this when the user explicitly asked to link or change the repository.", parameters: `{"type":"object","properties":{"identifier":{"type":"string","maxLength":32},"repository":{"type":"string","minLength":3,"maxLength":200},"base_branch":{"type":"string","minLength":1,"maxLength":255},"scope":{"type":"string","enum":["issue","project"]}},"required":["identifier","repository"],"additionalProperties":false}`},
}

func (t *AssistantTools) specs() []chatToolSpec {
	specs := make([]chatToolSpec, 0, len(assistantToolCatalog))
	for _, tool := range assistantToolCatalog {
		if !t.available(tool) {
			continue
		}
		var spec chatToolSpec
		spec.Type = "function"
		spec.Function.Name = tool.name
		spec.Function.Description = tool.description
		spec.Function.Parameters = json.RawMessage(tool.parameters)
		specs = append(specs, spec)
	}
	return specs
}

// available reports whether a tool's dependencies are wired.
func (t *AssistantTools) available(tool assistantTool) bool {
	return (!tool.agentWork || t.work != nil) && (!tool.repositories || t.repositories != nil)
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
// shown to the user and the model, and a reference to a created or changed issue.
func (t *AssistantTools) run(ctx context.Context, ws, user uuid.UUID, name, arguments string) (any, *uuid.UUID, error) {
	tool, ok := lookupAssistantTool(name)
	if !ok || !t.available(tool) {
		return nil, nil, toolFailure("unknown tool")
	}
	// Changes made through these tools are attributed to the assistant.
	ctx = domain.WithActingAgent(ctx, "assistant")
	member, err := t.members.GetMember(ctx, ws, user)
	if err != nil || member == nil || member.Role == domain.RoleGuest || !domain.HasPermission(member.Role, domain.PermIssuesRead) {
		return nil, nil, toolFailure("you no longer have access to this workspace")
	}
	if name == "create_issue" && !domain.HasPermission(member.Role, domain.PermIssueCreate) {
		return nil, nil, toolFailure("your role cannot create issues")
	}
	if (name == "update_issue" || name == "link_issues") && !domain.HasPermission(member.Role, domain.PermIssueUpdate) {
		return nil, nil, toolFailure("your role cannot update issues")
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
		if t.dependencies != nil {
			blockers, err := t.visibleBlockers(ctx, ws, []uuid.UUID{issue.ID})
			if err != nil {
				return nil, nil, err
			}
			result["blocked_by"] = blockers[issue.ID]
			params := dto.IssueFilterParams{ParentID: issue.ID.String(), Sort: "created_at", Order: "asc"}
			params.Page, params.PerPage = 1, 20
			children, _, err := t.issues.List(ctx, ws, params)
			if err != nil {
				return nil, nil, err
			}
			subIssues := make([]map[string]any, 0, len(children))
			for _, child := range children {
				subIssues = append(subIssues, issueSummary(child))
			}
			result["sub_issues"] = subIssues
		}
		return result, nil, nil
	case "get_agent_work":
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
		work, err := t.work.IssueAgentWork(ctx, ws, issue.ID)
		if err != nil {
			return nil, nil, err
		}
		return map[string]any{"identifier": issue.Identifier, "tasks": work}, &issue.ID, nil
	case "list_agent_work":
		return t.listAgentWork(ctx, ws, arguments)
	case "link_repository":
		return t.linkRepository(ctx, ws, member.Role, arguments)
	case "plan_issues":
		return t.planIssues(ctx, ws, arguments)
	case "link_issues":
		return t.linkIssues(ctx, ws, arguments)
	case "update_issue":
		var input struct {
			Identifier  string  `json:"identifier"`
			Title       *string `json:"title"`
			Description *string `json:"description"`
			Status      *string `json:"status"`
			Priority    *int    `json:"priority"`
		}
		if err := decodeToolInput(arguments, &input); err != nil {
			return nil, nil, err
		}
		request := dto.UpdateIssueRequest{Description: input.Description, Priority: input.Priority, Status: input.Status}
		if input.Title != nil {
			title := strings.TrimSpace(*input.Title)
			if title == "" || utf8.RuneCountInString(title) > 500 {
				return nil, nil, toolFailure("invalid issue fields")
			}
			request.Title = &title
		}
		if input.Description != nil && utf8.RuneCountInString(*input.Description) > 8000 {
			return nil, nil, toolFailure("invalid issue fields")
		}
		if input.Status != nil && !slices.Contains([]string{"backlog", "todo", "in_progress", "in_review", "done", "cancelled"}, *input.Status) {
			return nil, nil, toolFailure("invalid status")
		}
		if input.Priority != nil && (*input.Priority < 0 || *input.Priority > 4) {
			return nil, nil, toolFailure("invalid issue fields")
		}
		if request.Title == nil && request.Description == nil && request.Status == nil && request.Priority == nil {
			return nil, nil, toolFailure("no changes requested")
		}
		identifier := strings.ToUpper(strings.TrimSpace(input.Identifier))
		// Resolve with the user's own visibility first; missing and hidden issues look the same.
		if issue, err := t.issues.GetByIdentifier(ctx, ws, identifier); err != nil || issue == nil {
			return nil, nil, toolFailure("issue not found")
		}
		issue, err := t.creator.Update(ctx, ws, user, identifier, request)
		if err != nil {
			return nil, nil, toolFailure("the issue could not be updated")
		}
		return issueSummary(*issue), &issue.ID, nil
	case "create_issue":
		var input struct {
			TeamKey     string `json:"team_key"`
			Title       string `json:"title"`
			Description string `json:"description"`
			Priority    *int   `json:"priority"`
			Parent      string `json:"parent_identifier"`
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
		if parent := strings.ToUpper(strings.TrimSpace(input.Parent)); parent != "" {
			issue, err := t.issues.GetByIdentifier(ctx, ws, parent)
			if err != nil || issue == nil {
				return nil, nil, toolFailure("parent issue not found")
			}
			id := issue.ID.String()
			request.ParentID = &id
		}
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

// prepareMerge binds a merge request to the issue's task awaiting review, its
// recorded pull request and the commit Kuayle published.
func (t *AssistantTools) prepareMerge(ctx context.Context, ws, user uuid.UUID, arguments string) (uuid.UUID, domain.MergePayload, error) {
	member, err := t.members.GetMember(ctx, ws, user)
	if err != nil || member == nil || member.Role == domain.RoleGuest {
		return uuid.Nil, domain.MergePayload{}, toolFailure("you no longer have access to this workspace")
	}
	if t.work == nil {
		return uuid.Nil, domain.MergePayload{}, toolFailure("unknown tool")
	}
	var input struct {
		Identifier string `json:"identifier"`
	}
	if err = decodeToolInput(arguments, &input); err != nil {
		return uuid.Nil, domain.MergePayload{}, err
	}
	issue, err := t.issues.GetByIdentifier(ctx, ws, strings.ToUpper(strings.TrimSpace(input.Identifier)))
	if err != nil || issue == nil {
		return uuid.Nil, domain.MergePayload{}, toolFailure("issue not found")
	}
	target, err := t.work.MergeTarget(ctx, ws, &issue.ID, nil)
	if errors.Is(err, repository.ErrNoMergeTarget) {
		return uuid.Nil, domain.MergePayload{}, toolFailure("no pull request of this issue is awaiting review")
	}
	if err != nil {
		return uuid.Nil, domain.MergePayload{}, err
	}
	return issue.ID, domain.MergePayload{
		Identifier: issue.Identifier, Title: issue.Title, TaskID: target.TaskID, Repository: target.Repository,
		PullRequest: target.Number, PullRequestURL: target.URL, Commit: target.Commit,
	}, nil
}
