package domain

const (
	PermWorkspaceManage = "workspace:manage"
	PermTeamManage      = "team:manage"
	PermIssueCreate     = "issue:create"
	PermIssueRead       = "issue:read"
	PermIssueUpdate     = "issue:update"
	PermIssueDelete     = "issue:delete"
	PermProjectManage   = "project:manage"
	PermLabelManage     = "label:manage"
	PermMemberInvite    = "member:invite"
	PermCycleManage     = "cycle:manage"
	PermViewManage      = "view:manage"
)

var RolePermissions = map[string][]string{
	RoleOwner: {
		PermWorkspaceManage, PermTeamManage, PermIssueCreate, PermIssueRead,
		PermIssueUpdate, PermIssueDelete, PermProjectManage, PermLabelManage,
		PermMemberInvite, PermCycleManage, PermViewManage,
	},
	RoleAdmin: {
		PermTeamManage, PermIssueCreate, PermIssueRead, PermIssueUpdate,
		PermIssueDelete, PermProjectManage, PermLabelManage, PermMemberInvite,
		PermCycleManage, PermViewManage,
	},
	RoleMember: {
		PermIssueCreate, PermIssueRead, PermIssueUpdate, PermProjectManage,
		PermLabelManage, PermCycleManage, PermViewManage,
	},
	RoleGuest: {
		PermIssueRead,
	},
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
