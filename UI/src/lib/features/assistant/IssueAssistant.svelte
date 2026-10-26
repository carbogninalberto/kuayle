<script lang="ts">
	import { onDestroy } from 'svelte';
	import { MessageCircle, ChevronDown } from '@lucide/svelte';
	import * as Collapsible from '$lib/components/ui/collapsible/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { conversations, type Conversation } from '$lib/api/conversations';
	import { authState } from '$lib/features/auth/auth.state.svelte';
	import { m } from '$lib/paraglide/messages.js';
	import AssistantChat from './AssistantChat.svelte';

	let { slug, issueId }: { slug: string; issueId: string } = $props();
	let open = $state(false);
	let conversation = $state<Conversation | null>(null);
	let loading = $state(false);
	let error = $state('');
	let denied = $state(false);
	let disposed = false;
	let generation = 0;
	const scope = $derived(`${slug}:${issueId}:${authState.user?.id}`);
	let activeScope = '';
	$effect(() => {
		if (scope === activeScope) return;
		activeScope = scope;
		generation++;
		open = false;
		conversation = null;
		loading = false;
		error = '';
		denied = false;
	});
	onDestroy(() => {
		disposed = true;
		generation++;
	});
	async function load() {
		if (loading || conversation) return;
		const requestedIssue = issueId,
			requestedSlug = slug,
			requestedScope = scope,
			version = ++generation;
		loading = true;
		error = '';
		denied = false;
		try {
			const result = await conversations.openIssue(requestedSlug, requestedIssue);
			if (!disposed && version === generation && requestedScope === scope) conversation = result;
		} catch (e) {
			if (!disposed && version === generation && requestedScope === scope) {
				denied = [401, 403, 404].includes((e as { status?: number }).status ?? 0);
				error = denied ? m['assistant.unavailable']() : m['assistant.error']();
			}
		} finally {
			if (!disposed && version === generation) loading = false;
		}
	}
</script>

<Collapsible.Root
	bind:open
	onOpenChange={(value) => {
		if (value) void load();
	}}
>
	<Collapsible.Trigger>
		{#snippet child({ props })}
			<Button
				{...props}
				variant="plain"
				class="flex w-full items-center gap-2 rounded-md border border-border px-3 py-2 text-sm font-medium hover:bg-muted"
			>
				<MessageCircle size={16} />{m['assistant.issue_chat']()}<ChevronDown
					size={15}
					class="ml-auto transition-transform {open ? 'rotate-180' : ''}"
				/>
			</Button>
		{/snippet}
	</Collapsible.Trigger>
	<Collapsible.Content>
		<section aria-label={m['assistant.issue_chat']()} class="mt-2 overflow-hidden rounded-lg border border-border">
			{#if loading}<p class="p-4 text-sm text-muted-foreground" role="status">{m['assistant.loading']()}</p>
			{:else if error}<div class="space-y-2 p-4">
					<p role="alert" class="text-sm text-destructive">{error}</p>
					{#if !denied}<Button variant="outline" size="sm" onclick={() => void load()}>{m['assistant.retry']()}</Button
						>{/if}
				</div>
			{:else if conversation}
				<p class="border-b px-4 py-2 text-xs text-muted-foreground">{m['assistant.issue_intro']()}</p>
				<div class="h-[36rem] max-h-[80dvh] min-h-80">
					<AssistantChat {slug} conversationId={conversation.id} embedded />
				</div>
			{/if}
		</section>
	</Collapsible.Content>
</Collapsible.Root>
