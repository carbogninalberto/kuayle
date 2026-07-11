<script lang="ts">
	import ArrowRight from '@lucide/svelte/icons/arrow-right';

	interface HubLink {
		slug: string;
		label: string;
		href: string;
		description?: string;
	}

	let { links, currentSlug = '', title = 'In this section' }: {
		links: HubLink[];
		currentSlug?: string;
		title?: string;
	} = $props();
</script>

<nav aria-label={title} class="rounded-2xl border border-border bg-card/60 p-4">
	<h2 class="text-sm font-semibold tracking-widest text-muted-foreground uppercase">{title}</h2>
	<ul class="mt-3 space-y-1">
		{#each links as link}
			<li>
				<a
					href={link.href}
					class="group grid grid-cols-[0.875rem_1fr] items-center gap-x-2 gap-y-0.5 rounded-lg px-2 py-2 transition-colors hover:bg-white/5"
					aria-current={link.slug === currentSlug ? 'page' : undefined}
				>
					<ArrowRight class="size-3.5 text-brand-400 transition-transform group-hover:translate-x-0.5" aria-hidden="true" />
					<span class="text-sm font-medium leading-5 transition-colors group-hover:text-foreground">{link.label}</span>
					{#if link.description}
						<span class="col-start-2 text-xs leading-4 text-muted-foreground">{link.description}</span>
					{/if}
				</a>
			</li>
		{/each}
	</ul>
</nav>
