import { appToast } from '$lib/features/toast/toast';
import { m } from '$lib/paraglide/messages.js';

/**
 * Runs an AI rework of the selected description text for the rich editor's
 * `onreworkselection` hook. Issue details and issue creation share it so both
 * report success and failure the same way; an empty result leaves the text as is.
 */
export async function reworkDescriptionSelection(request: () => Promise<{ description: string }>): Promise<string> {
	try {
		const result = await request();
		appToast.success(m['issue.toast.selection_reworked']());
		return result.description;
	} catch (err) {
		appToast.apiError(err, m['issue.toast.failed_rework']());
		return '';
	}
}
