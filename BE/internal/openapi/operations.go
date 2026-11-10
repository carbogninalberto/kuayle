package openapi

import (
	"net/http"
	"strings"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
)

// Operation documents one stable, token-accessible route. Method, Path (in
// the router's ":param" form) and Scope must equal the route registered in
// cmd/server/routes.go; a test there compares the two.
type Operation struct {
	Method string
	Path   string
	// Scope is the token scope the route is annotated with.
	Scope string
	// AlsoRequires lists further scopes the route's extra middleware demands.
	AlsoRequires []string

	ID          string
	Tag         string
	Summary     string
	Description string

	// Query is a struct whose `query` tags are the query parameters.
	Query any
	// Params are query parameters a handler reads by name.
	Params []Param
	// Body is the request DTO; Response the DTO (or slice of DTOs) written
	// with Status.
	Body     any
	Status   int
	Response any
	// Conflict documents a 409 response.
	Conflict string
}

// Param is a query parameter a handler reads without a filter struct.
type Param struct {
	Name, Type, Format, Description string
}

// InternalRoute classifies token-accessible routes that are deliberately not
// part of the published reference. Prefix rules cover a whole subsystem.
type InternalRoute struct {
	Method string // empty with Prefix: any method
	Path   string
	Prefix bool
	Reason string
}

// Matches reports whether the rule covers the registered route.
func (r InternalRoute) Matches(method, path string) bool {
	if r.Prefix {
		return strings.HasPrefix(path, r.Path)
	}
	return r.Method == method && r.Path == path
}

// StatusResult is the body of writes that return no resource, for example
// {"status":"deleted"}.
type StatusResult struct {
	Status string `json:"status"`
}

// BulkUpdateResult counts the issues a bulk update changed.
type BulkUpdateResult struct {
	Updated int `json:"updated"`
}

// BulkDeleteResult counts the issues a bulk delete removed.
type BulkDeleteResult struct {
	Deleted int `json:"deleted"`
}

const ws = "/api/workspaces/:slug"

var issueFilterDescriptions = map[string]string{
	"page":                  "Page number, starting at 1.",
	"per_page":              "Items per page. Defaults to 50.",
	"status":                "Comma-separated legacy status names or status IDs.",
	"status_type":           "Status category: backlog, unstarted, started, completed or cancelled.",
	"priority":              "Comma-separated priorities, 0 (none) to 4.",
	"assignee":              "User ID, or \"none\" for unassigned issues.",
	"creator":               "User ID of the creator.",
	"team":                  "Team ID.",
	"project":               "Project ID, or \"none\" for issues without a project.",
	"cycle":                 "Cycle ID, or \"none\" for issues without a cycle.",
	"label":                 "Label ID, or \"none\" for issues without labels.",
	"search":                "Text matched against the issue title and identifier and the team name, key and description.",
	"due_before":            "Only issues due on or before this date (YYYY-MM-DD).",
	"due_after":             "Only issues due on or after this date (YYYY-MM-DD).",
	"triaged":               "\"false\" for issues waiting in triage, any other value for triaged issues.",
	"sub_issues":            "\"exclude\" or \"top_level\" for issues without a parent, \"only\" for sub-issues, \"has_sub_issues\" for parents.",
	"parent_id":             "Parent issue ID, or \"none\" for issues without a parent.",
	"group_by":              "Orders the result so that issues of one group are adjacent.",
	"sort":                  "Sort field. Defaults to created_at.",
	"order":                 "Sort direction.",
	"status_category_order": "With group_by=status, a comma-separated order of status categories.",
}

