<script lang="ts">
	import { goto } from '$app/navigation';
	import { LoaderCircle, MessageCircle } from '@lucide/svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import { conversations } from '$lib/api/conversations';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/paraglide/messages.js';

	// Opens this issue's private assistant conversation on the assistant page,
	// creating it the first time and reusing it afterwards (one per person,
	// issue and team, decided by the API).
	let { slug, issueId, class: className = '' }: { slug: string; issueId: string; class?: string } = $props();
	let opening = $state(false);
	async function open() {
		if (opening) return;
		opening = true;
		try {
			const conversation = await conversations.openIssue(slug, issueId);
			await goto(`/${slug}/assistant?conversation=${encodeURIComponent(conversation.id)}`);
		} catch (error) {
			appToast.apiError(error, m['assistant.issue_chat_open_error']());
		} finally {
			opening = false;
		}
	}
</script>

<Button
	variant="outline"
	size="sm"
	class="h-7 shrink-0 gap-1.5 px-2 text-xs {className}"
	disabled={opening}
	title={m['assistant.issue_chat_open_help']()}
	onclick={() => void open()}
	>{#if opening}<LoaderCircle class="animate-spin" />{:else}<MessageCircle />{/if}{m['assistant.issue_chat_open']()}</Button
>
