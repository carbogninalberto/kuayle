import type { DevelopmentRepository } from '$lib/types/dev-machine';
import { m } from '$lib/paraglide/messages.js';

/** Names the development-settings scope a repository was inherited from. */
export function repositorySourceLabel(source: DevelopmentRepository['source']): string {
	switch (source) {
		case 'issue':
			return m['machines.repository_source_issue']();
		case 'project':
			return m['machines.repository_source_project']();
		case 'team':
			return m['machines.repository_source_team']();
		default:
			return m['machines.repository_source_workspace']();
	}
}
