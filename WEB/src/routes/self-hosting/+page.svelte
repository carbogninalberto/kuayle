<script lang="ts">
	import Nav from '$lib/components/Nav.svelte';
	import Footer from '$lib/components/Footer.svelte';
	import Seo from '$lib/components/Seo.svelte';
	import Breadcrumbs from '$lib/components/Breadcrumbs.svelte';
	import HubLinks from '$lib/components/HubLinks.svelte';
	import CtaSection from '$lib/components/CtaSection.svelte';
	import { reveal } from '$lib/actions/reveal';
	import { HUBS, breadcrumbsFrom, metaForStandalone, webPageLd } from '$lib/data/routes';
	import { url } from '$lib/config/site';
	import { DEV_MACHINES_RELEASE_STATUS } from '$lib/config/releases';
	import ArrowRight from '@lucide/svelte/icons/arrow-right';

	const hub = HUBS.selfHosting;
	const meta = metaForStandalone('self-hosting')!;
	const crumbs = breadcrumbsFrom('self-hosting', 'Self-Hosting');
	const jsonLd = webPageLd(meta.title, meta.description, url('/self-hosting'), crumbs);
</script>

<Seo meta={{ ...meta, jsonLd }} />

<Nav />

<main class="mx-auto max-w-6xl px-6 pt-28 pb-20">
	<div class="mb-8">
		<Breadcrumbs breadcrumbs={crumbs} />
	</div>

	<div class="grid gap-10 lg:grid-cols-[1fr_260px]">
		<article class="min-w-0 [overflow-wrap:anywhere]">
			<div use:reveal>
				<h1 class="text-4xl font-bold tracking-tight">Self-Hosting Kuayle</h1>
				<p class="mt-4 text-lg leading-relaxed text-muted-foreground">
					The reference Docker Compose stack runs Caddy, PostgreSQL 17, Redis 7, the Go API and the SvelteKit frontend.
					These guides cover configuration, storage, updates, GitHub webhook delivery and the {DEV_MACHINES_RELEASE_STATUS.toLowerCase()}
					opt-in Dev Machines subsystem—including the work that remains with the operator.
				</p>
			</div>

			<div class="mt-12 grid gap-4 sm:grid-cols-2">
				{#each hub.children as child}
					<a
						href={child.href}
						use:reveal={{ delay: 50 }}
						class="group rounded-xl border border-border bg-card/60 p-5 transition-all duration-300 hover:-translate-y-1 hover:border-brand-400/40 hover:bg-card"
					>
						<h2 class="text-lg font-semibold">{child.label}</h2>
						{#if child.description}
							<p class="mt-1.5 text-sm text-muted-foreground">{child.description}</p>
						{/if}
						<span
							class="mt-3 inline-flex items-center gap-1 text-xs font-medium text-brand-300 opacity-0 transition-opacity group-hover:opacity-100"
						>
							Open guide <ArrowRight class="size-3" />
						</span>
					</a>
				{/each}
			</div>

			<CtaSection />
		</article>

		<aside use:reveal={{ delay: 100 }}>
			<HubLinks links={hub.children} title="Self-hosting guide" />
		</aside>
	</div>
</main>

<Footer />