// Operations returns the stable public operations in reference order.
func Operations() []Operation {
	ops := []Operation{
		// Account and workspaces
		{Method: http.MethodGet, Path: "/api/auth/me", Scope: domain.PermAccountRead, ID: "getCurrentUser", Tag: "Account",
			Summary: "Get the token's user", Description: "Returns the user who owns the token. Use it to check that a token works.",
			Status: 200, Response: dto.UserResponse{}},
		{Method: http.MethodGet, Path: "/api/workspaces", Scope: domain.PermWorkspacesRead, ID: "listWorkspaces", Tag: "Workspaces",
			Summary: "List workspaces", Description: "Lists the workspaces the user belongs to. A token limited to some workspaces lists only those.",
			Status: 200, Response: []dto.WorkspaceResponse{}},
		{Method: http.MethodGet, Path: ws, Scope: domain.PermWorkspacesRead, ID: "getWorkspace", Tag: "Workspaces",
			Summary: "Get a workspace", Status: 200, Response: dto.WorkspaceResponse{}},

		// Members
		{Method: http.MethodGet, Path: ws + "/members", Scope: domain.PermMembersRead, ID: "listMembers", Tag: "Members",
			Summary: "List workspace members", Status: 200, Response: []dto.WorkspaceMemberResponse{}},
		{Method: http.MethodPost, Path: ws + "/invite", Scope: domain.PermMemberInvite, ID: "inviteMember", Tag: "Members",
			Summary: "Add an existing user to the workspace", Description: "Adds a registered user by email with the given role.",
			Body: dto.InviteMemberRequest{}, Status: 200, Response: StatusResult{}},
		{Method: http.MethodPatch, Path: ws + "/members/:userId", Scope: domain.PermMemberInvite, ID: "updateMemberRole", Tag: "Members",
			Summary: "Change a member's role", Body: dto.UpdateMemberRoleRequest{}, Status: 200, Response: StatusResult{}},
		{Method: http.MethodDelete, Path: ws + "/members/:userId", Scope: domain.PermMemberInvite, ID: "removeMember", Tag: "Members",
			Summary: "Remove a member from the workspace", Status: 200, Response: StatusResult{}},

		// Teams
		{Method: http.MethodGet, Path: ws + "/teams", Scope: domain.PermTeamsRead, ID: "listTeams", Tag: "Teams",
			Summary: "List teams", Description: "Lists the teams the user can see. Private teams appear only for their members, owners and admins.",
			Status: 200, Response: []dto.TeamResponse{}},
		{Method: http.MethodPost, Path: ws + "/teams", Scope: domain.PermTeamManage, ID: "createTeam", Tag: "Teams",
			Summary: "Create a team", Body: dto.CreateTeamRequest{}, Status: 201, Response: dto.TeamResponse{},
			Conflict: "A team with this key already exists."},
		{Method: http.MethodGet, Path: ws + "/teams/:teamId", Scope: domain.PermTeamsRead, ID: "getTeam", Tag: "Teams",
			Summary: "Get a team", Status: 200, Response: dto.TeamResponse{}},
		{Method: http.MethodPatch, Path: ws + "/teams/:teamId", Scope: domain.PermTeamManage, ID: "updateTeam", Tag: "Teams",
			Summary: "Update a team", Body: dto.UpdateTeamRequest{}, Status: 200, Response: dto.TeamResponse{},
			Conflict: "A team with this key already exists."},
		{Method: http.MethodDelete, Path: ws + "/teams/:teamId", Scope: domain.PermTeamManage, ID: "deleteTeam", Tag: "Teams",
			Summary: "Delete a team", Status: 200, Response: StatusResult{}},
		{Method: http.MethodPatch, Path: ws + "/teams/:teamId/visibility", Scope: domain.PermTeamManage, ID: "setTeamVisibility", Tag: "Teams",
			Summary: "Make a team private or public", Body: dto.SetTeamVisibilityRequest{}, Status: 200, Response: dto.TeamResponse{},
			Conflict: "The change conflicts with existing private-team data."},
		{Method: http.MethodGet, Path: ws + "/teams/:teamId/members", Scope: domain.PermMembersRead, AlsoRequires: []string{domain.PermTeamsRead},
			ID: "listTeamMembers", Tag: "Teams", Summary: "List team members", Status: 200, Response: []domain.TeamMember{}},
		{Method: http.MethodPost, Path: ws + "/teams/:teamId/members", Scope: domain.PermTeamManage, ID: "addTeamMember", Tag: "Teams",
			Summary: "Add a workspace member to a team", Body: dto.AddTeamMemberRequest{}, Status: 200, Response: StatusResult{}},
		{Method: http.MethodDelete, Path: ws + "/teams/:teamId/members/:userId", Scope: domain.PermTeamManage, ID: "removeTeamMember", Tag: "Teams",
			Summary: "Remove a member from a team", Status: 200, Response: StatusResult{}},

		// Statuses
		{Method: http.MethodGet, Path: ws + "/teams/:teamId/statuses", Scope: domain.PermTeamsRead, ID: "listStatuses", Tag: "Statuses",
			Summary: "List a team's statuses", Description: "Statuses are the team's workflow, ordered by position. Use a status ID as status_id when writing issues.",
			Status: 200, Response: []dto.TeamStatusResponse{}},
		{Method: http.MethodPost, Path: ws + "/teams/:teamId/statuses", Scope: domain.PermTeamManage, ID: "createStatus", Tag: "Statuses",
			Summary: "Create a status", Body: dto.CreateTeamStatusRequest{}, Status: 201, Response: dto.TeamStatusResponse{}},
		{Method: http.MethodPatch, Path: ws + "/teams/:teamId/statuses/:statusId", Scope: domain.PermTeamManage, ID: "updateStatus", Tag: "Statuses",
			Summary: "Update a status", Body: dto.UpdateTeamStatusRequest{}, Status: 200, Response: dto.TeamStatusResponse{}},
		{Method: http.MethodDelete, Path: ws + "/teams/:teamId/statuses/:statusId", Scope: domain.PermTeamManage, ID: "deleteStatus", Tag: "Statuses",
			Summary: "Delete a status", Status: 200, Response: StatusResult{}},

		// Cycles: tokens read cycles; writes need an interactive session.
		{Method: http.MethodGet, Path: ws + "/teams/:teamId/cycles", Scope: domain.PermCyclesRead, ID: "listCycles", Tag: "Cycles",
			Summary: "List a team's cycles", Description: "Each cycle carries its progress counts.",
			Status: 200, Response: []dto.CycleResponse{}},
		{Method: http.MethodGet, Path: ws + "/teams/:teamId/cycles/velocity", Scope: domain.PermCyclesRead, ID: "getCycleVelocity", Tag: "Cycles",
			Summary: "Get velocity across a team's cycles", Status: 200, Response: []dto.VelocityPoint{}},
		{Method: http.MethodGet, Path: ws + "/teams/:teamId/cycles/:cycleId", Scope: domain.PermCyclesRead, ID: "getCycle", Tag: "Cycles",
			Summary: "Get a cycle", Status: 200, Response: dto.CycleResponse{}},
		{Method: http.MethodGet, Path: ws + "/teams/:teamId/cycles/:cycleId/burndown", Scope: domain.PermCyclesRead, ID: "getCycleBurndown", Tag: "Cycles",
			Summary: "Get a cycle's burndown", Status: 200, Response: []dto.BurndownPoint{}},

		// Issues
		{Method: http.MethodGet, Path: ws + "/issues", Scope: domain.PermIssuesRead, ID: "listIssues", Tag: "Issues",
			Summary: "List issues", Description: "Returns one page of the issues the user can see, with filters and sorting. See Pagination.",
			Query: dto.IssueFilterParams{}, Status: 200, Response: dto.ListResponse[dto.IssueResponse]{}},
		{Method: http.MethodPost, Path: ws + "/issues", Scope: domain.PermIssueCreate, ID: "createIssue", Tag: "Issues",
			Summary: "Create an issue", Description: "Creates an issue in a team. The description is Markdown or editor HTML and is sanitized. In a team with triage enabled the issue waits in triage.",
			Body: dto.CreateIssueRequest{}, Status: 201, Response: dto.IssueResponse{}},
		{Method: http.MethodPatch, Path: ws + "/issues/bulk", Scope: domain.PermIssueUpdate, ID: "bulkUpdateIssues", Tag: "Issues",
			Summary: "Update several issues", Body: dto.BulkUpdateIssueRequest{}, Status: 200, Response: BulkUpdateResult{}},
		{Method: http.MethodDelete, Path: ws + "/issues/bulk", Scope: domain.PermIssueDeleteOwn, ID: "bulkDeleteIssues", Tag: "Issues",
			Summary: "Delete several issues", Description: "Members delete only issues they created; owners and admins delete any.",
			Body: dto.BulkDeleteIssueRequest{}, Status: 200, Response: BulkDeleteResult{}},
		{Method: http.MethodGet, Path: ws + "/issues/:identifier", Scope: domain.PermIssuesRead, ID: "getIssue", Tag: "Issues",
			Summary: "Get an issue", Status: 200, Response: dto.IssueResponse{}},
		{Method: http.MethodPatch, Path: ws + "/issues/:identifier", Scope: domain.PermIssueUpdate, ID: "updateIssue", Tag: "Issues",
			Summary: "Update an issue", Description: "Changes the fields that are present.",
			Body: dto.UpdateIssueRequest{}, Status: 200, Response: dto.IssueResponse{}},
		{Method: http.MethodDelete, Path: ws + "/issues/:identifier", Scope: domain.PermIssueDeleteOwn, ID: "deleteIssue", Tag: "Issues",
			Summary: "Delete an issue", Description: "Members delete only issues they created; owners and admins delete any.",
			Status: 200, Response: StatusResult{}},
		{Method: http.MethodPost, Path: ws + "/issues/:identifier/duplicate", Scope: domain.PermIssueCreate, ID: "duplicateIssue", Tag: "Issues",
			Summary: "Duplicate an issue", Body: dto.DuplicateIssueRequest{}, Status: 201, Response: dto.IssueResponse{}},
		{Method: http.MethodPost, Path: ws + "/issues/:identifier/convert-to-project", Scope: domain.PermProjectManage, ID: "convertIssueToProject", Tag: "Issues",
			Summary: "Convert an issue to a project", Status: 201, Response: dto.ConvertIssueToProjectResponse{}},
		{Method: http.MethodGet, Path: ws + "/issues/:identifier/sub-issues", Scope: domain.PermIssuesRead, ID: "listSubIssues", Tag: "Issues",
			Summary: "List an issue's sub-issues", Status: 200, Response: []dto.IssueResponse{}},
		{Method: http.MethodPost, Path: ws + "/issues/:identifier/sub-issues", Scope: domain.PermIssueCreate, ID: "createSubIssue", Tag: "Issues",
			Summary: "Create a sub-issue", Body: dto.CreateSubIssueRequest{}, Status: 201, Response: dto.IssueResponse{}},
		{Method: http.MethodPost, Path: ws + "/issues/:identifier/sub-issues/bulk", Scope: domain.PermIssueCreate, ID: "bulkCreateSubIssues", Tag: "Issues",
			Summary: "Create several sub-issues", Body: dto.BulkCreateSubIssueRequest{}, Status: 201, Response: []dto.IssueResponse{}},
		{Method: http.MethodGet, Path: ws + "/issues/:identifier/history", Scope: domain.PermIssuesRead, ID: "getIssueHistory", Tag: "Issues",
			Summary: "Get an issue's change history", Status: 200, Response: []dto.IssueHistoryResponse{}},
		{Method: http.MethodPost, Path: ws + "/issues/:identifier/triage/accept", Scope: domain.PermIssueUpdate, ID: "acceptIssueTriage", Tag: "Issues",
			Summary: "Accept an issue from triage", Status: 200, Response: dto.IssueResponse{}},
		{Method: http.MethodPost, Path: ws + "/issues/:identifier/triage/decline", Scope: domain.PermIssueUpdate, ID: "declineIssueTriage", Tag: "Issues",
			Summary: "Decline an issue in triage", Status: 200, Response: dto.IssueResponse{}},

		// Comments
		{Method: http.MethodGet, Path: ws + "/issues/:identifier/comments", Scope: domain.PermCommentsRead, ID: "listIssueComments", Tag: "Comments",
			Summary: "List an issue's comments", Description: "Returns top-level comments, each with its replies.",
			Status: 200, Response: []dto.CommentResponse{}},
		{Method: http.MethodPost, Path: ws + "/issues/:identifier/comments", Scope: domain.PermIssueCreate, ID: "createIssueComment", Tag: "Comments",
			Summary: "Add a comment to an issue", Description: "The body is Markdown or editor HTML and is sanitized. Set parent_id to reply to a comment.",
			Body: dto.CreateCommentRequest{}, Status: 201, Response: dto.CommentResponse{}},
		{Method: http.MethodPost, Path: ws + "/issues/:identifier/comments/:commentId/resolve", Scope: domain.PermIssueUpdate, ID: "resolveIssueComment", Tag: "Comments",
			Summary: "Resolve a comment thread", Status: 200, Response: dto.CommentResponse{}},
		{Method: http.MethodPost, Path: ws + "/issues/:identifier/comments/:commentId/reopen", Scope: domain.PermIssueUpdate, ID: "reopenIssueComment", Tag: "Comments",
			Summary: "Reopen a comment thread", Status: 200, Response: dto.CommentResponse{}},

		// Issue relations
		{Method: http.MethodGet, Path: ws + "/issues/:identifier/relations", Scope: domain.PermIssuesRead, ID: "listIssueRelations", Tag: "Issue relations",
			Summary: "List an issue's relations", Status: 200, Response: []dto.IssueRelationResponse{}},
		{Method: http.MethodPost, Path: ws + "/issues/:identifier/relations", Scope: domain.PermIssueUpdate, ID: "createIssueRelation", Tag: "Issue relations",
			Summary: "Relate two issues", Body: dto.CreateIssueRelationRequest{}, Status: 201, Response: dto.IssueRelationResponse{}},
		{Method: http.MethodDelete, Path: ws + "/issues/:identifier/relations/:relationId", Scope: domain.PermIssueUpdate, ID: "deleteIssueRelation", Tag: "Issue relations",
			Summary: "Remove a relation", Status: 200, Response: StatusResult{}},

		// Labels
		{Method: http.MethodGet, Path: ws + "/labels", Scope: domain.PermLabelsRead, ID: "listLabels", Tag: "Labels",
			Summary: "List labels", Params: []Param{{Name: "project_id", Type: "string", Format: "uuid", Description: "Return workspace labels and the labels of this project only."}},
			Status: 200, Response: []dto.LabelResponse{}},
		{Method: http.MethodPost, Path: ws + "/labels", Scope: domain.PermLabelManage, ID: "createLabel", Tag: "Labels",
			Summary: "Create a label", Body: dto.CreateLabelRequest{}, Status: 201, Response: dto.LabelResponse{},
			Conflict: "A label with this name already exists in the same place."},
		{Method: http.MethodPatch, Path: ws + "/labels/:id", Scope: domain.PermLabelManage, ID: "updateLabel", Tag: "Labels",
			Summary: "Update a label", Body: dto.UpdateLabelRequest{}, Status: 200, Response: dto.LabelResponse{},
			Conflict: "A label with this name already exists in the same place."},
		{Method: http.MethodDelete, Path: ws + "/labels/:id", Scope: domain.PermLabelManage, ID: "deleteLabel", Tag: "Labels",
			Summary: "Delete a label", Status: 200, Response: StatusResult{}},

		// Projects
		{Method: http.MethodGet, Path: ws + "/projects", Scope: domain.PermProjectsRead, ID: "listProjects", Tag: "Projects",
			Summary: "List projects", Description: "Each project carries its progress counts.",
			Status: 200, Response: []dto.ProjectResponse{}},
		{Method: http.MethodPost, Path: ws + "/projects", Scope: domain.PermProjectManage, ID: "createProject", Tag: "Projects",
			Summary: "Create a project", Body: dto.CreateProjectRequest{}, Status: 201, Response: dto.ProjectResponse{}},
		{Method: http.MethodGet, Path: ws + "/projects/:id", Scope: domain.PermProjectsRead, ID: "getProject", Tag: "Projects",
			Summary: "Get a project", Status: 200, Response: dto.ProjectResponse{}},
		{Method: http.MethodPatch, Path: ws + "/projects/:id", Scope: domain.PermProjectManage, ID: "updateProject", Tag: "Projects",
			Summary: "Update a project", Body: dto.UpdateProjectRequest{}, Status: 200, Response: dto.ProjectResponse{}},
		{Method: http.MethodDelete, Path: ws + "/projects/:id", Scope: domain.PermProjectManage, ID: "deleteProject", Tag: "Projects",
			Summary: "Delete a project", Status: 200, Response: StatusResult{}},
		{Method: http.MethodGet, Path: ws + "/teams/:teamId/projects", Scope: domain.PermProjectsRead, ID: "listTeamProjects", Tag: "Projects",
			Summary: "List a team's projects", Status: 200, Response: []dto.ProjectResponse{}},

		// Views: tokens read views; writes need an interactive session.
		{Method: http.MethodGet, Path: ws + "/views", Scope: domain.PermViewsRead, ID: "listViews", Tag: "Views",
			Summary: "List saved views", Description: "Lists the user's own views and the views shared with the workspace.",
			Status: 200, Response: []dto.ViewResponse{}},
		{Method: http.MethodGet, Path: ws + "/views/:id", Scope: domain.PermViewsRead, ID: "getView", Tag: "Views",
			Summary: "Get a saved view", Status: 200, Response: dto.ViewResponse{}},

		// Webhooks
		{Method: http.MethodGet, Path: ws + "/webhooks", Scope: domain.PermWorkspaceManage, ID: "listWebhooks", Tag: "Webhooks",
			Summary: "List webhooks", Description: "The signing secret is never returned.",
			Status: 200, Response: []dto.WebhookResponse{}},
		{Method: http.MethodPost, Path: ws + "/webhooks", Scope: domain.PermWorkspaceManage, ID: "createWebhook", Tag: "Webhooks",
			Summary: "Create a webhook", Description: "The URL must be public: private, loopback and link-local addresses are rejected. The secret is stored encrypted and signs every delivery.",
			Body: dto.CreateWebhookRequest{}, Status: 201, Response: dto.WebhookResponse{}},
		{Method: http.MethodPatch, Path: ws + "/webhooks/:id", Scope: domain.PermWorkspaceManage, ID: "updateWebhook", Tag: "Webhooks",
			Summary: "Update a webhook", Body: dto.UpdateWebhookRequest{}, Status: 200, Response: dto.WebhookResponse{}},
		{Method: http.MethodDelete, Path: ws + "/webhooks/:id", Scope: domain.PermWorkspaceManage, ID: "deleteWebhook", Tag: "Webhooks",
			Summary: "Delete a webhook", Status: 200, Response: StatusResult{}},
	}
	return append(ops, wikiOperations()...)
}

