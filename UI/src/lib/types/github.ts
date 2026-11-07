export interface GitHubInstallation {
	id: string;
	installation_id: number;
	account_login: string;
	account_type: string;
	created_at: string;
}

export interface GitHubRepo {
	id: string;
	github_repo_id: number;
	full_name: string;
	default_branch: string;
	is_active: boolean;
}

export interface GitHubAvailableRepo {
	github_repo_id: number;
	full_name: string;
	default_branch: string;
	private: boolean;
	linked: boolean;
}

export interface GitHubStatus {
	configured: boolean;
	installed: boolean;
	global_app: boolean;
	app_slug?: string;
	installation?: GitHubInstallation;
	repos: GitHubRepo[];
}

export interface GitHubPullRequest {
	id: string;
	number: number;
	title: string;
	state: 'open' | 'closed' | 'merged' | 'draft';
	author_login: string;
	author_avatar_url: string;
	html_url: string;
	head_branch: string;
	base_branch: string;
	additions: number;
	deletions: number;
	repo_full_name: string;
	merged_at: string | null;
	created_at: string;
	updated_at: string;
}

export interface GitHubBranch {
	id: string;
	name: string;
	html_url: string;
	repo_full_name: string;
}

export interface GitHubCommit {
	id: string;
	sha: string;
	short_sha: string;
	message: string;
	author_login: string;
	author_avatar_url: string;
	html_url: string;
	repo_full_name: string;
	committed_at: string;
}

export interface GitHubIssueActivity {
	pull_requests: GitHubPullRequest[];
	branches: GitHubBranch[];
	commits: GitHubCommit[];
}

export interface GitHubAutoTransition {
	event: string;
	target_status: string;
	target_status_id: string | null;
	is_active: boolean;
}

export type GitHubBotAbilityKey =
	| 'link_issues'
	| 'task_feedback'
	| 'review_comments'
	| 'pull_request_actions'
	| 'merge_pull_requests'
	| 'releases'
	| 'mention_reviews';

export interface GitHubBotAbility {
	key: GitHubBotAbilityKey;
	enabled: boolean;
	enabled_by_default: boolean;
}

export interface GitHubBotSettings {
	display_name: string;
	default_name: string;
	// An external https image or Kuayle's public path of an uploaded one.
	avatar_url: string;
	avatar_source: 'none' | 'url' | 'upload';
	abilities: GitHubBotAbility[];
	mention_harness: string;
	mention_model: string;
	// The handle that mentions the bot on GitHub, e.g. "@kuayle-acme"; empty when unknown.
	mention_handle: string;
	harnesses: string[];
}

export interface GitHubBotSettingsUpdate {
	display_name?: string;
	avatar_url?: string;
	abilities?: Partial<Record<GitHubBotAbilityKey, boolean>>;
	mention_harness?: string;
	mention_model?: string;
}
