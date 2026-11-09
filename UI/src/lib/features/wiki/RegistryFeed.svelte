<script lang="ts">
	import { onMount } from 'svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import { Skeleton } from '$lib/components/ui/skeleton/index.js';
	import * as Popover from '$lib/components/ui/popover';
	import * as ToggleGroup from '$lib/components/ui/toggle-group/index.js';
	import EmptyState from '$lib/components/shared/EmptyState.svelte';
	import { mentionInteractivity } from '$lib/components/shared/mention/mention-interactivity.action';
	import { createRegistryOverview, listRegistryDays, listRegistryOverviews } from '$lib/api/wiki';
	import type { RegistryDay, RegistryOverview, WikiScope } from '$lib/types/wiki';
	import type { WorkspaceMember } from '$lib/types/workspace';
	import type { Issue } from '$lib/types/issue';
	import { sanitizeHtml } from '$lib/security/sanitize';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/paraglide/messages.js';
	import { getLocale } from '$lib/paraglide/runtime.js';
	import { CalendarDays, ExternalLink, Search, Sparkles } from '@lucide/svelte';
	import { registryEntries } from './wiki-content';
	import { recentPeriod, relativeDay, validPeriod } from './registry';
	import RegistrySchedule from './RegistrySchedule.svelte';

	// The registry read as one timeline: days newest first under sticky
	// headers, each entry with its time. Each day is still a wiki page; its
	// header links there for editing and history.
	let {
		slug,
		scope,
		canWrite,
		members,
		issues
	}: {
		slug: string;
		scope: WikiScope;
		canWrite: boolean;
		members: WorkspaceMember[];
		issues: Issue[];
	} = $props();

	let days = $state<RegistryDay[]>([]);
	let nextBefore = $state<string | null>(null);
	let feedState = $state<'loading' | 'ready' | 'failed'>('loading');
	let loadingMore = $state(false);
	let overviews = $state<RegistryOverview[]>([]);
	let overviewsNext = $state<string | null>(null);
	let showPast = $state(false);
	let generateOpen = $state(false);
	let generating = $state(false);
	let generateError = $state<string | null>(null);
	let preset = $state<'1' | '7' | 'custom'>('1');
	let customStart = $state('');
	let customEnd = $state('');
	let sentinel = $state<HTMLElement | null>(null);
	let query = $state('');

	// Days with their parsed entries, filtered by the search across loaded days.
	const shownDays = $derived.by(() => {
		const needle = query.trim().toLowerCase();
		return days
			.map((day) => {
				const entries = registryEntries(day.content);
				return {
					day,
					total: entries.length,
					entries: needle
						? entries.filter((entry) =>
								entry.html
									.replace(/<[^>]*>/g, '')
									.toLowerCase()
									.includes(needle)
							)
						: entries
				};
			})
			.filter((item) => !needle || item.entries.length > 0);
	});
	let request = 0;

	const latestOverview = $derived(overviews[0]);
	const period = $derived(
		preset === 'custom'
			? { period_start: customStart, period_end: customEnd }
			: recentPeriod(Number(preset), new Date())
	);
	const periodValid = $derived(validPeriod(period.period_start, period.period_end));

	async function loadFirst(showLoading = true) {
		const current = ++request;
		if (showLoading) feedState = 'loading';
		try {
			const page = await listRegistryDays(slug, scope);
			if (current !== request) return;
			days = page.days;
			nextBefore = page.next_before;
			feedState = 'ready';
		} catch {
			if (current === request) feedState = 'failed';
		}
		listRegistryOverviews(slug, scope)
			.then((page) => {
				if (current !== request) return;
				overviews = page.overviews;
				overviewsNext = page.next_before;
			})
			.catch(() => {});
	}

	async function loadOlder() {
		if (!nextBefore || loadingMore) return;
		loadingMore = true;
		const current = request;
		try {
			const page = await listRegistryDays(slug, scope, nextBefore);
			if (current !== request) return;
			days = [...days, ...page.days.filter((day) => !days.some((known) => known.date === day.date))];
			nextBefore = page.next_before;
		} catch {
			appToast.error(m['registry.load_failed']());
		} finally {
			loadingMore = false;
		}
	}

	async function loadMoreOverviews() {
		if (!overviewsNext) return;
		try {
			const page = await listRegistryOverviews(slug, scope, overviewsNext);
			overviews = [...overviews, ...page.overviews];
			overviewsNext = page.next_before;
		} catch {
			appToast.error(m['registry.load_failed']());
		}
	}

	async function generate() {
		if (!periodValid || generating) return;
		generating = true;
		generateError = null;
		try {
			const overview = await createRegistryOverview(slug, scope, period);
			overviews = [overview, ...overviews];
			generateOpen = false;
		} catch (error) {
			const code = (error as { error?: { code?: string } }).error?.code;
			if (code === 'AI_NOT_CONFIGURED') generateError = m['registry.overview.ai_missing']();
			else if (code === 'NO_REGISTRY_ENTRIES') generateError = m['registry.overview.nothing']();
			else appToast.apiError(error, m['registry.overview.failed']());
		} finally {
			generating = false;
		}
	}

	function dayLabel(day: string): string {
		const relative = relativeDay(day, new Date());
		if (relative === 'today') return m['registry.today']();
		if (relative === 'yesterday') return m['registry.yesterday']();
		const [y, mo, d] = day.split('-').map(Number);
		return new Date(y, mo - 1, d).toLocaleDateString(getLocale(), {
			weekday: 'long',
			month: 'long',
			day: 'numeric',
			year: 'numeric'
		});
	}

	// Entry times are logged in UTC; show them in the viewer's time.
	function localTime(day: string, time: string | null): { label: string; utc: string } | null {
		if (!time) return null;
		const at = new Date(`${day}T${time}:00Z`);
		if (Number.isNaN(at.getTime())) return { label: time, utc: `${time} UTC` };
		return { label: at.toLocaleTimeString(getLocale(), { hour: 'numeric', minute: '2-digit' }), utc: `${time} UTC` };
	}

	function jumpTo(date: string) {
		document.getElementById(`registry-day-${date}`)?.scrollIntoView({ behavior: 'smooth', block: 'start' });
	}

	function shortDay(day: string): string {
		const [y, mo, d] = day.split('-').map(Number);
		return new Date(y, mo - 1, d).toLocaleDateString(getLocale(), { weekday: 'short', month: 'short', day: 'numeric' });
	}

	function periodLabel(overview: RegistryOverview): string {
		const format = (day: string) => {
			const [y, mo, d] = day.split('-').map(Number);
			return new Date(y, mo - 1, d).toLocaleDateString(getLocale(), { month: 'short', day: 'numeric' });
		};
		return overview.period_start === overview.period_end
			? format(overview.period_start)
			: m['registry.overview.range']({ start: format(overview.period_start), end: format(overview.period_end) });
	}

	onMount(() => {
		void loadFirst();
		const onWikiChanged = (event: Event) => {
			const detail = (event as CustomEvent<{ team_id?: string | null; project_id?: string | null }>).detail;
			if ((scope.kind === 'team' ? detail?.team_id : detail?.project_id) === scope.id) void loadFirst(false);
		};
		window.addEventListener('ws:wiki-changed', onWikiChanged);
		return () => {
			request++;
			window.removeEventListener('ws:wiki-changed', onWikiChanged);
		};
	});

	// Infinite scroll: reaching the end loads older days.
	$effect(() => {
		if (!sentinel || typeof IntersectionObserver === 'undefined') return;
		const observer = new IntersectionObserver((entries) => {
			if (entries.some((entry) => entry.isIntersecting)) void loadOlder();
		});
		observer.observe(sentinel);
		return () => observer.disconnect();
	});