// wikiOperations documents the wiki of a team and of a project, which share
// their handlers.
func wikiOperations() []Operation {
	var ops []Operation
	for _, scope := range []struct{ base, id, noun string }{
		{ws + "/teams/:teamId/wiki", "Team", "team"},
		{ws + "/projects/:id/wiki", "Project", "project"},
	} {
		read, write := domain.PermWikiRead, domain.PermWikiWrite
		cursor := func(format, limit string) []Param {
			return []Param{
				{Name: "before", Type: "string", Format: format, Description: "Return entries older than this value; pass next_before from the previous response."},
				{Name: "limit", Type: "integer", Description: limit},
			}
		}
		versionConflict := "The page changed since the given version. Read it again before saving."
		ops = append(ops,
			Operation{Method: http.MethodGet, Path: scope.base, Scope: read, ID: "list" + scope.id + "WikiPages", Tag: "Wiki",
				Summary: "List the pages of a " + scope.noun + " wiki", Description: "Pages are returned without their content. With search, only pages whose title or text contains it.",
				Params: []Param{{Name: "search", Type: "string", Description: "Text to find in the title or the text of a page, ignoring case."}},
				Status: 200, Response: []dto.WikiPageSummaryResponse{}},
			Operation{Method: http.MethodPost, Path: scope.base, Scope: write, ID: "create" + scope.id + "WikiPage", Tag: "Wiki",
				Summary: "Create a page in a " + scope.noun + " wiki", Description: "Content is Markdown or editor HTML and is sanitized.",
				Body: dto.CreateWikiPageRequest{}, Status: 201, Response: dto.WikiPageResponse{}},
			Operation{Method: http.MethodGet, Path: scope.base + "/tree", Scope: read, ID: "get" + scope.id + "WikiTree", Tag: "Wiki",
				Summary: "Get the page tree of a " + scope.noun + " wiki", Description: "Lists each page's parent, position and kind (page or folder).",
				Status: 200, Response: []dto.WikiTreeNodeResponse{}},
			Operation{Method: http.MethodPost, Path: scope.base + "/tree", Scope: write, ID: "create" + scope.id + "WikiTreePage", Tag: "Wiki",
				Summary: "Create a page or folder under a parent in a " + scope.noun + " wiki",
				Body:    dto.CreateWikiTreePageRequest{}, Status: 201, Response: dto.WikiPageResponse{}},
			Operation{Method: http.MethodGet, Path: scope.base + "/settings", Scope: read, ID: "get" + scope.id + "WikiSettings", Tag: "Wiki",
				Summary: "Get the settings of a " + scope.noun + " wiki", Status: 200, Response: dto.WikiSettingsResponse{}},
			Operation{Method: http.MethodPatch, Path: scope.base + "/settings", Scope: write, ID: "update" + scope.id + "WikiSettings", Tag: "Wiki",
				Summary: "Update the settings of a " + scope.noun + " wiki", Body: dto.WikiSettingsRequest{}, Status: 200, Response: dto.WikiSettingsResponse{}},
			Operation{Method: http.MethodGet, Path: scope.base + "/registry", Scope: read, ID: "get" + scope.id + "WikiRegistry", Tag: "Wiki",
				Summary: "Read the registry of a " + scope.noun + " wiki", Description: "The registry is a dated log, one page per UTC day, newest first.",
				Params: cursor("date", "Days to return, 1 to 31. Defaults to 7."), Status: 200, Response: dto.WikiRegistryFeedResponse{}},
			Operation{Method: http.MethodPost, Path: scope.base + "/registry", Scope: write, ID: "append" + scope.id + "WikiRegistry", Tag: "Wiki",
				Summary: "Add an entry to today's registry page of a " + scope.noun + " wiki",
				Body:    dto.WikiRegistryRequest{}, Status: 200, Response: dto.WikiPageResponse{}, Conflict: "The registry of this wiki is turned off."},
			Operation{Method: http.MethodGet, Path: scope.base + "/registry/overviews", Scope: read, ID: "list" + scope.id + "WikiOverviews", Tag: "Wiki",
				Summary: "List the registry overviews of a " + scope.noun + " wiki",
				Params:  cursor("date-time", "Overviews to return, 1 to 50. Defaults to 10."), Status: 200, Response: dto.WikiOverviewListResponse{}},
			Operation{Method: http.MethodGet, Path: scope.base + "/:pageId", Scope: read, ID: "get" + scope.id + "WikiPage", Tag: "Wiki",
				Summary: "Get a page of a " + scope.noun + " wiki", Status: 200, Response: dto.WikiPageResponse{}},
			Operation{Method: http.MethodPatch, Path: scope.base + "/:pageId", Scope: write, ID: "update" + scope.id + "WikiPage", Tag: "Wiki",
				Summary: "Update a page of a " + scope.noun + " wiki", Description: "Send the version you read; a stale version is rejected so concurrent edits are not lost.",
				Body: dto.UpdateWikiPageRequest{}, Status: 200, Response: dto.WikiPageResponse{}, Conflict: versionConflict},
			Operation{Method: http.MethodDelete, Path: scope.base + "/:pageId", Scope: write, ID: "delete" + scope.id + "WikiPage", Tag: "Wiki",
				Summary: "Delete a page of a " + scope.noun + " wiki", Status: 200, Response: StatusResult{}},
			Operation{Method: http.MethodGet, Path: scope.base + "/:pageId/versions", Scope: read, ID: "list" + scope.id + "WikiPageVersions", Tag: "Wiki",
				Summary: "List the earlier versions of a " + scope.noun + " wiki page", Status: 200, Response: []dto.WikiRevisionResponse{}},
			Operation{Method: http.MethodPost, Path: scope.base + "/:pageId/move", Scope: write, ID: "move" + scope.id + "WikiPage", Tag: "Wiki",
				Summary: "Move a page within a " + scope.noun + " wiki", Description: "Returns the new tree.",
				Body: dto.MoveWikiPageRequest{}, Status: 200, Response: []dto.WikiTreeNodeResponse{}},
			Operation{Method: http.MethodGet, Path: scope.base + "/:pageId/publish", Scope: read, ID: "get" + scope.id + "WikiPagePublication", Tag: "Wiki",
				Summary: "Get how a " + scope.noun + " wiki page is published", Status: 200, Response: dto.WikiPublicationResponse{}},
			Operation{Method: http.MethodPut, Path: scope.base + "/:pageId/publish", Scope: write, ID: "publish" + scope.id + "WikiPage", Tag: "Wiki",
				Summary: "Publish or unpublish a " + scope.noun + " wiki page", Description: "Mode is off, internal or public. Only roles allowed by the workspace may publish publicly.",
				Body: dto.WikiPublishRequest{}, Status: 200, Response: dto.WikiPublicationResponse{}, Conflict: "Pages of a private team cannot be published publicly."},
		)
	}
	return ops
}

