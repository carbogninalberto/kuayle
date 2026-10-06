export interface Team {
	is_private?: boolean;
	id: string;
	name: string;
	key: string;
	description: string | null;
	color: string | null;
	icon: string | null;
	triage_enabled: boolean;
	parent_auto_close_enabled: boolean;
	sub_issue_auto_close_enabled: boolean;
	issue_copy_prompt: string | null;
	created_at: string;
	updated_at: string;
}

export interface TeamMember { team_id: string; user_id: string; created_at: string }
export interface CreateTeamInput { name: string; key: string; description?: string; color?: string; is_private?: boolean; acknowledge_privacy_limitations?: boolean }
