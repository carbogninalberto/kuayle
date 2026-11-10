package mcp

import (
	"net/http"
	"regexp"
)

// tool maps one MCP tool onto one route of the HTTP API. Arguments named in
// Path fill the route; the others become the query string of a GET or the
// JSON body of a write, unchanged, so the API validates them as it does for
// any other client.
type tool struct {
	Name        string
	Description string
	Method      string
	// Path is relative to /api/workspaces/{workspace} unless it starts with /api.
	Path  string
	Props []prop
}

type prop struct {
	Name     string
	Type     string // JSON Schema type; "array" is an array of strings
	Desc     string
	Required bool
	Enum     []string
}

var pathParam = regexp.MustCompile(`\{([a-z_]+)\}`)

// workspaceProp is accepted by every workspace tool.
var workspaceProp = prop{Name: "workspace", Type: "string", Desc: "Workspace slug. Optional when the server URL pins one with ?workspace= or the token reaches a single workspace; see list_workspaces."}

func (t tool) workspaceScoped() bool { return len(t.Path) < 4 || t.Path[:4] != "/api" }

func (t tool) readOnly() bool { return t.Method == http.MethodGet }

func (t tool) props() []prop {
	if t.workspaceScoped() {
		return append([]prop{workspaceProp}, t.Props...)
	}
	return t.Props
}

// descriptor is the tool as listed to MCP clients.
func (t tool) descriptor() map[string]any {
	properties := map[string]any{}
	required := []string{}
	for _, p := range t.props() {
		schema := map[string]any{"type": p.Type, "description": p.Desc}
		if p.Type == "array" {
			schema["items"] = map[string]any{"type": "string"}
		}
		if len(p.Enum) > 0 {
			schema["enum"] = p.Enum
		}
		properties[p.Name] = schema
		if p.Required {
			required = append(required, p.Name)
		}
	}
	return map[string]any{
		"name":        t.Name,
		"description": t.Description,
		"inputSchema": map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false},
		"annotations": map[string]any{"readOnlyHint": t.readOnly(), "destructiveHint": false, "openWorldHint": false},
	}
}

var (
	wikiScopeProps = []prop{
		{Name: "scope", Type: "string", Desc: "Whether the wiki belongs to a team or a project.", Required: true, Enum: []string{"team", "project"}},
		{Name: "scope_id", Type: "string", Desc: "ID of that team or project (list_teams, list_projects).", Required: true},
	}
	wikiPageProp = prop{Name: "page_id", Type: "string", Desc: "Wiki page ID (search_wiki_pages).", Required: true}
	identifier   = prop{Name: "identifier", Type: "string", Desc: "Issue identifier, for example KUA-12.", Required: true}
	teamID       = prop{Name: "team_id", Type: "string", Desc: "Team ID (list_teams).", Required: true}

	statuses = []string{"backlog", "todo", "in_progress", "in_review", "done", "cancelled"}

	// issueFields are the fields create_issue and update_issue share.
	issueFields = []prop{
		{Name: "description", Type: "string", Desc: "Markdown or HTML."},
		{Name: "status", Type: "string", Desc: "Status category. Prefer status_id for a team's own statuses.", Enum: statuses},
		{Name: "status_id", Type: "string", Desc: "ID of one of the team's statuses (list_statuses)."},
		{Name: "priority", Type: "integer", Desc: "0 none, 1 urgent, 2 high, 3 medium, 4 low."},
		{Name: "assignee_ids", Type: "array", Desc: "User IDs (list_members); replaces the current assignees."},
		{Name: "label_ids", Type: "array", Desc: "Label IDs (list_labels); replaces the current labels."},
		{Name: "project_id", Type: "string", Desc: "Project ID (list_projects)."},
		{Name: "cycle_id", Type: "string", Desc: "Cycle ID (list_cycles)."},
		{Name: "parent_id", Type: "string", Desc: "ID of the parent issue, making this a sub-issue."},
		{Name: "due_date", Type: "string", Desc: "Due date as YYYY-MM-DD; an empty string clears it on update."},
	}
)

func with(first []prop, rest ...prop) []prop {
	return append(append([]prop{}, first...), rest...)
}

