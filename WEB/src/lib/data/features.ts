/**
 * Content registry for feature detail pages.
 * Each entry maps to /features/[slug].
 */
import type { ContentRegistry } from './routes';

export const features: ContentRegistry = {
	'issue-tracking': {
		slug: 'issue-tracking',
		title: 'Issue Tracking — Kuayle',
		description:
			'Self-hosted issue tracking with multiple assignees, sub-issues, relations, labels, comments, history, templates, and team-specific statuses.',
		heading: 'Issue Tracking',
		intro:
			'Kuayle supports the core path from issue creation through triage, assignment, planning and completion. Multiple people can be assigned to the same issue, and each team can define its own statuses.',
		sections: [
			{
				heading: 'Rich issue model',
				body: 'Issues include a rich-text description, priority, due date, multiple assignees, labels, comments and change history. Sub-issues appear in a tree, and relations record blocked-by, blocking, duplicate and related links.',
				list: [
					'Multi-assignee support — no single-owner assumptions',
					'Sub-issues with tree view and counter badges',
					'Blocking/blocked, duplicate, and related issue links',
					'Rich text editor with code blocks, mentions, and task lists',
					'File uploads with S3-compatible storage'
				]
			},
			{
				heading: 'Custom statuses and workflows',
				body: 'Each team defines its own statuses and workflow. Move cards on the team board or press S from an open issue to choose a status. Teams in the same workspace can use different status sets.',
				list: [
					'Per-team custom statuses',
					'Configurable auto-transitions via GitHub events',
					'Bulk issue updates from list views'
				]
			},
			{
				heading: 'Issue templates',
				body: 'Workspace issue templates can prefill title, description, status, priority, assignee and labels in the issue creation dialog. Templates may be limited to a specific team.',
				list: [
					'Workspace-level template management',
					'Prefilled issue fields in the creation dialog',
					'Optional team scope'
				]
			},
			{
				heading: 'Public sharing',
				body: 'Create token-based, read-only links scoped to a workspace, team, project or saved view. Recipients can browse the issues exposed by that scope without a Kuayle account.',
				list: [
					'Workspace, team, project and view scopes',
					'Optional expiry and description visibility',
					'No account required to open the link'
				]
			}
		]
	},

	cycles: {
		slug: 'cycles',
		title: 'Cycles — Kuayle',
		description:
			'Time-box work with cycle dates, progress statistics, burndown and velocity charts, retrospectives, and optional carry-over to the next cycle.',
		heading: 'Cycles',
		intro:
			'Cycles group a team’s issues between a start and end date. Kuayle calculates progress, burndown and historical velocity from issue status data.',
		sections: [
			{
				heading: 'Cycle planning',
				body: 'Create a cycle with start and end dates, goals and an optional description. Add issues from the team backlog and review the issue list from the cycle page.',
				list: [
					'Explicit start and end dates',
					'Goals and retrospective fields',
					'Issue assignment to active or upcoming cycles'
				]
			},
			{
				heading: 'Burndown and velocity charts',
				body: 'The cycle page plots completed and remaining issues across the cycle date range. Velocity history compares completed work across recent cycles.',
				list: [
					'Burndown based on issue completion history',
					'Velocity history for up to 20 cycles',
					'Cycle totals for completed and cancelled issues'
				]
			},
			{
				heading: 'Carry over unfinished work',
				body: 'When completing a cycle, choose whether incomplete issues should move to the next upcoming cycle. If no upcoming cycle exists, the issues remain in the completed cycle.',
				list: [
					'Carry-over is selected when the cycle is completed',
					'Requires an upcoming cycle for the same team',
					'Optional retrospective recorded on completion'
				]
			}
		]
	},

	projects: {
		slug: 'projects',
		title: 'Projects — Kuayle',
		description:
			'Group issues from multiple teams in a project, track completion, set project dates, and review issue due dates in a Gantt view.',
		heading: 'Projects',
		intro:
			'Projects group issues from across a workspace. Each project has a status, a lead, start and target dates, an issue list and a Gantt view.',
		sections: [
			{
				heading: 'Cross-team coordination',
				body: 'A project can contain issues from multiple teams in the workspace. The project page provides one list and timeline for that cross-team set of issues.',
				list: [
					'Multi-team issue aggregation',
					'Single source of truth for project status',
					'No team-boundary limitations'
				]
			},
			{
				heading: 'Gantt view',
				body: 'The Gantt view plots each issue from its creation date to its due date. Issues without a due date are shown separately, and cycle date ranges appear as background bands.',
				list: [
					'Filter issues with or without due dates',
					'Issue bars colored by status',
					'Cycle ranges shown on the project timeline'
				]
			},
			{
				heading: 'Progress tracking',
				body: 'Project progress is calculated from the number of completed, cancelled and total issues. The project page displays both the counts and a progress bar.',
				list: [
					'Automatic progress calculation',
					'Project-level status overview',
					'Filter and sort within projects'
				]
			}
		]
	},

	'github-integration': {
		slug: 'github-integration',
		title: 'GitHub Integration — Kuayle',
		description:
			'Connect repositories with a GitHub App, match issue identifiers in development activity, and configure status changes for branch and pull-request events.',
		heading: 'GitHub Integration',
		intro:
			'Kuayle connects each workspace to GitHub through an App. Repository events are matched to Kuayle issue identifiers and recorded in the issue activity feed.',
		sections: [
			{
				heading: 'Auto-linking PRs, branches, and commits',
				body: 'Include an identifier such as ENG-123 in a branch name, pull-request title or body, or commit message. Kuayle matches the identifier case-insensitively and records the GitHub activity on the issue.',
				list: [
					'Works with branch names, PR titles, and commit messages',
					'Case-insensitive issue key matching',
					'Linked GitHub activity in the issue detail view'
				]
			},
			{
				heading: 'Auto-transitions',
				body: 'GitHub automation rules can move a matched issue when a branch is created, a pull request opens or a pull request merges. Default rules target In Progress, In Review and Done; each rule can be enabled, disabled or mapped to another status.',
				list: [
					'Branch created → In Progress',
					'PR opened → In Review',
					'PR merged → Done',
					'Fully configurable transition rules'
				]
			},
			{
				heading: 'Self-configuring GitHub App',
				body: 'In self-hosted mode, workspace settings start GitHub’s App Manifest flow with the required permissions, callback and webhook URL. After GitHub returns the manifest code, Kuayle exchanges it for App credentials and stores them encrypted.',
				list: [
					'Per-workspace GitHub App in self-hosted mode',
					'AES-256-GCM encryption for stored App credentials',
					'Webhook proxy or tunnel required for private networks'
				]
			},
			{
				heading: 'Webhook and WebSocket flow',
				body: 'Kuayle verifies GitHub webhook signatures, processes supported events and broadcasts refresh events to connected clients in the workspace. Open pages then reload the affected issue data.',
				list: [
					'HMAC-SHA256 verification of webhook payloads',
					'Workspace WebSocket notification after processing',
					'Client refresh of affected issue data'
				]
			}
		]
	},

	'real-time-sync': {
		slug: 'real-time-sync',
		title: 'Real-Time Sync — Kuayle',
		description:
			'Issue, comment, cycle, view and GitHub events are sent to connected workspace clients over WebSockets.',
		heading: 'Real-Time Sync',
		intro:
			'Kuayle uses a workspace-scoped WebSocket connection to announce changes. The frontend updates local issue data or reloads the affected resource when it receives a supported event.',
		sections: [
			{
				heading: 'How it works',
				body: 'The backend keeps a WebSocket connection for each connected client and groups connections by workspace. Issue and comment services broadcast typed events after successful writes; some events carry the updated record and others request a client-side refresh.',
				list: [
					'Workspace-scoped connections',
					'Typed events for issues, comments, cycles, views and GitHub activity',
					'User-targeted events for notifications'
				]
			},
			{
				heading: 'Issue presence and updates',
				body: 'When users open the same issue, the presence channel reports who is viewing it. Issue and comment events update the open issue view. Kuayle does not provide character-by-character collaborative text editing.',
				list: [
					'Join, leave and presence-sync events',
					'Issue and comment refresh events',
					'No collaborative rich-text merge engine'
				]
			},
			{
				heading: 'Notification delivery',
				body: 'Notification events can target a specific workspace member over the same WebSocket connection. The inbox supports read and unread state, snoozing, archiving and bulk mark-as-read.',
				list: [
					'Real-time notification delivery',
					'Inbox with snooze, read status, and archive',
					'In-app delivery; email notifications are not implemented'
				]
			}
		]
	},

	'keyboard-shortcuts': {
		slug: 'keyboard-shortcuts',
		title: 'Keyboard Shortcuts — Kuayle',
		description:
			'Use documented shortcuts for issue creation, workspace navigation, search, issue properties and triage decisions.',
		heading: 'Keyboard Shortcuts',
		intro:
			'Kuayle provides global shortcuts and context-specific issue shortcuts. The command palette searches issues and opens common workspace destinations.',
		sections: [
			{
				heading: 'Single-key actions',
				body: 'Press C from the workspace shell to create an issue. In the full issue view, S opens status, P priority, A assignees and L labels. Shortcuts are ignored while typing in form fields.',
				list: [
					'C — Create issue',
					'S / P — Status or priority in an open issue',
					'A / L — Assignees or labels in an open issue',
					'G then I / M / P / S — Inbox, My Issues, Projects or Settings'
				]
			},
			{
				heading: 'Command palette',
				body: 'Press Cmd+K on macOS or Ctrl+K elsewhere to open the command palette. It searches issue titles, identifiers and descriptions, and lists commands for issue creation, teams, Inbox, My Issues, Projects and Settings.',
				list: [
					'Cmd+K / Ctrl+K to open',
					'Substring search across issue fields',
					'Keyboard selection with arrow keys and Enter',
					'Issue creation and workspace destinations'
				]
			},
			{
				heading: 'Triage shortcuts',
				body: 'The team triage queue uses J and K to move through incoming issues. Press 1 to accept the selected issue or 3 to decline it.',
				list: [
					'J / K — next or previous issue',
					'1 — accept selected issue',
					'3 — decline selected issue'
				]
			}
		]
	},

	'views-and-triage': {
		slug: 'views-and-triage',
		title: 'Views & Triage — Kuayle',
		description:
			'Saved filters as shareable views, a triage inbox for incoming work, hierarchical labels, and per-team custom workflows.',
		heading: 'Views & Triage',
		intro:
			'Kuayle combines reusable issue filters, a team triage queue, hierarchical labels and team-specific statuses. These tools keep intake separate from planned work.',
		sections: [
			{
				heading: 'Saved views',
				body: 'Save issue filters as personal, team or workspace views. Views retain their filter definition and layout, and a saved view can be exposed through a read-only public link.',
				list: [
					'Complex filters: status, assignee, priority, labels, due date',
					'Personal, team, and workspace view scoping',
					'List and board layouts',
					'Shareable — views can be made public with a link'
				]
			},
			{
				heading: 'Team triage queue',
				body: 'Teams with triage enabled place incoming issues in a separate queue. Review the selected issue, then accept it into the team workflow or decline it. J/K changes the selection; 1 accepts and 3 declines.',
				list: [
					'Triage enabled per team',
					'Accept or decline decisions',
					'J/K, 1 and 3 keyboard controls'
				]
			},
			{
				heading: 'Hierarchical labels',
				body: 'Workspace labels can have parent-child relationships, such as a Bug parent with UI Bug and Backend Bug children. Labels support soft deletion.',
				list: [
					'Parent-child label hierarchy',
					'Workspace-scoped labels',
					'Soft delete',
					'Default labels created with a new workspace'
				]
			},
			{
				heading: 'Per-team workflows',
				body: 'Each team can define its own statuses and triage settings. Engineering may use "In Progress → In Review → Done" while Support uses "New → Triaged → Resolved." Both coexist in the same workspace.',
				list: [
					'Custom statuses per team',
					'Team-specific triage settings',
					'Independent workflows in shared workspace'
				]
			}
		]
	},

	'teams-and-access-control': {
		slug: 'teams-and-access-control',
		title: 'Teams & Access Control — Kuayle',
		description:
			'Multiple teams per workspace, owner/admin/member/guest roles, and read-only public links for sharing outside the team.',
		heading: 'Teams & Access Control',
		intro:
			'Kuayle applies four predefined roles at workspace level, then organizes members and issues into teams. Public links provide separate read-only access to selected issue sets.',
		sections: [
			{
				heading: 'Workspace roles',
				body: 'Every workspace member is an Owner, Admin, Member or Guest. Owners control workspace settings. Admins manage teams, members and shared resources. Members create and update work. Guests receive read-only issue access.',
				list: [
					'Owner — full workspace control',
					'Admin — manage teams, members and workspace resources',
					'Member — create, edit, and triage issues',
					'Guest — read-only access to assigned teams'
				]
			},
			{
				heading: 'Teams with custom workflows',
				body: 'A team is a group of workspace members with their own status definitions, triage settings, and issue scope. You can have an Engineering team, a Design team, and a Support team, each operating with the workflow that fits them best.',
				list: [
					'Multiple teams per workspace',
					'Custom statuses per team',
					'Team-specific triage settings',
					'Team-scoped views and filters'
				]
			},
			{
				heading: 'Public sharing',
				body: 'Create token-based public links for a workspace, team, project or saved view. The link exposes a read-only issue list and issue detail without requiring an account.',
				list: [
					'Workspace, team, project or view scope',
					'Optional expiry date',
					'No Kuayle account required for recipients'
				]
			}
		]
	},

	'analytics-insights': {
		slug: 'analytics-insights',
		title: 'Analytics & Insights — Kuayle',
		description:
			'Workspace and team overviews, burn-up trends, and configurable issue insights computed from durable lifecycle events.',
		heading: 'Analytics & Insights',
		intro:
			'Kuayle records durable issue lifecycle events and computes analytics from them. The Insights page offers workspace and team overviews, burn-up trends, and a configurable insight builder.',
		sections: [
			{
				heading: 'Workspace and team overviews',
				body: 'The overview summarizes total, completed, open and overdue issues alongside project and member counts. A team scope narrows the same metrics to one team.',
				list: [
					'Total, completed, open and overdue issue counts',
					'Project and member counts',
					'Workspace-wide or team-scoped views'
				]
			},
			{
				heading: 'Burn-up trends',
				body: 'The burn-up chart plots created and completed issues over a selected date range, so scope growth is visible next to completed work.',
				list: [
					'Created and completed series per date',
					'Scope line for total work',
					'Configurable date range'
				]
			},
			{
				heading: 'Configurable issue insights',
				body: 'The insight builder groups issues by a chosen slice and measures them by count, age, lead time, cycle time or triage time. Results can be segmented for comparison.',
				list: [
					'Measures: issue count, age, lead time, cycle time, triage time',
					'Slices: status, priority, assignee, team, project, cycle, label',
					'Optional segment and date range',
					'Backed by durable lifecycle events, not estimates'
				]
			}
		]
	},

	'dev-machines': {
		slug: 'dev-machines',
		title: 'Dev Machines — Kuayle',
		description:
			'Opt-in multi-container development environments with code-server, a native terminal, agent providers, an in-browser Chrome, and issue worktrees.',
		heading: 'Dev Machines',
		intro:
			'Dev Machines are an opt-in self-hosted subsystem for agentic coding. Each machine is a set of cooperating containers on an isolated network — a developer environment, agents, a browser, and an activity collector — routed through an authenticated gateway. The subsystem is disabled by default.',
		sections: [
			{
				heading: 'A full environment per machine',
				body: 'The developer container runs code-server and a tmux-backed terminal that the Kuayle UI renders natively with xterm. A separate browser container provides Chrome over KasmVNC, and an app-preview service exposes the dev server. No host ports are published; access goes through the Machine Gateway with one-time launch tickets.',
				list: [
					'code-server IDE and native xterm terminal',
					'In-browser Chrome via KasmVNC',
					'Dev-server app preview',
					'Authenticated wildcard routing — no public ports'
				]
			},
			{
				heading: 'Agent providers',
				body: 'Machines can run Claude Code, OpenCode, Codex or an admin-configured custom CLI, either interactively or as bounded autonomous runs. Provider output is normalized into a common result model with a summary, changed files, commits, branch and pull-request URL.',
				list: [
					'Claude Code, OpenCode, Codex and custom CLI providers',
					'Interactive and autonomous modes',
					'Normalized run results with commits and PR links',
					'Scoped secrets delivered through tmpfs, redacted in logs'
				]
			},
			{
				heading: 'Issue worktrees and environments',
				body: 'Machines start generic and attach issue worktrees when work begins. Repository and environment defaults resolve from the issue, then project, team and workspace. Owner/admins can snapshot a customized builder machine into an immutable local Development Environment image.',
				list: [
					'Idempotent issue worktrees under /workspace/tasks/{issue-key}',
					'One repository affinity per machine',
					'Scoped defaults: issue → project → team → workspace',
					'Environment Builder snapshots as immutable local OCI images'
				]
			},
			{
				heading: 'Policy, sizes and lifecycle',
				body: 'Workspace policy controls concurrency, providers, repositories, maximum runtime and idle pause (default 240 minutes). A per-machine Keep running switch bypasses idle pause. Machines pause, stop and tear down as separate lifecycle operations recorded durably in PostgreSQL.',
				list: [
					'Small 2 vCPU/4 GB, medium 4 vCPU/8 GB, large 8 vCPU/16 GB',
					'Idle pause with per-machine keep-running bypass',
					'Filesystem, shell, Git, browser and agent activity events',
					'Disabled by default; requires a separate wildcard domain'
				]
			}
		]
	}
};
