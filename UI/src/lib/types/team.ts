export interface Team {
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
