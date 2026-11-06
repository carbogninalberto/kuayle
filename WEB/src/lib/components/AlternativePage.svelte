<script lang="ts">
	import ArrowRight from '@lucide/svelte/icons/arrow-right';
	import ExternalLink from '@lucide/svelte/icons/external-link';
	import Breadcrumbs from '$lib/components/Breadcrumbs.svelte';
	import CtaSection from '$lib/components/CtaSection.svelte';
	import Footer from '$lib/components/Footer.svelte';
	import GithubIcon from '$lib/components/GithubIcon.svelte';
	import MarkLabel from '$lib/components/MarkLabel.svelte';
	import Nav from '$lib/components/Nav.svelte';
	import Seo from '$lib/components/Seo.svelte';
	import { Button } from '$lib/components/ui/button';
	import * as Table from '$lib/components/ui/table';
	import { reveal } from '$lib/actions/reveal';
	import { url } from '$lib/config/site';
	import {
		COMPETITORS_CHECKED_AT,
		alternativePath,
		annualCostAtMonthlyBilling,
		annualLicenseCost,
		formatUsd,
		hostingLabel as hostingLabelFor,
		type AlternativePageContent,
		type Competitor
	} from '$lib/data/competitor-types';
	import { KUAYLE, KUAYLE_FACTS_VERSION } from '$lib/data/kuayle-facts';
	import { faqLd, webPageLd, type Crumb } from '$lib/data/routes';

	let {
		content,
		competitor,
		others,
		modifiedAt
	}: {
		content: AlternativePageContent;
		competitor: Competitor;
		others: Competitor[];
		modifiedAt: string;
	} = $props();

	const name = $derived(competitor.displayName ?? competitor.name);
	const path = $derived(alternativePath(competitor));
	const canonical = $derived(url(path));
	const crumbs: Crumb[] = $derived([
		{ label: 'Home', href: '/' },
		{ label: 'Alternatives', href: '/alternatives' },
		{ label: `${name} alternative`, href: path }
	]);

	const meta = $derived({
		title: content.title,
		description: content.description,
		canonical,
		ogType: 'article' as const,
		modifiedAt,
		jsonLd: [
			webPageLd(content.title, content.description, canonical, crumbs, modifiedAt),
			faqLd(content.faq)
		]
	});

	const checkedOn = new Date(`${COMPETITORS_CHECKED_AT}T00:00:00Z`).toLocaleDateString('en-GB', {
		day: 'numeric',
		month: 'long',
		year: 'numeric',
		timeZone: 'UTC'
	});

	const teamSizes = [5, 20, 50, 100];

	function cost(people: number, plan: 'entry' | 'business'): string {
		const value = annualLicenseCost(competitor, people, plan) ?? annualCostAtMonthlyBilling(competitor, people, plan);
		return value === null ? '—' : formatUsd(value);
	}

	const hostingLabel = $derived(
		hostingLabelFor(competitor.hosting)
	);

	const pricingLabel = $derived.by(() => {
		const p = competitor.pricing;
		if (p.model === 'free') return 'Free';
		if (p.model === 'flat' && p.flatAnnualUsd) return `${formatUsd(p.flatAnnualUsd)} a year, flat`;
		if (p.model === 'per-seat' && p.monthlyPerSeatAnnualBillingUsd !== null)
			return `From ${formatUsd(p.monthlyPerSeatAnnualBillingUsd)} per user a month`;
		if (p.model === 'quote') return 'Price on request';
		return p.entryPlan ?? 'See vendor pricing';
	});

	const verdict = $derived([
		{ label: 'Licence', theirs: competitor.license, ours: 'Apache 2.0' },
		{ label: 'Hosting', theirs: hostingLabel, ours: 'Self-hosted' },
		{ label: 'Pricing', theirs: pricingLabel, ours: 'Free, no paid tier' }
	]);
</script>

<Seo {meta} />
<Nav />

