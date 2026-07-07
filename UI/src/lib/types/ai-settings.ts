export interface AISettings {
	provider: string;
	base_url: string;
	model: string;
	has_api_key: boolean;
	description_expand_prompt: string;
	default_prompt: string;
	issue_copy_prompt: string;
	default_issue_copy_prompt: string;
	created_at: string;
	updated_at: string;
}

export interface UpdateAISettingsRequest {
	provider?: string;
	base_url?: string;
	model?: string;
	api_key?: string | null;
	description_expand_prompt?: string;
	issue_copy_prompt?: string;
}
