export type ForgeProvider = 'gitlab' | 'gitea' | 'forgejo';

export interface ForgeConnection {
	id: string;
	provider: ForgeProvider;
	name: string;
	base_url: string;
	has_access_token: boolean;
	has_webhook_secret: boolean;
	webhook_url: string;
	created_at: string;
	updated_at: string;
}

export interface ForgeConnectionInput {
	provider: ForgeProvider;
	name: string;
	base_url: string;
	webhook_secret: string;
	access_token?: string;
}

export interface ForgeConnectionUpdate {
	name?: string;
	base_url?: string;
	webhook_secret?: string;
	access_token?: string;
}

export interface ForgeRepo {
	id: string;
	connection_id: string;
	external_repo_id: number;
	full_name: string;
	web_url: string;
	default_branch: string;
	is_active: boolean;
}

export interface ForgeRepoInput {
	external_repo_id: number;
	full_name: string;
	web_url?: string;
	default_branch?: string;
}

export interface ForgeMergeRequest {
	id: string;
	provider: ForgeProvider;
	number: number;
	title: string;
	state: 'open' | 'closed' | 'merged' | 'draft';
	author_login: string;
	web_url: string;
	source_branch: string;
	target_branch: string;
	repo_full_name: string;
	merged_at: string | null;
	created_at: string;
	updated_at: string;
}

export interface ForgeBranch {
	id: string;
	provider: ForgeProvider;
	name: string;
	web_url: string;
	repo_full_name: string;
}

export interface ForgeCommit {
	id: string;
	provider: ForgeProvider;
	sha: string;
	short_sha: string;
	message: string;
	author_login: string;
	web_url: string;
	repo_full_name: string;
	committed_at: string;
}

export interface ForgeIssueActivity {
	merge_requests: ForgeMergeRequest[];
	branches: ForgeBranch[];
	commits: ForgeCommit[];
}
