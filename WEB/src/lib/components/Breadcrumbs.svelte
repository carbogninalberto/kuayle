<script lang="ts">
	import type { Crumb } from '$lib/data/routes';
	import ChevronRight from '@lucide/svelte/icons/chevron-right';

	let { breadcrumbs: crumbs }: { breadcrumbs: Crumb[] } = $props();
</script>

{#if crumbs.length > 0}
	<nav aria-label="Breadcrumb" class="flex items-center gap-1.5 text-sm text-muted-foreground">
		{#each crumbs as crumb, i}
			{#if i > 0}
				<ChevronRight class="size-3.5 shrink-0" aria-hidden="true" />
			{/if}
			{#if i === crumbs.length - 1}
				<span class="text-foreground font-medium" aria-current="page">{crumb.label}</span>
			{:else}
				<a href={crumb.href} class="transition-colors hover:text-foreground">{crumb.label}</a>
			{/if}
		{/each}
	</nav>
{/if}