// InternalRoutes classifies every token-accessible route that the reference
// leaves out. A new token-accessible route must be added to Operations or
// here before the route test passes.
func InternalRoutes() []InternalRoute {
	const (
		machines    = "Dev machines and coding agents are operated from the app; their API is not stable."
		integration = "Integration setup used by the settings pages."
		ai          = "Calls the workspace's AI model; not part of the data API."
		app         = "Serves the web app's own screens."
		binary      = "Serves or stores files; a stable upload API is not defined yet."
	)
	return []InternalRoute{
		{Path: ws + "/dev-machine", Prefix: true, Reason: machines},
		{Path: ws + "/agent-", Prefix: true, Reason: machines},
		{Path: ws + "/workflows", Prefix: true, Reason: machines},
		{Path: ws + "/github", Prefix: true, Reason: integration},
		{Path: ws + "/invite-links", Prefix: true, Reason: integration},
		{Path: ws + "/ai-settings", Prefix: true, Reason: ai},
		{Path: ws + "/analytics", Prefix: true, Reason: app},
		{Path: ws + "/issue-templates", Prefix: true, Reason: app},
		{Method: http.MethodGet, Path: ws + "/issues/:identifier/github", Reason: integration},
		{Method: http.MethodPost, Path: ws + "/issues/expand-description", Reason: ai},
		{Method: http.MethodPost, Path: ws + "/issues/:identifier/expand-description", Reason: ai},
		{Method: http.MethodPost, Path: ws + "/teams/:teamId/wiki/:pageId/rework", Reason: ai},
		{Method: http.MethodPost, Path: ws + "/projects/:id/wiki/:pageId/rework", Reason: ai},
		{Method: http.MethodPost, Path: ws + "/teams/:teamId/wiki/registry/overviews", Reason: ai},
		{Method: http.MethodPost, Path: ws + "/projects/:id/wiki/registry/overviews", Reason: ai},
		{Method: http.MethodGet, Path: ws + "/export", Reason: "Workspace transfer archive; see the self-hosting guide."},
		{Method: http.MethodGet, Path: ws + "/favorites", Reason: app},
		{Method: http.MethodGet, Path: "/api/preferences", Reason: app},
		{Method: http.MethodGet, Path: "/api/notifications", Reason: app},
		{Method: http.MethodGet, Path: "/api/users/:userId/avatar/:file", Reason: binary},
		{Method: http.MethodGet, Path: ws + "/teams/:teamId/icon/:file", Reason: binary},
		{Method: http.MethodPost, Path: ws + "/upload", Reason: binary},
		{Method: http.MethodGet, Path: ws + "/assets/:assetId", Reason: binary},
	}
}