<main id="main" tabindex="-1" class="mx-auto max-w-6xl px-6 pt-28 pb-20 outline-none">
	<Breadcrumbs breadcrumbs={crumbs} />

	<article class="mt-8 min-w-0 [overflow-wrap:anywhere]">
		<!-- Verdict -->
		<header class="grid gap-10 lg:grid-cols-[1.15fr_1fr] lg:items-start">
			<div use:reveal>
				<p class="font-mono text-xs tracking-widest text-brand-300 uppercase">{name} alternative</p>
				<h1 class="mt-3 text-4xl leading-tight font-bold tracking-tight sm:text-5xl">{content.h1}</h1>
				<p class="mt-5 text-lg leading-relaxed text-muted-foreground">{content.intro}</p>
				<div class="mt-8 flex flex-wrap gap-3">
					<Button href="/self-hosting" size="lg" class="h-11 bg-brand-400 px-6 text-base text-white hover:bg-brand-500">
						Self-host Kuayle
						<ArrowRight />
					</Button>
					<Button
						href="https://github.com/carbogninalberto/kuayle"
						target="_blank"
						rel="noopener"
						variant="outline"
						size="lg"
						class="h-11 px-6 text-base"
					>
						<GithubIcon />
						View source
					</Button>
				</div>
			</div>

			<section aria-labelledby="verdict-heading" class="rounded-2xl border border-border bg-card/70 p-5 sm:p-6" use:reveal={{ delay: 100 }}>
				<h2 id="verdict-heading" class="font-mono text-xs tracking-widest text-muted-foreground uppercase">At a glance</h2>
				<dl class="mt-4 divide-y divide-dashed divide-white/10">
					{#each verdict as row}
						<div class="grid grid-cols-[5.5rem_1fr] gap-3 py-3 text-sm">
							<dt class="text-muted-foreground">{row.label}</dt>
							<dd class="space-y-1">
								<p><span class="text-muted-foreground">{name}:</span> {row.theirs}</p>
								<p><span class="text-brand-200">Kuayle:</span> {row.ours}</p>
							</dd>
						</div>
					{/each}
					<div class="grid grid-cols-[5.5rem_1fr] gap-3 py-3 text-sm">
						<dt class="text-muted-foreground">Difference</dt>
						<dd>{content.difference}</dd>
					</div>
				</dl>
			</section>
		</header>

		<!-- Why leave -->
		<section class="mt-20" aria-labelledby="why-heading">
			<h2 id="why-heading" class="text-2xl font-semibold tracking-tight sm:text-3xl">Why teams look for a {name} alternative</h2>
			<div class="mt-6 grid gap-4 sm:grid-cols-2">
				{#each content.whyLeave as reason, i}
					<div class="rounded-xl border border-border bg-card/50 p-5" use:reveal={{ delay: i * 50 }}>
						<h3 class="font-semibold">{reason.heading}</h3>
						<p class="mt-2 text-sm leading-relaxed text-muted-foreground">{reason.body}</p>
					</div>
				{/each}
			</div>
		</section>

		<!-- Side by side -->
		<section class="mt-20" aria-labelledby="table-heading">
			<h2 id="table-heading" class="text-2xl font-semibold tracking-tight sm:text-3xl">Kuayle vs {name}, side by side</h2>
			<div class="mt-6 rounded-xl border border-border bg-card/40">
				<Table.Root class="min-w-[40rem] table-fixed">
					<Table.Caption class="px-4 pb-4 text-left text-xs">
						{name} details checked against {competitor.name}'s own pages on {checkedOn}. Kuayle details verified
						against the {KUAYLE_FACTS_VERSION} source code.
					</Table.Caption>
					<Table.Header>
						<Table.Row>
							<Table.Head class="w-[8.5rem] px-4">Topic</Table.Head>
							<Table.Head class="px-4">{name}</Table.Head>
							<Table.Head class="px-4">Kuayle</Table.Head>
						</Table.Row>
					</Table.Header>
					<Table.Body>
						{#each content.rows as row}
							{@const ours = KUAYLE[row.kuayle]}
							<Table.Row class="align-top">
								<Table.Head scope="row" class="px-4 py-3 font-medium whitespace-normal text-foreground">{row.dimension}</Table.Head>
								<Table.Cell class="px-4 py-3 whitespace-normal">
									<MarkLabel mark={row.mark} />
									<p class="mt-1.5 text-muted-foreground">{row.text}</p>
								</Table.Cell>
								<Table.Cell class="px-4 py-3 whitespace-normal">
									<MarkLabel mark={ours.mark} />
									<p class="mt-1.5 text-muted-foreground">{ours.text}</p>
								</Table.Cell>
							</Table.Row>
						{/each}
					</Table.Body>
				</Table.Root>
			</div>
		</section>

		<!-- Cost -->
		<section class="mt-20" aria-labelledby="cost-heading">
			<h2 id="cost-heading" class="text-2xl font-semibold tracking-tight sm:text-3xl">What {name} costs a year compared with Kuayle</h2>
			<div class="mt-6 rounded-xl border border-dashed border-white/15 bg-card/40">
				<Table.Root class="min-w-[32rem]">
					<Table.Caption class="px-4 pb-4 text-left text-xs">{content.costNote}</Table.Caption>
					<Table.Header>
						<Table.Row>
							<Table.Head class="px-4">Team</Table.Head>
							{#if content.costPlans.entry}<Table.Head class="px-4">{name} {content.costPlans.entry}</Table.Head>{/if}
							{#if content.costPlans.business}<Table.Head class="px-4">{name} {content.costPlans.business}</Table.Head>{/if}
							<Table.Head class="px-4 text-brand-200">Kuayle licence</Table.Head>
						</Table.Row>
					</Table.Header>
					<Table.Body>
						{#each teamSizes as people}
							<Table.Row>
								<Table.Head scope="row" class="px-4 font-mono text-foreground">{people} people</Table.Head>
								{#if content.costPlans.entry}<Table.Cell class="px-4 font-mono">{cost(people, 'entry')}</Table.Cell>{/if}
								{#if content.costPlans.business}<Table.Cell class="px-4 font-mono">{cost(people, 'business')}</Table.Cell>{/if}
								<Table.Cell class="px-4 font-mono text-brand-200">$0</Table.Cell>
							</Table.Row>
						{/each}
					</Table.Body>
				</Table.Root>
			</div>
		</section>

		<!-- Migration -->
		{#if content.migration.length > 0}
			<section class="mt-20" aria-labelledby="migration-heading">
				<h2 id="migration-heading" class="text-2xl font-semibold tracking-tight sm:text-3xl">Moving from {name} to Kuayle</h2>
				<ol class="mt-6 grid gap-4 md:grid-cols-2">
					{#each content.migration as step, i}
						<li class="rounded-xl border border-border bg-card/50 p-5">
							<span class="font-mono text-xs text-brand-300">{String(i + 1).padStart(2, '0')}</span>
							<h3 class="mt-1 font-semibold">{step.heading}</h3>
							<p class="mt-2 text-sm leading-relaxed text-muted-foreground">{step.body}</p>
						</li>
					{/each}
				</ol>
			</section>
		{/if}

		<!-- Choice -->
		<section class="mt-20" aria-labelledby="choice-heading">
			<h2 id="choice-heading" class="text-2xl font-semibold tracking-tight sm:text-3xl">When to stay on {name}, and when to choose Kuayle</h2>
			<div class="mt-6 grid gap-4 md:grid-cols-2">
				<div class="rounded-xl border border-border bg-card/50 p-5">
					<h3 class="font-semibold">Stay on {name} if</h3>
					<ul class="mt-3 list-disc space-y-2 pl-5 text-sm leading-relaxed text-muted-foreground">
						{#each content.stayIf as item}<li>{item}</li>{/each}
					</ul>
				</div>
				<div class="rounded-xl border border-brand-400/30 bg-brand-400/5 p-5">
					<h3 class="font-semibold">Choose Kuayle if</h3>
					<ul class="mt-3 list-disc space-y-2 pl-5 text-sm leading-relaxed text-muted-foreground">
						{#each content.switchIf as item}<li>{item}</li>{/each}
					</ul>
				</div>
			</div>
		</section>

		<!-- Others -->
		{#if others.length > 0}
			<section class="mt-20" aria-labelledby="others-heading">
				<h2 id="others-heading" class="text-2xl font-semibold tracking-tight sm:text-3xl">Other {name} alternatives</h2>
				<p class="mt-3 max-w-3xl text-muted-foreground">
					Kuayle is not the only option. Compare these too, or see every tracker in the
					<a href="/compare" class="text-brand-300 underline-offset-4 hover:underline">full comparison matrix</a>.
				</p>
				<ul class="mt-6 grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
					{#each others as other}
						<li>
							<a href={alternativePath(other)} class="group block h-full rounded-xl border border-border bg-card/50 p-4 transition-colors hover:border-brand-400/40">
								<span class="flex items-center justify-between font-medium">
									{other.displayName ?? other.name} alternative
									<ArrowRight class="size-4 text-brand-300 transition-transform group-hover:translate-x-0.5" aria-hidden="true" />
								</span>
								<span class="mt-1 block text-xs text-muted-foreground">{other.license} · {hostingLabelFor(other.hosting)}</span>
							</a>
						</li>
					{/each}
				</ul>
			</section>
		{/if}

		<!-- FAQ -->
		<section class="mt-20" aria-labelledby="faq-heading">
			<h2 id="faq-heading" class="text-2xl font-semibold tracking-tight sm:text-3xl">{name} alternative FAQ</h2>
			<dl class="mt-6 divide-y divide-border rounded-xl border border-border bg-card/40">
				{#each content.faq as item}
					<div class="p-5">
						<dt><h3 class="font-semibold">{item.question}</h3></dt>
						<dd class="mt-2 text-sm leading-relaxed text-muted-foreground">{item.answer}</dd>
					</div>
				{/each}
			</dl>
		</section>

		<!-- Sources -->
		<section class="mt-20" aria-labelledby="sources-heading">
			<h2 id="sources-heading" class="text-xl font-semibold tracking-tight">Sources</h2>
			<ul class="mt-4 grid gap-2 text-sm sm:grid-cols-2">
				{#each competitor.sources as source}
					<li>
						<a href={source.url} target="_blank" rel="noopener noreferrer" class="inline-flex items-center gap-1.5 text-brand-300 underline-offset-4 hover:underline">
							{source.label}
							<ExternalLink class="size-3" aria-hidden="true" />
						</a>
					</li>
				{/each}
				<li>
					<a href="https://github.com/carbogninalberto/kuayle" target="_blank" rel="noopener noreferrer" class="inline-flex items-center gap-1.5 text-brand-300 underline-offset-4 hover:underline">
						Kuayle source code
						<ExternalLink class="size-3" aria-hidden="true" />
					</a>
				</li>
			</ul>
			<p class="mt-6 rounded-lg border border-white/5 bg-white/[0.02] p-4 text-xs leading-relaxed text-muted-foreground">
				{competitor.name} details were checked against {competitor.name}'s own pages on {checkedOn}; Kuayle details against
				the {KUAYLE_FACTS_VERSION} source code. Prices are list prices before tax and change over time; the vendor's pricing
				page is authoritative. This is a snapshot, not an endorsement. Kuayle is developed by Bakney and is not affiliated
				with {competitor.name}. {competitor.name} is a trademark of its owner.
			</p>
		</section>

		<CtaSection
			heading="Try Kuayle on your own server"
			description="Read the Apache 2.0 source, then follow the Docker Compose guide to run an instance with no licence fee."
		/>
	</article>
</main>

<Footer />
