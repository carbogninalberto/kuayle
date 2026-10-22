<script lang="ts">
	import { Button } from '$lib/components/ui/button/index.js';
	let {
		title,
		description,
		action
	}: {
		title: string;
		description?: string;
		action?: { label: string; onclick: () => void };
	} = $props();
</script>

<div class="flex h-64 flex-col items-center justify-center gap-3">
	<p class="text-sm font-medium text-[var(--color-text-primary)]">{title}</p>
	{#if description}
		<p class="text-sm text-[var(--color-text-secondary)]">{description}</p>
	{/if}
	{#if action}
		<Button variant="default" size="sm"
			onclick={action.onclick}
			class="mt-2 rounded-md px-3 py-1.5 text-sm"
		>
			{action.label}
		</Button>
	{/if}
</div>
