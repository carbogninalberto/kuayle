package domain

const (
	PermWorkspaceManage  = "workspace:manage"
	PermTeamManage       = "team:manage"
	PermIssueCreate      = "issue:create"
	PermIssueRead        = "issue:read"
	PermIssueUpdate      = "issue:update"
	PermIssueDelete      = "issue:delete"
	PermIssueDeleteOwn   = "issue:delete_own"
	PermProjectManage    = "project:manage"
	PermLabelManage      = "label:manage"
	PermMemberInvite     = "member:invite"
	PermCycleManage      = "cycle:manage"
	PermViewManage       = "view:manage"
	PermDevMachineRead   = "dev_machine:read"
	PermDevMachineCreate = "dev_machine:create"
	PermDevMachineManage = "dev_machine:manage"
	PermDevMachineAdmin  = "dev_machine:admin"
)

var RolePermissions = map[string][]string{
	RoleOwner: {
		PermWorkspaceManage, PermTeamManage, PermIssueCreate, PermIssueRead,
		PermIssueUpdate, PermIssueDelete, PermIssueDeleteOwn, PermProjectManage, PermLabelManage,
		PermMemberInvite, PermCycleManage, PermViewManage,
		PermDevMachineRead, PermDevMachineCreate, PermDevMachineManage, PermDevMachineAdmin,
	},
	RoleAdmin: {
		PermTeamManage, PermIssueCreate, PermIssueRead, PermIssueUpdate,
		PermIssueDelete, PermIssueDeleteOwn, PermProjectManage, PermLabelManage, PermMemberInvite,
		PermCycleManage, PermViewManage,
		PermDevMachineRead, PermDevMachineCreate, PermDevMachineManage, PermDevMachineAdmin,
	},
	RoleMember: {
		PermIssueCreate, PermIssueRead, PermIssueUpdate, PermIssueDeleteOwn, PermProjectManage,
		PermLabelManage, PermCycleManage, PermViewManage,
		PermDevMachineRead, PermDevMachineCreate, PermDevMachineManage,
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
