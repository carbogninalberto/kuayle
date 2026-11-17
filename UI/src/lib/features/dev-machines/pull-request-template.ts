// The template agent pull request descriptions are rendered from. It mirrors
// DefaultPullRequestTemplate, PullRequestTemplatePlaceholders and
// PullRequestTemplateMaxRunes in BE/internal/domain/agent_pull_request.go,
// which the API enforces.
export const DEFAULT_PULL_REQUEST_TEMPLATE =
	'## Description\n{{description}}\n\n## Changes\n{{changes}}\n\n## Verification\n{{verification}}\n\n## Screenshots\n{{screenshots}}\n\n## Notes\n{{notes}}\n';

export const PULL_REQUEST_TEMPLATE_PLACEHOLDERS = ['description', 'changes', 'verification', 'screenshots', 'notes', 'issue'];

export const PULL_REQUEST_TEMPLATE_MAX_LENGTH = 8000;

export type PullRequestTemplateProblem = { kind: 'too_long' } | { kind: 'unknown_placeholder'; name: string } | null;

export function pullRequestTemplateProblem(value: string): PullRequestTemplateProblem {
	const template = value.trim();
	if ([...template].length > PULL_REQUEST_TEMPLATE_MAX_LENGTH) return { kind: 'too_long' };
	for (const match of template.matchAll(/\{\{([a-z_]+)\}\}/g)) {
		if (!PULL_REQUEST_TEMPLATE_PLACEHOLDERS.includes(match[1])) return { kind: 'unknown_placeholder', name: match[1] };
	}
	return null;
}

// What the field shows. A workspace without a template shows the default,
// ready to edit; a narrower scope shows its own template, empty to inherit.
export function pullRequestTemplateValue(stored: string | null | undefined, inherits: boolean): string {
	return stored ?? (inherits ? '' : DEFAULT_PULL_REQUEST_TEMPLATE);
}

// What saving sends. Empty inherits, and so does the default at workspace
// scope, so the workspace follows Kuayle's default as it changes. Throws on a
// template the API refuses, like previewSettingPayload.
export function pullRequestTemplatePayload(value: string, inherits: boolean): { pull_request_template?: string } {
	if (pullRequestTemplateProblem(value)) throw new Error('Invalid pull request template');
	const template = value.trim();
	if (!template || (!inherits && template === DEFAULT_PULL_REQUEST_TEMPLATE.trim())) return {};
	return { pull_request_template: template };
}