var tools = []tool{
	{Name: "list_workspaces", Description: "List the workspaces this token can reach.", Method: http.MethodGet, Path: "/api/workspaces"},
	{Name: "list_teams", Description: "List the teams visible to the token's user, with their IDs and issue keys.", Method: http.MethodGet, Path: "/teams"},
	{Name: "list_projects", Description: "List the workspace's projects.", Method: http.MethodGet, Path: "/projects"},
	{Name: "list_labels", Description: "List the workspace's labels.", Method: http.MethodGet, Path: "/labels"},
	{Name: "list_members", Description: "List the workspace's members: user IDs, names and roles.", Method: http.MethodGet, Path: "/members"},
	{Name: "list_statuses", Description: "List a team's issue statuses.", Method: http.MethodGet, Path: "/teams/{team_id}/statuses", Props: []prop{teamID}},
	{Name: "list_cycles", Description: "List a team's cycles.", Method: http.MethodGet, Path: "/teams/{team_id}/cycles", Props: []prop{teamID}},
	{
		Name:        "list_issues",
		Description: "Search and list issues, paginated. Every filter is optional and filters combine with AND.",
		Method:      http.MethodGet, Path: "/issues",
		Props: []prop{
			{Name: "search", Type: "string", Desc: "Text to find in the identifier, title or description."},
			{Name: "team", Type: "string", Desc: "Team ID."},
			{Name: "status", Type: "string", Desc: "Comma-separated statuses: either status IDs (list_statuses) or categories (backlog, todo, in_progress, in_review, done, cancelled), not a mix."},
			{Name: "priority", Type: "string", Desc: "Comma-separated priorities, 0 (none) to 4 (low); 1 is urgent."},
			{Name: "assignee", Type: "string", Desc: "User ID, or none for unassigned issues."},
			{Name: "creator", Type: "string", Desc: "User ID."},
			{Name: "project", Type: "string", Desc: "Project ID, or none."},
			{Name: "cycle", Type: "string", Desc: "Cycle ID, or none."},
			{Name: "label", Type: "string", Desc: "Label ID, or none."},
			{Name: "parent_id", Type: "string", Desc: "ID of the parent issue, or none."},
			{Name: "due_before", Type: "string", Desc: "YYYY-MM-DD."},
			{Name: "due_after", Type: "string", Desc: "YYYY-MM-DD."},
			{Name: "sort", Type: "string", Desc: "Sort field.", Enum: []string{"created_at", "updated_at", "priority", "sort_order", "status", "due_date"}},
			{Name: "order", Type: "string", Desc: "Sort direction.", Enum: []string{"asc", "desc"}},
			{Name: "page", Type: "integer", Desc: "Page number, from 1."},
			{Name: "per_page", Type: "integer", Desc: "Issues per page, 1 to 100 (default 50)."},
		},
	},
	{Name: "get_issue", Description: "Get one issue by its identifier.", Method: http.MethodGet, Path: "/issues/{identifier}", Props: []prop{identifier}},
	{Name: "list_comments", Description: "List the comments of an issue.", Method: http.MethodGet, Path: "/issues/{identifier}/comments", Props: []prop{identifier}},
	{
		Name:        "create_issue",
		Description: "Create an issue in a team. Not idempotent: calling it again creates another issue.",
		Method:      http.MethodPost, Path: "/issues",
		Props: with([]prop{{Name: "title", Type: "string", Desc: "Issue title.", Required: true}, teamID}, issueFields...),
	},
	{
		Name:        "update_issue",
		Description: "Update an issue. Only the fields given change.",
		Method:      http.MethodPatch, Path: "/issues/{identifier}",
		Props: with([]prop{identifier, {Name: "title", Type: "string", Desc: "Issue title."}}, issueFields...),
	},
	{
		Name:        "add_comment",
		Description: "Add a comment to an issue. Not idempotent: calling it again adds another comment.",
		Method:      http.MethodPost, Path: "/issues/{identifier}/comments",
		Props: []prop{identifier,
			{Name: "body", Type: "string", Desc: "Comment text, Markdown or HTML.", Required: true},
			{Name: "parent_id", Type: "string", Desc: "ID of the comment this one replies to."}},
	},
	{
		Name:        "search_wiki_pages",
		Description: "List the pages of a team or project wiki, without their content. With search, only pages whose title or content contains that text.",
		Method:      http.MethodGet, Path: "/{scope}s/{scope_id}/wiki",
		Props: with(wikiScopeProps, prop{Name: "search", Type: "string", Desc: "Text to find in the title or content."}),
	},
	{Name: "get_wiki_page", Description: "Get a wiki page with its content and version.", Method: http.MethodGet, Path: "/{scope}s/{scope_id}/wiki/{page_id}", Props: with(wikiScopeProps, wikiPageProp)},
	{
		Name:        "create_wiki_page",
		Description: "Create a page in a team or project wiki. Not idempotent: calling it again creates another page.",
		Method:      http.MethodPost, Path: "/{scope}s/{scope_id}/wiki",
		Props: with(wikiScopeProps,
			prop{Name: "title", Type: "string", Desc: "Page title.", Required: true},
			prop{Name: "content", Type: "string", Desc: "Page content, Markdown or HTML."}),
	},
	{
		Name:        "update_wiki_page",
		Description: "Replace the title and/or content of a wiki page. version must be the version read with get_wiki_page; a page changed since then is rejected with a conflict.",
		Method:      http.MethodPatch, Path: "/{scope}s/{scope_id}/wiki/{page_id}",
		Props: with(wikiScopeProps, wikiPageProp,
			prop{Name: "version", Type: "integer", Desc: "Version of the page the edit starts from.", Required: true},
			prop{Name: "title", Type: "string", Desc: "New title."},
			prop{Name: "content", Type: "string", Desc: "New content, Markdown or HTML; replaces the whole page."}),
	},
}

// resourceTemplates lists the readable resources. Each is a GET route with
// the same placeholders as its URI.
var resourceTemplates = []struct {
	URI, Name, Description, Path string
}{
	{"kuayle://issue/{identifier}", "Issue", "An issue by its identifier, for example kuayle://issue/KUA-12.", "/issues/{identifier}"},
	{"kuayle://wiki/{scope}/{scope_id}/{page_id}", "Wiki page", "A page of a team or project wiki; scope is team or project.", "/{scope}s/{scope_id}/wiki/{page_id}"},
}
