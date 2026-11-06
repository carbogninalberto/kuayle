<script lang="ts">
	import { reveal } from '$lib/actions/reveal';
	import { KUAYLE_FEATURE_GROUPS } from '$lib/data/kuayle-facts';

	const total = KUAYLE_FEATURE_GROUPS.reduce((sum, group) => sum + group.items.length, 0);
</script>

<section id="features" class="py-20 sm:py-28" aria-labelledby="features-heading">
	<div class="mx-auto max-w-6xl px-6">
		<div class="max-w-2xl" use:reveal>
			<p class="font-mono text-xs tracking-widest text-brand-300 uppercase">Everything in the box · {total}</p>
			<h2 id="features-heading" class="mt-3 text-3xl font-bold tracking-tight sm:text-4xl">Every feature, in every install</h2>
			<p class="mt-4 text-lg text-muted-foreground">Shipped in the current release, with nothing reserved for a paid edition.</p>
		</div>
		<div class="mt-10 grid gap-px overflow-hidden rounded-2xl border border-border bg-border sm:grid-cols-2 lg:grid-cols-3">
			{#each KUAYLE_FEATURE_GROUPS as group, i}
				<div class="bg-background p-6" use:reveal={{ delay: i * 40 }}>
					<h3 class="flex items-baseline justify-between">
						<span class="font-semibold">{group.group}</span>
						<span class="font-mono text-xs text-muted-foreground">{String(group.items.length).padStart(2, '0')}</span>
					</h3>
					<p class="mt-1 text-xs text-muted-foreground">{group.blurb}</p>
					<ul class="mt-4 space-y-1.5 text-sm">
						{#each group.items as item}
							<li>
								{#if item.href}
									<a href={item.href} class="text-muted-foreground transition-colors hover:text-foreground">{item.label}</a>
								{:else}
									<span class="text-muted-foreground">{item.label}</span>
								{/if}
							</li>
						{/each}
					</ul>
				</div>
			{/each}
		</div>
		<p class="mt-5 text-sm text-muted-foreground">
			Not yet: SSO, an MCP server, importers from other trackers, custom fields. See the
			<a href="/roadmap" class="text-brand-300 underline-offset-4 hover:underline">roadmap</a>.
		</p>
	</div>
</section>
