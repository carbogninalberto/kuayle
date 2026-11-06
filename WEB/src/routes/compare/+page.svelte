<script lang="ts">
	import ArrowRight from '@lucide/svelte/icons/arrow-right';
	import Breadcrumbs from '$lib/components/Breadcrumbs.svelte';
	import CompetitorMatrix from '$lib/components/CompetitorMatrix.svelte';
	import CtaSection from '$lib/components/CtaSection.svelte';
	import Footer from '$lib/components/Footer.svelte';
	import Nav from '$lib/components/Nav.svelte';
	import Seo from '$lib/components/Seo.svelte';
	import { reveal } from '$lib/actions/reveal';
	import { url } from '$lib/config/site';
	import { alternativePages, competitorBySlug } from '$lib/data/alternative-pages';
	import { alternativePath, hostingLabel } from '$lib/data/competitor-types';
	import { competitors } from '$lib/data/competitors';
	import { KUAYLE_FACTS_VERSION } from '$lib/data/kuayle-facts';
	import { breadcrumbsFrom, faqLd, metaForStandalone, webPageLd } from '$lib/data/routes';

	const meta = metaForStandalone('compare')!;
	const crumbs = breadcrumbsFrom('compare', 'Compare');

	const hosted = competitors.filter((competitor) => competitor.category === 'hosted');
	const openSource = competitors.filter((competitor) => competitor.category === 'open-source');

	const faq = [
		{
			question: 'What is the best self-hosted issue tracker?',
			answer:
				'It depends on scope. Kuayle, Plane and It’s a Plan are focused trackers; OpenProject and Redmine are broader project-management suites; GitLab, Gitea and Forgejo attach issues to Git hosting. Compare licence, edition model and what is gated before choosing.'
		},
		{
			question: 'Which issue trackers can be self-hosted?',
			answer:
				'Among the tools compared here: Kuayle, Plane, It’s a Plan, OpenProject, Taiga, YouTrack Server, GitLab, Vikunja, Leantime, Redmine, Gitea, Forgejo, Gogs, Kanboard, WeKan, Huly (its hosted service has shut down), GitHub Enterprise Server, Zenhub Enterprise Server and Azure DevOps Server. Jira Data Center is closed to new customers and ends in 2029.'
		},
		{
			question: 'Which self-hosted issue tracker is free?',
			answer:
				'Kuayle, Redmine, Forgejo, Gogs, Kanboard and WeKan have no paid edition. Plane, It’s a Plan, OpenProject, Taiga, GitLab, Gitea, Vikunja and Leantime have free self-hosted editions alongside paid tiers, add-ons or hosted plans.'
		},
		{
			question: 'How were these comparisons checked?',
			answer: `Every competitor fact links to the vendor’s own pricing page, documentation or licence file, checked on 6 November 2026. Kuayle facts were verified against the ${KUAYLE_FACTS_VERSION} source code. Unverified details are marked “Not documented”.`
		}
	];

	const jsonLd = [webPageLd(meta.title, meta.description, url('/compare'), crumbs), faqLd(faq)];
</script>

<Seo meta={{ ...meta, jsonLd }} />
<Nav />

