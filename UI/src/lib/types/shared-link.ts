import type { Label } from './label';
import type { StatusInfo, IssuePriority } from './issue';

export interface SharedLink {
	id: string;
	token: string;
	workspace_id: string;
	created_by: string;
	scope: 'workspace' | 'team' | 'project' | 'view';
	scope_id?: string;
	filters: Record<string, string>;
	include_description: boolean;
	is_active: boolean;
	expires_at?: string;
	url: string;
	created_at: string;
	updated_at: string;
}

export interface CreateSharedLinkRequest {
	scope: 'workspace' | 'team' | 'project' | 'view';
	scope_id?: string;
	filters?: Record<string, string>;
	include_description?: boolean;
	expires_at?: string;
}

export interface UpdateSharedLinkRequest {
	is_active?: boolean;
	include_description?: boolean;
	expires_at?: string;
}

export interface PublicShareMeta {
	scope: string;
	scope_id?: string;
	scope_name: string;
	workspace_name: string;
	filters: Record<string, string>;
	statuses?: PublicStatus[];
}

export interface PublicStatus {
	id: string;
	name: string;
	category: string;
	color: string | null;
	position: number;
}

export interface PublicIssue {
	identifier: string;
	title: string;
	description?: string | null;
	status: string;
	status_info?: StatusInfo;
	priority: IssuePriority;
	labels?: Label[];
	assignees?: PublicUser[];
	due_date?: string;
	estimate?: number;
	created_at: string;
	updated_at: string;
}

export interface PublicUser {
	name: string;
	display_name: string;
	avatar_url?: string;
}
