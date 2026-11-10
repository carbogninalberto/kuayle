// Maps the API's GitHub App setup check to what the settings card shows.
// Kept free of Svelte and message imports so it can be unit tested directly.

export type GitHubAppSetupState =
	| 'ok'
	| 'app_missing'
	| 'installation_pending'
	| 'installation_suspended'
	| 'installation_missing'
	| 'not_configured'
	| 'not_installed'
	| 'unavailable';

export interface GitHubAppPermissionRequirement {
	name: string;
	level: string;
}

export interface GitHubAppSetup {
	state: GitHubAppSetupState;
	managed: boolean;
	app_slug?: string;
	account_login?: string;
	missing_events: string[];
	missing_permissions: GitHubAppPermissionRequirement[];
	app_settings_url?: string;
	installation_url?: string;
}

export type GitHubAppSetupMessage = GitHubAppSetupState | 'app_missing_managed';

export interface GitHubAppSetupView {
	tone: 'ok' | 'warning' | 'error' | 'neutral';
	message: GitHubAppSetupMessage;
	/** Where the main button goes; null when there is nothing to open. */
	action: { kind: 'fix' | 'accept' | 'open_installation'; url: string } | null;
	/** GitHub UI names of the items to tick. */
	checklist: string[];
	/** Closing instructions after the checklist. */
	steps: Array<'save_and_confirm' | 'review_and_accept'>;
	recheckOnFocus: boolean;
}

// Names as GitHub's app settings page shows them.
const EVENT_LABELS: Record<string, string> = {
	pull_request: 'Pull request',
	pull_request_review: 'Pull request review',
	check_suite: 'Check suite',
	issue_comment: 'Issue comment',
	push: 'Push',
	create: 'Create'
};

const PERMISSION_LABELS: Record<string, string> = {
	pull_requests: 'Pull requests',
	contents: 'Contents',
	metadata: 'Metadata',
	issues: 'Issues',
	checks: 'Checks'
};

const LEVEL_LABELS: Record<string, string> = {
	read: 'Read-only',
	write: 'Read and write',
	admin: 'Admin'
};

export function githubEventLabel(name: string): string {
	return EVENT_LABELS[name] ?? name;
}

export function githubPermissionLabel(permission: GitHubAppPermissionRequirement): string {
	const name = PERMISSION_LABELS[permission.name] ?? permission.name;
	return `${name}: ${LEVEL_LABELS[permission.level] ?? permission.level}`;
}

function githubPage(value: string | undefined): string | null {
	if (!value) return null;
	try {
		const url = new URL(value);
		if (url.protocol !== 'https:' || url.hostname !== 'github.com' || url.username || url.password || url.port) return null;
		return url.href;
	} catch {
		return null;
	}
}

function link(kind: 'fix' | 'accept' | 'open_installation', value: string | undefined): GitHubAppSetupView['action'] {
	const url = githubPage(value);
	return url ? { kind, url } : null;
}

export function githubAppSetupView(setup: GitHubAppSetup): GitHubAppSetupView {
	const checklist = [
		...setup.missing_permissions.map(githubPermissionLabel),
		...setup.missing_events.map(githubEventLabel)
	];
	const view: GitHubAppSetupView = {
		tone: 'neutral',
		message: setup.state,
		action: null,
		checklist: [],
		steps: [],
		recheckOnFocus: true
	};
	switch (setup.state) {
		case 'ok':
			return { ...view, tone: 'ok', recheckOnFocus: false };
		case 'app_missing':
			if (setup.managed) return { ...view, tone: 'warning', message: 'app_missing_managed', checklist };
			return {
				...view,
				tone: 'warning',
				action: link('fix', setup.app_settings_url),
				checklist,
				steps: ['save_and_confirm']
			};
		case 'installation_pending':
			return {
				...view,
				tone: 'warning',
				action: link('accept', setup.installation_url),
				checklist,
				steps: ['review_and_accept']
			};
		case 'installation_suspended':
			return { ...view, tone: 'error', action: link('open_installation', setup.installation_url) };
		case 'installation_missing':
			return { ...view, tone: 'error', action: link('open_installation', setup.installation_url) };
		default:
			return view;
	}
}

/** Where Kuayle's logo for the GitHub App is served (a PNG, as GitHub requires). */
export const GITHUB_APP_LOGO_PATH = '/icons/icon-512.png';

/**
 * The app's general settings page, whose Display information section holds
 * the logo. GitHub has no API for an app's logo, so this is where it is
 * changed. Null for an operator-managed app, which the workspace cannot edit.
 */
export function githubAppLogoSettingsUrl(setup: GitHubAppSetup): string | null {
	if (setup.managed) return null;
	const url = githubPage(setup.app_settings_url);
	return url ? url.replace(/\/permissions$/, '') : null;
}
