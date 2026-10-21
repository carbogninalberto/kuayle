/**
 * Content registry for feature detail pages.
 * Each entry maps to /features/[slug].
 */
import type { ContentRegistry } from './routes';
import { DEV_MACHINES_RELEASE_STATUS } from '$lib/config/releases';

export const features: ContentRegistry = {
	'issue-tracking': {
		slug: 'issue-tracking',
		title: 'Issue Tracking — Kuayle',
		description:
			'Self-hosted issue tracking with multiple assignees, sub-issues, relations, labels, comments, version history, templates, and team-specific statuses.',
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
					'File attachments, image resizing and screenshots pasted into the create dialog',
					'Local or S3-compatible upload storage'
				]
			},
			{
				heading: 'Issue version history',
				body: 'Open Version history from either issue view to browse recorded title and description edits in chronological order. Each entry shows the editor when known, the time and the complete before and after values; descriptions can be compared as rendered content or HTML source. History is captured in the same database transaction as the edit, so failed saves leave no entry.',
				list: [
					'Title and description revisions with author and timestamp',
					'Rendered and HTML source views for descriptions',
					'Edits made before history capture cannot be reconstructed',
					'Read-only: restoring an earlier version is not included'
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
				heading: 'Optional AI assistance',
				body: 'Owners can connect one OpenAI-compatible provider per workspace to expand or rewrite issue descriptions. Workspace and team issue-copy prompts format an issue for pasting into an external assistant. Nothing is sent to a provider until one is configured, and AI expansion is disabled in workspaces that use private teams.',
				list: [
					'Any OpenAI-compatible API: base URL, model and encrypted API key',
					'Customizable description expansion prompt',
					'Workspace and per-team issue copy prompts',
					'Interface available in English, Italian, Simplified and Traditional Chinese'
				]
			},
			{
				heading: 'Public sharing',
				body: 'Create token-based, read-only links scoped to a workspace, team, project or saved view. Recipients can browse the issues exposed by that scope without a Kuayle account.',
				list: [
					'Workspace, team, project and view scopes',
					'Optional expiry and description visibility',
					'No account required to open the link',
					'Private-team content is never exposed through a public scope'
				]
			}
		]
	},

	cycles: {
		slug: 'cycles',
		title: 'Cycles — Kuayle',
		description:
			'Time-box work with cycles: a full issue list with filters, board view and bulk actions, a searchable issue picker, burndown and velocity charts, and optional carry-over.',
		heading: 'Cycles',
		intro:
			'Cycles group a team’s issues between a start and end date. Each cycle page works like the team issue list, and Kuayle calculates progress, burndown and historical velocity from issue status data.',
		sections: [
			{
				heading: 'Cycle planning',
				body: 'Create a cycle with start and end dates, goals and an optional description. The actions menu on each cycle offers edit, start, complete and delete; deletion asks for confirmation. Guests can view cycles but do not see create, edit or delete actions.',
				list: [
					'Explicit start and end dates, editable from the cycle page',
					'Goals and retrospective fields',
					'Edit, start, complete and delete from the cycle actions menu'
				]
			},
			{
				heading: 'A full issue list for every cycle',
				body: 'The cycle detail page offers the same grouping, filters, list and board layouts, bulk actions and load-more as the team issues page. Issues open on the full issue page.',
				list: [
					'Add issues: a searchable multi-select picker across the whole team',
					'New issue is prefilled with the current cycle',
					'Remove from cycle keeps the issue and only clears its cycle',
					'Failed additions can be retried individually'
				]
			},
			{
				heading: 'Burndown and velocity charts',
				body: 'The team cycles page plots completed and remaining issues across the selected cycle’s date range. Velocity history compares completed work across recent cycles.',
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
			'Group issues from multiple teams in a project, work them in list, board or Gantt layouts, add issues with a searchable picker and track completion.',
		heading: 'Projects',
		intro:
			'Projects group issues from across a workspace. Each project has a status, a lead, start and target dates, and an issue list with list, board and Gantt layouts.',
		sections: [
			{
				heading: 'Cross-team coordination',
				body: 'A project can contain issues from multiple public teams in the workspace. A project that belongs to a private team inherits that team’s visibility, and relationships involving a private team must stay within it.',
				list: [
					'Multi-team issue aggregation for public teams',
					'Project status, lead, start and target dates',
					'Private-team projects follow team membership'
				]
			},
			{
				heading: 'The same issue list as your team',
				body: 'The project page offers the grouping, filters, list and board layouts, bulk actions and load-more used on team issue pages. Issues open on the full issue page. Project deletion asks for confirmation, and guests do not see create, edit or delete actions.',
				list: [
					'Add issues with a searchable multi-select picker',
					'New issue is prefilled with the current project',
					'Remove from project keeps the issue',
					'List, board and Gantt layouts'
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
				list: ['Automatic progress calculation', 'Progress numbers on project lists match the progress bar', 'Filter and sort within projects']
			}
		]
	},

	'github-integration': {
		slug: 'github-integration',
		title: 'GitHub Integration — Kuayle',
		description:
			'Connect repositories with a GitHub App, match issue identifiers in development activity, comment issue links on new pull requests, and configure status changes.',
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
				heading: 'Pull-request bot comments',
				body: 'When a pull request is opened, including drafts and pull requests opened by agents, the GitHub App comments with a link to the associated Kuayle issue. The branch, title and body must resolve to exactly one existing public-team issue; ambiguous references, unknown identifiers and private-team issues produce no comment.',
				list: [
					'One "Linked Kuayle issue" comment per newly opened pull request',
					'Duplicate and concurrent webhook deliveries are reconciled before posting',
					'Never links private-team issues, even in private repositories',
					'No backfill for pull requests opened before the upgrade'
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
				body: 'When users open the same issue, the presence channel reports who is viewing it. Issue and comment events update the open issue view. Kuayle does not provide character-by-character collaborative text editing. Live presence is disabled in workspaces that use private teams, and events for private-team content reach only members with access.',
				list: [
					'Join, leave and presence-sync events',
					'Issue and comment refresh events',
					'No collaborative rich-text merge engine'
				]
			},
			{
				heading: 'Notification delivery',
				body: 'Notification events can target a specific workspace member over the same WebSocket connection. Subscribe to an issue to be notified about its changes. The inbox supports read and unread state, snoozing, archiving and bulk mark-as-read.',
				list: [
					'Real-time notification delivery',
					'Issue subscriptions for change notifications',
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
			'Use documented shortcuts for issue creation, workspace navigation, search, issue properties, triage decisions and the inbox.',
		heading: 'Keyboard Shortcuts',
		intro:
			'Kuayle provides global shortcuts and context-specific issue shortcuts. The command palette searches issues and opens common workspace destinations.',
		sections: [
			{
				heading: 'Single-key actions',
				body: 'Press C from the workspace shell to create an issue. In the full issue view, S opens status, P priority, A assignees and L labels. Press ? to list every shortcut. Shortcuts are ignored while typing in form fields.',
				list: [
					'C — Create issue',
					'S / P — Status or priority in an open issue',
					'A / L — Assignees or labels in an open issue',
					'G then I / M / A / P / S — Inbox, My Issues, Insights, Projects or Settings',
					'? — Keyboard shortcut help'
				]
			},
			{
				heading: 'Command palette',
				body: 'Press Cmd+K on macOS or Ctrl+K elsewhere, or /, to open the command palette. It searches issue titles, identifiers and descriptions, and lists commands for issue creation, teams, Inbox, My Issues, Projects and Settings.',
				list: [
					'Cmd+K / Ctrl+K or / to open',
					'Substring search across issue fields',
					'Keyboard selection with arrow keys and Enter',
					'Issue creation and workspace destinations'
				]
			},
			{
				heading: 'Triage and inbox shortcuts',
				body: 'The team triage queue uses J and K to move through incoming issues. Press 1 to accept the selected issue or 3 to decline it. The inbox uses the same J and K navigation, with E to archive and H to snooze the selected notification.',
				list: [
					'J / K — next or previous item in triage and the inbox',
					'1 / 3 — accept or decline the selected triage issue',
					'E / H — archive or snooze the selected notification'
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
				body: 'Save issue filters as personal, team or workspace views. Views retain their filter definition, grouping and sort order, while list/board layout selection is not stored. A saved view can be exposed through a read-only public link. Saved views and view-based public links are disabled in workspaces that use private teams.',
				list: [
					'Complex filters: status, assignee, priority, labels, due date',
					'Personal, team, and workspace view scoping',
					'Grouping and sort order saved with filters',
					'Shareable — views can be made public with a link'
				]
			},
			{
				heading: 'Team triage queue',
				body: 'Teams with triage enabled place incoming issues in a separate queue. Review the selected issue, then accept it into the team workflow or decline it. J/K changes the selection; 1 accepts and 3 declines.',
				list: ['Triage enabled per team', 'Accept or decline decisions', 'J/K, 1 and 3 keyboard controls']
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
				list: ['Custom statuses per team', 'Team-specific triage settings', 'Independent workflows in shared workspace']
			}
		]
	},

	'teams-and-access-control': {
		slug: 'teams-and-access-control',
		title: 'Teams & Access Control — Kuayle',
		description:
			'Multiple teams per workspace, private teams, owner/admin/member/guest roles, invite links, scoped personal access tokens and read-only public links.',
		heading: 'Teams & Access Control',
		intro:
			'Kuayle applies four predefined roles at workspace level, then organizes members and issues into public or private teams. Invite links control who joins, personal access tokens cover API access, and public links provide separate read-only access to selected issue sets.',
		sections: [
			{
				heading: 'Workspace roles',
				body: 'Every workspace member is an Owner, Admin, Member or Guest. Owners control workspace settings. Admins manage teams, members and shared resources. Members create and update work. Guests receive read-only issue access.',
				list: [
					'Owner — full workspace control',
					'Admin — manage teams, members and workspace resources',
					'Member — create, edit, and triage issues',
					'Guest — read-only access, without create, edit or delete actions'
				]
			},
			{
				heading: 'Private teams',
				body: 'Owners and admins can create private teams, manage their membership and change visibility in team settings. Issues, projects, cycles, statuses, comments and attachments inherit the team’s visibility, including real-time updates. Removing a member revokes access on the next request, including existing tokens. Owners and admins always retain access.',
				list: [
					'Membership-based access for members and guests',
					'Existing teams stay public after upgrading',
					'Making a team public again requires confirmation',
					'Once enabled, saved views, view-based public links, workspace export, AI expansion, outgoing webhooks, Dev Machines and live presence are disabled workspace-wide'
				]
			},
			{
				heading: 'Invite links and API tokens',
				body: 'Invite links let new members join a workspace, and operators can set DISABLE_REGISTRATION so that sign-up requires a valid invite link. Personal access tokens provide non-interactive API access: their scopes are a ceiling on the owner’s workspace role, and each token can be limited to specific workspaces, given an expiry and revoked.',
				list: [
					'Workspace invite links with onboarding',
					'Optional invite-only registration',
					'Scoped personal access tokens with optional expiry',
					'Token permissions never exceed the owner’s role'
				]
			},
			{
				heading: 'Teams with custom workflows',
				body: 'A team is a group of workspace members with their own status definitions, triage settings, issue scope and visibility. You can have an Engineering team, a Design team, and a Support team, each operating with the workflow that fits them best.',
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
					'No Kuayle account required for recipients',
					'Unfiltered links to public resources remain available with private teams'
				]
			}
		]
	},

	'analytics-insights': {
		slug: 'analytics-insights',
		title: 'Analytics & Insights — Kuayle',
		description:
			'Workspace and team overviews, event-based burn-up trends, and configurable issue insights based on issue data and lifecycle timestamps.',
		heading: 'Analytics & Insights',
		intro:
			'The Insights page combines current issue data, stored lifecycle timestamps, and recorded creation/status events to provide workspace and team overviews, burn-up trends, and a configurable insight builder.',
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
				body: 'The burn-up chart plots cumulative created and net-completed totals at each date bucket, with remaining work shown beside completed work.',
				list: [
					'Cumulative total-created and net-total-completed series',
					'Remaining-work line: total created minus net completed',
					'Configurable date range'
				]
			},
			{
				heading: 'Configurable issue insights',
				body: 'The insight builder measures issue count; age from creation to now; lead time from creation to completion; cycle time from first start to completion; or triage time from creation to triage. Results can use a supported group and a different comparison segment.',
				list: [
					'Duration groups report P50, P75 and P95',
					'Group or segment by status type, priority, assignee, team, project, cycle, label or creator; team scope also offers custom status',
					'Optional second dimension and date range',
					'Based on current issue data and stored lifecycle timestamps'
				]
			}
		]
	},

	'dev-machines': {
		slug: 'dev-machines',
		title: 'Dev Machines — Kuayle',
		description:
			'Unreleased opt-in multi-container development environments with code-server, a native terminal, agent providers, an in-browser Chrome, and issue worktrees.',
		heading: 'Dev Machines',
		intro: `Dev Machines are an ${DEV_MACHINES_RELEASE_STATUS.toLowerCase()} opt-in self-hosted subsystem on the development branch. Every machine has collector and egress services on an isolated network; it may include a shared developer/terminal container and browser, while agent containers are created on demand. Access is routed through an authenticated gateway. The subsystem is disabled by default.`,
		sections: [
			{
				heading: 'A full environment per machine',
				body: 'When configured, the developer container runs code-server and a tmux-backed terminal that the Kuayle UI renders natively with xterm. An optional separate browser container provides Chrome over KasmVNC. No host ports are published; access goes through the Machine Gateway with one-time launch tickets.',
				list: [
					'code-server IDE and native xterm terminal',
					'In-browser Chrome via KasmVNC',
					'Authenticated wildcard routing — no public ports'
				]
			},
			{
				heading: 'Agent providers',
				body: 'The developer image includes pinned Claude Code, OpenCode and Codex CLIs for direct interactive terminal use. From the machine dashboard, users can launch bounded autonomous runs in provider-specific containers, including an admin-configured custom CLI. Autonomous output is normalized into a common result model with a summary, changed files, commits, branch and pull-request URL.',
				list: [
					'Claude Code, OpenCode, Codex and custom CLI providers',
					'Interactive built-in CLIs in the developer terminal',
					'Bounded autonomous provider runs from the dashboard',
					'Normalized run results with commits and PR links',
					'Scoped secrets delivered through tmpfs, redacted in logs'
				]
			},
			{
				heading: 'Issue worktrees and environments',
				body: 'Machines can start generic and attach issue worktrees when work begins. Repository and environment defaults resolve from the issue, then project, team and workspace. Owner/admins can snapshot a customized builder machine into an immutable local Development Environment image.',
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
					'Filesystem, shell, root-checkout Git, browser and agent activity events',
					'Disabled by default; requires a separate wildcard domain',
					'Not available in workspaces that use private teams'
				]
			}
		]
	}
};