<main id="main" tabindex="-1" class="mx-auto max-w-6xl px-6 pt-28 pb-20 outline-none">
	<Breadcrumbs breadcrumbs={crumbs} />
	<article class="mt-8 min-w-0">
		<div use:reveal>
			<p class="font-mono text-xs tracking-widest text-brand-300 uppercase">Comparison matrix</p>
			<h1 class="mt-3 text-4xl font-bold tracking-tight sm:text-5xl">Self-hosted issue tracker comparison</h1>
			<p class="mt-4 max-w-3xl text-lg leading-relaxed text-muted-foreground">
				Kuayle next to {competitors.length} issue trackers and project tools: licence, self-hosting, price, SSO, MCP and
				importers. Every fact comes from the vendor’s own pages, and Kuayle’s gaps are listed as gaps.
			</p>
		</div>

		<section class="mt-14" aria-labelledby="detailed-heading">
			<h2 id="detailed-heading" class="text-2xl font-semibold tracking-tight">Detailed comparisons</h2>
			<ul class="mt-6 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
				{#each alternativePages as page, i}
					{@const competitor = competitorBySlug(page.competitor)}
					<li use:reveal={{ delay: i * 30 }}>
						<a href={alternativePath(competitor)} class="group flex h-full flex-col rounded-xl border border-border bg-card/60 p-5 transition-colors hover:border-brand-400/40">
							<span class="font-semibold">{competitor.displayName ?? competitor.name} alternative</span>
							<span class="mt-1 text-xs text-muted-foreground">{competitor.license} · {hostingLabel(competitor.hosting)}</span>
							<span class="mt-3 flex-1 text-sm leading-relaxed text-muted-foreground">{page.difference}</span>
							<span class="mt-4 inline-flex items-center gap-1 text-sm font-medium text-brand-300">
								Read comparison <ArrowRight class="size-4 transition-transform group-hover:translate-x-0.5" aria-hidden="true" />
							</span>
						</a>
					</li>
				{/each}
				<li>
					<a href="/jira-data-center-end-of-life" class="group flex h-full flex-col rounded-xl border border-dashed border-white/15 bg-card/40 p-5 transition-colors hover:border-brand-400/40">
						<span class="font-semibold">Jira Data Center end of life</span>
						<span class="mt-3 flex-1 text-sm leading-relaxed text-muted-foreground">Key dates from Atlassian and the self-hosted options that remain after 2029.</span>
						<span class="mt-4 inline-flex items-center gap-1 text-sm font-medium text-brand-300">Read the guide <ArrowRight class="size-4" aria-hidden="true" /></span>
					</a>
				</li>
			</ul>
		</section>

		<section class="mt-16" aria-labelledby="open-heading">
			<h2 id="open-heading" class="text-2xl font-semibold tracking-tight">Open source and self-hostable trackers</h2>
			<p class="mt-3 max-w-3xl text-sm text-muted-foreground">Scroll the table sideways on small screens.</p>
			<div class="mt-6">
				<CompetitorMatrix competitors={openSource} caption="Open source tools. Licences from each project’s own repository; checked 6 November 2026." />
			</div>
		</section>

		<section class="mt-16" aria-labelledby="hosted-heading">
			<h2 id="hosted-heading" class="text-2xl font-semibold tracking-tight">Hosted and proprietary trackers</h2>
			<div class="mt-6">
				<CompetitorMatrix competitors={hosted} caption="Proprietary tools. List prices before tax from each vendor’s pricing page; checked 6 November 2026." />
			</div>
		</section>

		<section class="mt-16" aria-labelledby="faq-heading">
			<h2 id="faq-heading" class="text-2xl font-semibold tracking-tight">Questions</h2>
			<dl class="mt-6 divide-y divide-border rounded-xl border border-border bg-card/40">
				{#each faq as item}
					<div class="p-5">
						<dt><h3 class="font-semibold">{item.question}</h3></dt>
						<dd class="mt-2 text-sm leading-relaxed text-muted-foreground">{item.answer}</dd>
					</div>
				{/each}
			</dl>
		</section>

		<section class="mt-16" aria-labelledby="sources-heading">
			<h2 id="sources-heading" class="text-xl font-semibold tracking-tight">Sources</h2>
			<div class="mt-4 grid gap-x-8 gap-y-5 text-xs sm:grid-cols-2 lg:grid-cols-3">
				{#each competitors as competitor}
					<div>
						<h3 class="font-medium text-foreground">{competitor.name}</h3>
						<ul class="mt-1.5 space-y-1">
							{#each competitor.sources as source}
								<li><a href={source.url} target="_blank" rel="noopener noreferrer" class="break-words text-muted-foreground underline-offset-4 hover:text-foreground hover:underline">{source.label}</a></li>
							{/each}
						</ul>
					</div>
				{/each}
			</div>
			<p class="mt-8 rounded-lg border border-white/5 bg-white/[0.02] p-4 text-xs leading-relaxed text-muted-foreground">
				Snapshot checked on 6 November 2026. Features and prices change; each vendor’s own pages are authoritative. Kuayle is
				developed by Bakney and is not affiliated with any tool listed. Product names are trademarks of their owners.
			</p>
		</section>

		<CtaSection />
	</article>
</main>

<Footer />
