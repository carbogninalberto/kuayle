<script lang="ts">
	import ArrowRight from '@lucide/svelte/icons/arrow-right';
	import Check from '@lucide/svelte/icons/check';
	import * as ToggleGroup from '$lib/components/ui/toggle-group';
	import { reveal } from '$lib/actions/reveal';
	import {
		alternativePath,
		annualCostAtMonthlyBilling,
		annualLicenseCost,
		formatUsd
	} from '$lib/data/competitor-types';
	import { competitorBySlug } from '$lib/data/alternative-pages';

	const SIZES = ['10', '25', '50', '100'] as const;
	let size = $state<string>('25');
	const people = $derived(Number(size) || 25);

	const PRICED = ['asana', 'linear', 'clickup', 'jira', 'plane'].map(competitorBySlug);

	const rows = $derived(
		PRICED.map((competitor) => {
			const yearly = annualLicenseCost(competitor, people);
			const monthly = yearly === null ? annualCostAtMonthlyBilling(competitor, people) : null;
			return {
				competitor,
				plan: competitor.pricing.entryPlan,
				cost: yearly ?? monthly ?? 0,
				billing: yearly === null ? 'monthly billing × 12' : 'billed yearly'
			};
		}).sort((a, b) => b.cost - a.cost)
	);
	const max = $derived(Math.max(...rows.map((row) => row.cost), 1));

	const included = [
		{ feature: 'Any number of users and issues', gated: 'Linear Free stops at 2 teams and 250 issues; Jira Free at 10 users; Trello Free at 10 collaborators.', href: '/linear-alternative' },
		{ feature: 'GitHub integration', gated: 'Plane’s GitHub and GitLab integrations start on the Pro plan.', href: '/plane-alternative' },
		{ feature: 'Multiple workspaces', gated: 'It’s a Plan puts multiple workspaces in its $240-a-year Pro edition.', href: '/itsaplan-alternative' },
		{ feature: 'Full workspace export', gated: 'Trello needs Premium for CSV and Workspace export; ClickUp list exports start on Business.', href: '/trello-alternative' }
	];
</script>

<section id="pricing" class="relative py-24 sm:py-32" aria-labelledby="pricing-heading">
	<div class="mx-auto max-w-6xl px-6">
		<div class="max-w-2xl" use:reveal>
			<p class="font-mono text-xs tracking-widest text-brand-300 uppercase">Pricing</p>
			<h2 id="pricing-heading" class="mt-3 text-3xl font-bold tracking-tight sm:text-4xl">$0 per user, at any team size</h2>
			<p class="mt-4 text-lg text-muted-foreground">
				Kuayle has no licence fee, no per-seat price and no paid tier. You pay for the server you run it on. Here is what
				the same team pays for an entry plan elsewhere.
			</p>
		</div>

		<div class="mt-12 grid gap-8 lg:grid-cols-[1.4fr_1fr]">
			<div class="rounded-2xl border border-dashed border-white/15 bg-card/50 p-5 sm:p-7" use:reveal>
				<div class="flex flex-wrap items-center justify-between gap-3">
					<p id="team-size-label" class="text-sm font-medium">Yearly licence cost for</p>
					<ToggleGroup.Root
						type="single"
						bind:value={size}
						aria-labelledby="team-size-label"
						variant="outline"
						size="sm"
						onValueChange={(value) => {
							if (!value) size = '25';
						}}
					>
						{#each SIZES as option}
							<ToggleGroup.Item value={option} class="px-3 font-mono" aria-label="{option} people">{option}</ToggleGroup.Item>
						{/each}
					</ToggleGroup.Root>
				</div>

				<ul class="mt-6 space-y-3" aria-live="polite">
					{#each rows as row (row.competitor.slug)}
						<li class="grid grid-cols-[6.5rem_1fr_auto] items-center gap-3 text-sm sm:grid-cols-[8rem_1fr_auto]">
							<a href={alternativePath(row.competitor)} class="truncate text-muted-foreground hover:text-foreground">
								{row.competitor.displayName ?? row.competitor.name}
								<span class="block text-[0.7rem]">{row.plan}</span>
							</a>
							<span class="h-2 overflow-hidden rounded-full bg-white/5" aria-hidden="true">
								<span class="block h-full rounded-full bg-white/25" style="width: {(row.cost / max) * 100}%"></span>
							</span>
							<span class="text-right font-mono">{formatUsd(Math.round(row.cost))}</span>
						</li>
					{/each}
					<li class="grid grid-cols-[6.5rem_1fr_auto] items-center gap-3 border-t border-dashed border-white/15 pt-3 text-sm sm:grid-cols-[8rem_1fr_auto]">
						<span class="font-semibold text-brand-200">Kuayle</span>
						<span class="h-2 rounded-full bg-white/5" aria-hidden="true"></span>
						<span class="text-right font-mono text-brand-200">$0</span>
					</li>
				</ul>
				<p class="mt-5 text-xs leading-relaxed text-muted-foreground">
					Entry paid plans at list price for {people} people, before tax, checked on 6 November 2026. Jira publishes
					per-user prices for monthly billing only, shown × 12. Vendors may discount larger teams or sell seats in
					increments. Kuayle’s cost is your own infrastructure and the time to run it.
				</p>
			</div>

			<div class="rounded-2xl border border-brand-400/25 bg-brand-400/5 p-5 sm:p-7" use:reveal={{ delay: 100 }}>
				<h3 class="text-lg font-semibold">Included, not upsold</h3>
				<p class="mt-1 text-sm text-muted-foreground">One edition. Nothing behind a licence key.</p>
				<ul class="mt-5 space-y-4">
					{#each included as item}
						<li class="flex gap-3">
							<Check class="mt-0.5 size-4 shrink-0 text-brand-300" aria-hidden="true" />
							<div>
								<p class="text-sm font-medium">{item.feature}</p>
								<p class="mt-0.5 text-xs leading-relaxed text-muted-foreground">{item.gated}</p>
							</div>
						</li>
					{/each}
				</ul>
				<a href="/compare" class="mt-6 inline-flex items-center gap-1 text-sm font-medium text-brand-300 hover:text-brand-200">
					See the full comparison <ArrowRight class="size-4" aria-hidden="true" />
				</a>
			</div>
		</div>
	</div>
</section>
