/**
 * Kuayle's side of every comparison, verified against the implementation (BE/, UI/, selfhosting/)
 * at v0.2.23. One source of truth: alternative pages, the compare matrix and the homepage read from here.
 * Gaps are stated as gaps. Update this file when the product changes.
 */
import type { Mark } from './competitor-types.ts';

export const KUAYLE_FACTS_VERSION = 'v0.2.23';

export interface KuayleFact {
	mark: Mark;
	text: string;
}

export const KUAYLE = {
	license: { mark: 'yes', text: 'Apache 2.0. The full source is in one public repository.' },
	hosting: {
		mark: 'yes',
		text: 'Self-hosted with Docker Compose (Caddy, PostgreSQL, Redis, API, frontend). There is no hosted Kuayle service.'
	},
	pricing: {
		mark: 'yes',
		text: 'No licence fee and no paid tier. Nothing is gated behind a key; you pay for your own server.'
	},
	sso: {
		mark: 'no',
		text: 'Email and password sign-in with invite links. No SSO (SAML/OIDC), SCIM, passkeys or magic links yet.'
	},
	ai: {
		mark: 'yes',
		text: 'A built-in assistant on your own OpenAI-compatible model searches, creates and updates issues and delegates them to coding agents (Claude Code, Codex, OpenCode and others) on opt-in, self-hosted Dev Machines. Each task is checked independently and opens a pull request; automations and editable workflows start agents on a label or status. Agents are not issue assignees yet.'
	},
	mcp: { mark: 'no', text: 'No MCP server yet.' },
	api: {
		mark: 'partial',
		text: 'HTTP API with scoped, expiring personal access tokens, plus HMAC-signed webhooks. No published OpenAPI reference yet.'
	},
	importers: {
		mark: 'no',
		text: 'No importer from other trackers. Kuayle-to-Kuayle workspace export and import (.kuayle.zip) is built in.'
	},
	gitForges: {
		mark: 'partial',
		text: 'GitHub App: branches, commits and pull requests link to issues, status automation on PR events, and PR comments. No GitLab, Bitbucket or Gitea yet.'
	},
	data: {
		mark: 'yes',
		text: 'In your own PostgreSQL and storage (local disk or S3-compatible). You run backups and updates.'
	},
	views: {
		mark: 'partial',
		text: 'List and board layouts, saved views, triage inbox and a project Gantt chart. No calendar or table view yet.'
	},
	planning: {
		mark: 'partial',
		text: 'Cycles with burndown, velocity and carry-over; cross-team projects; workspace and team analytics. No initiatives or configurable dashboards.'
	},
	issues: {
		mark: 'yes',
		text: 'Multiple assignees, sub-issues, relations, labels, priorities, due dates, templates, team-specific statuses and title/description version history.'
	},
	access: {
		mark: 'yes',
		text: 'Public and private teams, workspace roles (owner, admin, member, guest), multiple workspaces per instance.'
	},
	customFields: { mark: 'no', text: 'No custom fields, estimates or time tracking.' }
} as const satisfies Record<string, KuayleFact>;

export type KuayleDimension = keyof typeof KUAYLE;

/** Implemented features, grouped for the homepage grid. Only list what ships today. */
export const KUAYLE_FEATURE_GROUPS: { group: string; blurb: string; items: { label: string; href?: string }[] }[] = [
	{
		group: 'Track',
		blurb: 'Shape the work.',
		items: [
			{ label: 'Multiple assignees per issue', href: '/features/issue-tracking' },
			{ label: 'Sub-issues and relations', href: '/features/issue-tracking' },
			{ label: 'Labels, priorities, due dates', href: '/features/issue-tracking' },
			{ label: 'Issue templates', href: '/features/issue-tracking' },
			{ label: 'Title and description history', href: '/features/issue-tracking' },
			{ label: 'Team-specific statuses', href: '/features/views-and-triage' }
		]
	},
	{
		group: 'Plan',
		blurb: 'See where it stands.',
		items: [
			{ label: 'Cycles with carry-over', href: '/features/cycles' },
			{ label: 'Burndown and velocity', href: '/features/cycles' },
			{ label: 'Cross-team projects', href: '/features/projects' },
			{ label: 'Project Gantt chart', href: '/features/projects' },
			{ label: 'Workspace and team insights', href: '/features/analytics-insights' }
		]
	},
	{
		group: 'Collaborate',
		blurb: 'Work in one place.',
		items: [
			{ label: 'Threaded comments and @mentions', href: '/features/issue-tracking' },
			{ label: 'Saved views and triage inbox', href: '/features/views-and-triage' },
			{ label: 'Real-time sync', href: '/features/real-time-sync' },
			{ label: 'Keyboard shortcuts and ⌘K', href: '/features/keyboard-shortcuts' },
			{ label: 'Public share links' },
			{ label: 'Attachments' }
		]
	},
	{
		group: 'Integrate',
		blurb: 'Connect your code and your agents.',
		items: [
			{ label: 'Built-in assistant on your own model', href: '/features/issue-tracking' },
			{ label: 'Delegate issues to coding agents', href: '/features/dev-machines' },
			{ label: 'Agent automations and workflows', href: '/features/dev-machines' },
			{ label: 'GitHub App: branches, commits, PRs', href: '/features/github-integration' },
			{ label: 'Status automation on PR events', href: '/features/github-integration' },
			{ label: 'Signed webhooks' },
			{ label: 'Scoped personal access tokens', href: '/features/teams-and-access-control' }
		]
	},
	{
		group: 'Control',
		blurb: 'Decide who sees what.',
		items: [
			{ label: 'Private teams', href: '/features/teams-and-access-control' },
			{ label: 'Workspace roles', href: '/features/teams-and-access-control' },
			{ label: 'Multiple workspaces' },
			{ label: 'Workspace export and import', href: '/self-hosting/workspace-transfer' }
		]
	},
	{
		group: 'Deploy',
		blurb: 'Run it yourself.',
		items: [
			{ label: 'Docker Compose stack', href: '/self-hosting/docker-compose' },
			{ label: 'Local disk or S3 storage', href: '/self-hosting/storage' },
			{ label: 'Scripted and in-app updates', href: '/self-hosting/updating' },
			{ label: 'Opt-in Dev Machines', href: '/features/dev-machines' }
		]
	}
];
