<script lang="ts">
	let {
		name,
		avatarUrl,
		size = 'sm'
	}: { name: string; avatarUrl?: string | null; size?: 'sm' | 'md' | 'lg' } = $props();

	const sizes = { sm: 'h-6 w-6 text-xs', md: 'h-8 w-8 text-sm', lg: 'h-10 w-10 text-base' };
	const initial = name.charAt(0).toUpperCase();
</script>

{#if avatarUrl}
	<img src={avatarUrl} alt={name} class="rounded-full object-cover {sizes[size]}" />
{:else}
	<div
		class="flex items-center justify-center rounded-full bg-[var(--app-accent)] font-medium text-[var(--app-accent-foreground)] {sizes[
			size
		]}"
	>
		{initial}
	</div>
{/if}
