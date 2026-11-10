<script lang="ts" generics="T">
	import type { Snippet } from 'svelte';
	import * as Dialog from '$lib/components/ui/dialog/index.js';
	import { ScrollArea } from '$lib/components/ui/scroll-area/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { m } from '$lib/features/i18n/messages';

	// Shows the `limit` most recent items of a long list in place and the whole
	// list in a scrollable dialog. `take` picks the recent end: 'first' for
	// newest-first lists, 'last' for chronological ones (logs). The caller
	// renders the items, so a list, grid or log block keeps its own markup.
	let {
		items,
		limit = 5,
		take = 'first',
		title,
		description,
		content,
		empty
	}: {
		items: T[];
		limit?: number;
		take?: 'first' | 'last';
		title: string;
		description?: string;
		content: Snippet<[T[], boolean]>;
		empty?: Snippet;
	} = $props();

	let open = $state(false);
	const recent = $derived(take === 'first' ? items.slice(0, limit) : items.slice(Math.max(0, items.length - limit)));
</script>

{#if items.length === 0}
	{@render empty?.()}
{:else}
	{@render content(recent, false)}
	{#if items.length > limit}
		<Button variant="ghost" size="sm" class="mt-2 h-7 px-2 text-xs" onclick={() => (open = true)}>
			{m['machines.view_all']({ count: items.length })}
		</Button>
		<Dialog.Root bind:open>
			<Dialog.Content class="sm:max-w-3xl">
				<Dialog.Header>
					<Dialog.Title>{title}</Dialog.Title>
					{#if description}<Dialog.Description>{description}</Dialog.Description>{/if}
				</Dialog.Header>
				<ScrollArea class="h-[60vh] pr-3">
					{#if open}{@render content(items, true)}{/if}
				</ScrollArea>
			</Dialog.Content>
		</Dialog.Root>
	{/if}
{/if}
