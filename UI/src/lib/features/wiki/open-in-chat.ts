import { goto } from '$app/navigation';
import { conversations } from '$lib/api/conversations';
import { authState } from '$lib/features/auth/auth.state.svelte';
import { assistantDraftKey } from '$lib/features/assistant/chat-draft';
import { appToast } from '$lib/features/toast/toast';
import { m } from '$lib/paraglide/messages.js';
import type { WikiScope } from '$lib/types/wiki';

/**
 * Opens a new assistant conversation about a wiki page. The page goes in as
 * context in the composer draft (its wiki and id, plus any selected text),
 * so the person adds their question and sends it; the assistant then reads
 * and edits the page with its wiki tools, as that person. No special
 * conversation kind is needed: the tools check access on every call.
 */
export async function openWikiPageInChat(options: {
	slug: string;
	scope: WikiScope;
	scopeLabel: string;
	page: { id: string; title: string };
	selection?: string;
}): Promise<void> {
	const { slug, scope, scopeLabel, page, selection } = options;
	try {
		const conversation = await conversations.create(slug, page.title);
		const context = m['wiki.chat_context']({
			title: page.title,
			wiki: scopeLabel,
			kind: scope.kind,
			scope_id: scope.id,
			page_id: page.id
		});
		const quote = selection?.trim()
			? `\n\n${m['wiki.chat_selection']()}\n${selection
					.trim()
					.split('\n')
					.map((line) => `> ${line}`)
					.join('\n')}`
			: '';
		try {
			localStorage.setItem(assistantDraftKey(authState.user?.id, slug, conversation.id), `${context}${quote}\n\n`);
		} catch {
			/* Without storage the conversation still opens, just without the draft. */
		}
		await goto(`/${slug}/assistant?conversation=${encodeURIComponent(conversation.id)}`);
	} catch (error) {
		appToast.apiError(error, m['wiki.toast.chat_failed']());
	}
}
