import { redirect } from '@sveltejs/kit';
import type { PageLoad } from './$types';

// Dev Machines live in Settings; keep old links (and their issue filter) working.
export const load: PageLoad = ({ params, url }) => {
	const target = new URL(`/${params.workspaceSlug}/settings/dev-machines`, url);
	const issueId = url.searchParams.get('issue_id');
	if (issueId) target.searchParams.set('issue_id', issueId);
	redirect(307, target.pathname + target.search);
};