</script>

{#snippet overviewCard(overview: RegistryOverview)}
	<article
		class="rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-3"
		data-testid="registry-overview"
	>
		<header
			class="mb-1.5 flex flex-wrap items-center gap-x-1.5 gap-y-0.5 text-[11px] text-[var(--color-text-tertiary)]"
		>
			<span class="flex items-center gap-1 font-medium text-[var(--color-text-secondary)]"
				><Sparkles size={12} />{m['registry.overview.label']()}</span
			>
			<span aria-hidden="true">·</span>
			<span>{periodLabel(overview)}</span>
			{#if overview.trigger === 'schedule'}<span aria-hidden="true">·</span><span
					>{m['registry.overview.by_schedule']()}</span
				>{/if}
		</header>
		<div
			class="prose prose-sm max-w-none text-[13px] text-[var(--color-text-primary)] dark:prose-invert [&>*:first-child]:mt-0 [&>*:last-child]:mb-0"
			use:mentionInteractivity={{ slug, members, issues }}
		>
			{@html sanitizeHtml(overview.content)}
		</div>
		<time datetime={overview.generated_at} class="mt-2 block text-[11px] text-[var(--color-text-tertiary)]"
			>{m['registry.overview.generated']({
				date: new Date(overview.generated_at).toLocaleString(getLocale(), { dateStyle: 'medium', timeStyle: 'short' })
			})}</time
		>
	</article>
{/snippet}

<div class="min-h-0 flex-1 overflow-y-auto md:flex md:overflow-hidden" data-testid="registry-feed">
	<div class="min-w-0 md:min-h-0 md:flex-1 md:overflow-y-auto">
		<div class="mx-auto max-w-[840px] px-4 pb-6 sm:px-10">
			<header class="flex flex-wrap items-center gap-2 pt-5 pb-3 sm:pt-6">
				<h1 class="mr-auto flex items-center gap-2 text-lg font-semibold text-[var(--color-text-primary)]">
					<CalendarDays size={18} class="text-[var(--color-text-tertiary)]" />
					{m['wiki.registry']()}
				</h1>
				<div class="relative w-full sm:w-48">
					<Search
						size={14}
						class="pointer-events-none absolute top-1/2 left-2.5 -translate-y-1/2 text-[var(--color-text-tertiary)]"
					/>
					<Input
						type="search"
						bind:value={query}
						placeholder={m['registry.search']()}
						aria-label={m['registry.search']()}
						class="h-8 pl-8 text-sm"
					/>
				</div>
				{#if canWrite}
					<div class="flex items-center gap-1.5">
						<RegistrySchedule {slug} {scope} />
						<Popover.Root bind:open={generateOpen}>
							<Popover.Trigger>
								{#snippet child({ props })}
									<Button {...props} variant="outline" size="sm" title={m['registry.overview.generate_hint']()}>
										<Sparkles size={14} />
										{m['registry.overview.generate']()}
									</Button>
								{/snippet}
							</Popover.Trigger>
							<Popover.Content class="grid w-80 max-w-[calc(100vw-1rem)] gap-3" align="end">
								<p class="text-sm font-medium">{m['registry.overview.period']()}</p>
								<ToggleGroup.Root
									type="single"
									variant="outline"
									size="sm"
									value={preset}
									onValueChange={(value) => {
										if (value) preset = value as typeof preset;
									}}
								>
									<ToggleGroup.Item value="1" class="px-3 text-xs">{m['registry.overview.today']()}</ToggleGroup.Item>
									<ToggleGroup.Item value="7" class="px-3 text-xs">{m['registry.overview.week']()}</ToggleGroup.Item>
									<ToggleGroup.Item value="custom" class="px-3 text-xs"
										>{m['registry.overview.custom']()}</ToggleGroup.Item
									>
								</ToggleGroup.Root>
								{#if preset === 'custom'}
									<div class="grid grid-cols-2 gap-2">
										<div class="grid gap-1">
											<Label for="overview-from" class="text-xs">{m['registry.overview.from']()}</Label>
											<Input id="overview-from" type="date" bind:value={customStart} />
										</div>
										<div class="grid gap-1">
											<Label for="overview-to" class="text-xs">{m['registry.overview.to']()}</Label>
											<Input id="overview-to" type="date" bind:value={customEnd} />
										</div>
									</div>
									{#if (customStart || customEnd) && !periodValid}
										<p class="text-xs text-[var(--color-error)]">{m['registry.overview.invalid']()}</p>
									{/if}
								{/if}
								{#if generateError}
									<p role="alert" class="text-xs text-[var(--color-error)]">{generateError}</p>
								{/if}
								<Button variant="outline" onclick={generate} disabled={!periodValid || generating}>
									<Sparkles size={14} />
									{generating ? m['registry.overview.generating']() : m['registry.overview.generate']()}
								</Button>
							</Popover.Content>
						</Popover.Root>
					</div>
				{/if}
			</header>

			{#if generating}
				<div role="status" class="mb-3 space-y-2 rounded-lg border border-[var(--app-border)] p-3">
					<p class="text-xs text-[var(--color-text-tertiary)]">{m['registry.overview.generating']()}</p>
					<Skeleton class="h-3.5 w-5/6" />
					<Skeleton class="h-3.5 w-2/3" />
				</div>
			{/if}
			{#if latestOverview}
				<div class="mb-4 lg:hidden">{@render overviewCard(latestOverview)}</div>
			{/if}

			{#if feedState === 'loading'}
				<div role="status" aria-label={m['common.loading']()} class="space-y-3">
					<Skeleton class="h-5 w-40" />
					<Skeleton class="h-4 w-full" />
					<Skeleton class="h-4 w-4/5" />
				</div>
			{:else if feedState === 'failed'}
				<EmptyState
					title={m['registry.load_failed']()}
					action={{ label: m['wiki.retry'](), onclick: () => loadFirst() }}
				/>
			{:else if days.length === 0}
				<EmptyState title={m['registry.empty_title']()} description={m['registry.empty']()} />
			{:else if shownDays.length === 0}
				<p class="py-6 text-center text-sm text-[var(--color-text-tertiary)]">{m['registry.no_matches']()}</p>
			{:else}
				<ol class="grid gap-3">
					{#each shownDays as { day, entries, total } (day.date)}
						<li aria-labelledby="registry-day-{day.date}" class="scroll-mt-2">
							<div
								class="group/day sticky top-0 z-10 flex items-center gap-2 border-b border-[var(--app-border)] bg-[var(--color-bg)] py-1.5"
							>
								<h2
									id="registry-day-{day.date}"
									class="scroll-mt-2 text-[13px] font-semibold text-[var(--color-text-primary)]"
								>
									<time datetime={day.date}>{dayLabel(day.date)}</time>
								</h2>
								<span class="text-[11px] text-[var(--color-text-tertiary)]"
									>{m['registry.entry_count']({ count: total })}</span
								>
								<a
									href="?page={day.page_id}"
									class="ml-auto rounded p-1 text-[var(--color-text-tertiary)] opacity-0 transition-opacity group-focus-within/day:opacity-100 group-hover/day:opacity-100 hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)] focus-visible:opacity-100"
									title={m['registry.open_day_hint']()}
									aria-label={m['registry.open_day_hint']()}
								>
									<ExternalLink size={12} />
								</a>
							</div>
							<ul class="relative mt-1" use:mentionInteractivity={{ slug, members, issues }}>
								{#each entries as entry, index (index)}
									{@const time = localTime(day.date, entry.time)}
									<li
										class="grid grid-cols-[1rem_minmax(0,1fr)] gap-x-2 rounded-md py-1.5 pr-2 text-[13px] leading-5 hover:bg-[var(--color-bg-hover)] sm:grid-cols-[4.75rem_1rem_minmax(0,1fr)]"
									>
										<span
											class="col-span-2 pl-1 text-[11px] whitespace-nowrap text-[var(--color-text-tertiary)] tabular-nums sm:col-span-1 sm:pt-px sm:pl-2 sm:text-right"
											title={time?.utc}>{time?.label ?? ''}</span
										>
										<span aria-hidden="true" class="relative flex justify-center">
											<span class="absolute inset-y-[-0.375rem] w-px bg-[var(--app-border)]"></span>
											<span
												class="relative mt-1.5 h-2 w-2 rounded-full border border-[var(--color-bg)] bg-[var(--color-text-tertiary)]"
											></span>
										</span>
										<div class="registry-entry min-w-0 break-words text-[var(--color-text-primary)] [&_a]:underline">
											{@html sanitizeHtml(entry.html)}
										</div>
									</li>
								{:else}
									<li class="px-2 py-1 text-xs text-[var(--color-text-tertiary)]">{m['registry.day_empty']()}</li>
								{/each}
							</ul>
						</li>
					{/each}
				</ol>
				<div bind:this={sentinel} class="h-px"></div>
				{#if nextBefore}
					<div class="mt-3 flex justify-center">
						<Button variant="outline" size="sm" disabled={loadingMore} onclick={loadOlder}
							>{m['registry.load_older']()}</Button
						>
					</div>
				{/if}
			{/if}
		</div>
	</div>

	<aside
		aria-label={m['registry.side']()}
		class="hidden w-[18.75rem] shrink-0 space-y-2 overflow-y-auto border-l border-[var(--app-border)] p-3 lg:block"
	>
		{#if latestOverview}
			{@render overviewCard(latestOverview)}
		{/if}
		{#if overviews.length > 1 || overviewsNext}
			<div class="rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-2">
				<Button
					variant="plain"
					class="w-full px-1 py-1 text-left text-[11px] font-medium tracking-wider text-[var(--color-text-tertiary)] uppercase hover:text-[var(--color-text-secondary)]"
					aria-expanded={showPast}
					onclick={() => (showPast = !showPast)}>{m['registry.overview.past']()}</Button
				>
				{#if showPast}
					<div class="mt-1 grid gap-2">
						{#each overviews.slice(1) as overview (overview.id)}
							{@render overviewCard(overview)}
						{/each}
						{#if overviewsNext}
							<Button variant="outline" size="sm" onclick={loadMoreOverviews}>{m['registry.load_older']()}</Button>
						{/if}
					</div>
				{/if}
			</div>
		{/if}
		{#if days.length > 0}
			<nav
				aria-label={m['registry.jump']()}
				class="rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-2"
			>
				<h2 class="px-1 pb-1 text-[11px] font-medium tracking-wider text-[var(--color-text-tertiary)] uppercase">
					{m['registry.jump']()}
				</h2>
				<ul>
					{#each shownDays as { day, total } (day.date)}
						<li>
							<Button
								variant="plain"
								onclick={() => jumpTo(day.date)}
								class="flex w-full items-center justify-between rounded-md px-2 py-1 text-left text-[13px] text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]"
							>
								<span class="truncate"
									>{relativeDay(day.date, new Date()) ? dayLabel(day.date) : shortDay(day.date)}</span
								>
								<span class="text-[11px] text-[var(--color-text-tertiary)] tabular-nums">{total}</span>
							</Button>
						</li>
					{/each}
				</ul>
				{#if nextBefore}
					<Button
						variant="plain"
						class="mt-1 w-full px-2 py-1 text-left text-xs text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)]"
						onclick={loadOlder}>{m['registry.load_older']()}</Button
					>
				{/if}
			</nav>
		{/if}
	</aside>
</div>

<style>
	.registry-entry :global(.registry-ref) {
		display: inline-flex;
		align-items: center;
		border-radius: 0.25rem;
		background: var(--color-bg-tertiary);
		padding: 0 0.3rem;
		font-size: 12px;
		color: var(--color-text-secondary);
		white-space: nowrap;
	}
</style>
