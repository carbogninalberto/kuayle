// Translated text for custom tool schema problems and run modes.
import { m } from '$lib/paraglide/messages.js';
import type { AssistantToolRunMode } from '$lib/api/assistant-tools';
import type { SchemaIssue } from './tool-schema';

export function schemaIssueText(issue: SchemaIssue) {
	const name = issue.param;
	switch (issue.code) {
		case 'name':
			return m['assistant_custom_tools.issue_name']();
		case 'duplicate':
			return m['assistant_custom_tools.issue_duplicate']({ name });
		case 'too_many':
			return m['assistant_custom_tools.issue_too_many']();
		case 'description':
			return m['assistant_custom_tools.issue_description']({ name });
		case 'enum':
			return m['assistant_custom_tools.issue_enum']({ name });
		case 'number':
			return m['assistant_custom_tools.issue_number']({ name });
		case 'bounds':
			return m['assistant_custom_tools.issue_bounds']({ name });
		case 'length':
			return m['assistant_custom_tools.issue_length']({ name });
		default:
			return m['assistant_custom_tools.issue_required']({ name });
	}
}

export const runModeLabel = (mode: AssistantToolRunMode) =>
	mode === 'auto' ? m['assistant_custom_tools.mode_auto']() : m['assistant_custom_tools.mode_approval']();
