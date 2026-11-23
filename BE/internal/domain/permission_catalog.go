package domain

type PermissionGroup struct {
	Area        string           `json:"area"`
	Permissions []PermissionInfo `json:"permissions"`
}

type PermissionInfo struct {
	Code string `json:"code"`
	Hint string `json:"hint"`
}

// PermissionGroups describes the permission codes shared by roles and PATs.
var PermissionGroups = []PermissionGroup{
	{"issues", []PermissionInfo{
		{PermIssuesRead, "Read visible issues and their history."},
		{PermCommentsRead, "Read discussions on visible issues."},
		{PermIssueCreate, "Create issues, comments and issue templates."},
		{PermIssueUpdate, "Edit issues and change their status."},
		{PermIssueDeleteOwn, "Delete or restore issues you created."},
		{PermIssueDelete, "Moderate comments and extend issue deletion to other authors."},
		{PermTemplatesRead, "Read issue templates."},
		{PermAssetsRead, "Open visible attachments."},
	}},
	{"projects", []PermissionInfo{
		{PermProjectsRead, "Read projects."},
		{PermProjectManage, "Create, edit and delete projects."},
	}},
	{"cycles", []PermissionInfo{
		{PermCyclesRead, "Read cycles and their reports."},
		{PermCycleManage, "Create, edit, complete and delete cycles."},
	}},
	{"labels", []PermissionInfo{
		{PermLabelsRead, "Read labels."},
		{PermLabelManage, "Create, edit and delete labels."},
	}},
	{"views", []PermissionInfo{
		{PermViewsRead, "Read saved views."},
		{PermViewManage, "Create, edit and delete saved views."},
		{PermAnalyticsRead, "Read workspace analytics."},
	}},
	{"wiki", []PermissionInfo{
		{PermWikiRead, "Read visible wiki pages."},
		{PermWikiWrite, "Create, edit and organize wiki pages."},
	}},
	{"members", []PermissionInfo{
		{PermTeamsRead, "Read visible teams and their statuses."},
		{PermTeamManage, "Manage teams, their statuses and membership."},
		{PermMembersRead, "Read member lists."},
		{PermMemberInvite, "Invite people and manage workspace memberships."},
	}},
	{"dev_machines", []PermissionInfo{
		{PermDevMachineRead, "Read available development machines and services."},
		{PermDevMachineCreate, "Create development machines."},
		{PermDevMachineManage, "Operate development machines and launch services."},
		{PermDevMachineAdmin, "Administer workspace machine policies and infrastructure."},
	}},
	{"agents_and_workflows", []PermissionInfo{
		{PermAgentTasksRead, "Read visible agent tasks, history and runs."},
		{PermWorkflowsRead, "Read agent profiles and workflow definitions."},
	}},
	{"settings", []PermissionInfo{
		{PermWorkspacesRead, "Read workspace details."},
		{PermWorkspaceManage, "Manage workspace integrations and settings."},
		{PermWorkspaceTransfer, "Export workspace data."},
		{PermAccountRead, "Read your account and preferences."},
		{PermAccountWrite, "Change your own account state."},
		{PermNotificationsRead, "Read your notifications."},
		{PermNotificationsWrite, "Update or archive your notifications."},
	}},
}
