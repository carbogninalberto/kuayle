<script lang="ts">
	import type { Snippet } from 'svelte';
	import * as Card from '$lib/components/ui/card';

	let { title, description, children, footer }: {
		title: string;
		description?: string;
		children: Snippet;
		footer?: Snippet;
	} = $props();
</script>

<Card.Root class="min-w-0 rounded-lg bg-[var(--color-bg-secondary)] ring-[var(--app-border)]">
	<Card.Header class="wrap-anywhere">
		<Card.Title><h2>{title}</h2></Card.Title>
		{#if description}<Card.Description>{description}</Card.Description>{/if}
	</Card.Header>
	<Card.Content class="min-w-0">{@render children()}</Card.Content>
	{#if footer}
		<Card.Footer class="flex-wrap justify-end gap-2">{@render footer()}</Card.Footer>
	{/if}
</Card.Root>
