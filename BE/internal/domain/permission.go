package domain

const (
	PermWorkspaceTransfer = "workspace:transfer"
	PermWorkspaceManage   = "workspace:manage"
	PermTeamManage        = "team:manage"
	PermIssueCreate       = "issue:create"
	PermIssuesRead        = "issues:read"
	PermIssueUpdate       = "issue:update"
	PermIssueDelete       = "issue:delete"
	PermIssueDeleteOwn    = "issue:delete_own"
	PermProjectManage     = "project:manage"
	PermLabelManage       = "label:manage"
	PermMemberInvite      = "member:invite"
	PermCycleManage       = "cycle:manage"
	PermViewManage        = "view:manage"
	PermDevMachineRead    = "dev_machine:read"
	PermDevMachineCreate  = "dev_machine:create"
	PermDevMachineManage  = "dev_machine:manage"
	PermDevMachineAdmin   = "dev_machine:admin"
)

// Read permission codes for read routes. Every workspace role holds all of
// them (today any member can read everything), so annotating read routes with
// RequirePermission does not change JWT authorization; the codes exist so
// personal access tokens can be limited to a subset.
const (
	PermCommentsRead      = "comments:read"
	PermProjectsRead      = "projects:read"
	PermCyclesRead        = "cycles:read"
	PermLabelsRead        = "labels:read"
	PermTeamsRead         = "teams:read"
	PermMembersRead       = "members:read"
	PermTemplatesRead     = "templates:read"
	PermViewsRead         = "views:read"
	PermAnalyticsRead     = "analytics:read"
	PermNotificationsRead = "notifications:read"
	PermWorkspacesRead    = "workspaces:read"
	PermAccountRead       = "account:read"
)

var readPermissions = []string{
	PermIssuesRead, PermCommentsRead, PermProjectsRead, PermCyclesRead,
	PermLabelsRead, PermTeamsRead, PermMembersRead, PermTemplatesRead,
	PermViewsRead, PermAnalyticsRead, PermNotificationsRead, PermWorkspacesRead,
	PermAccountRead,
}

var RolePermissions = map[string][]string{
	RoleOwner: append([]string{
		PermWorkspaceManage, PermWorkspaceTransfer, PermTeamManage, PermIssueCreate,
		PermIssueUpdate, PermIssueDelete, PermIssueDeleteOwn, PermProjectManage, PermLabelManage,
		PermMemberInvite, PermCycleManage, PermViewManage,
		PermDevMachineRead, PermDevMachineCreate, PermDevMachineManage, PermDevMachineAdmin,
	}, readPermissions...),
	RoleAdmin: append([]string{
		PermWorkspaceTransfer, PermTeamManage, PermIssueCreate, PermIssueUpdate,
		PermIssueDelete, PermIssueDeleteOwn, PermProjectManage, PermLabelManage, PermMemberInvite,
		PermCycleManage, PermViewManage,
		PermDevMachineRead, PermDevMachineCreate, PermDevMachineManage, PermDevMachineAdmin,
	}, readPermissions...),
	RoleMember: append([]string{
		PermIssueCreate, PermIssueUpdate, PermIssueDeleteOwn, PermProjectManage,
		PermLabelManage, PermCycleManage, PermViewManage,
		PermDevMachineRead, PermDevMachineCreate, PermDevMachineManage,
	}, readPermissions...),
	RoleGuest: append([]string{}, readPermissions...),
}

func HasPermission(role string, permission string) bool {
	perms, ok := RolePermissions[role]
	if !ok {
		return false
	}
	for _, p := range perms {
		if p == permission {
			return true
		}
	}
	return false
}
