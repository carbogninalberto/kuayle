<script lang="ts">
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
		<button
			onclick={action.onclick}
			class="mt-2 rounded-md bg-[var(--app-accent)] px-3 py-1.5 text-sm text-white hover:bg-[var(--app-accent-hover)]"
		>
			{action.label}
		</button>
	{/if}
</div>
