<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import {
		Activity,
		BarChart3,
		Building2,
		ChartNoAxesCombined,
		LayoutDashboard,
		SlidersHorizontal,
		TrendingUp,
		UsersRound
	} from 'lucide-svelte';
	import * as Select from '$lib/components/ui/select';
	import * as Tabs from '$lib/components/ui/tabs';
	import SidebarToggle from '$lib/components/layout/SidebarToggle.svelte';
	import TeamIcon from '$lib/components/shared/TeamIcon.svelte';
	import LoadingState from '$lib/components/shared/LoadingState.svelte';
	import ErrorState from '$lib/components/shared/ErrorState.svelte';
	import { listTeams } from '$lib/api/teams';
	import { listTeamStatuses } from '$lib/api/team-statuses';
	import {
		defaultDateRange,
		getAnalyticsBurnup,
		getAnalyticsDistribution,
		getAnalyticsOverview,
		type AnalyticsBurnup,
		type AnalyticsDistribution,
		type AnalyticsOverview
	} from '$lib/api/analytics';
	import type { Team } from '$lib/types/team';
	import type { TeamStatus } from '$lib/types/team-status';
	import OverviewCards from '$lib/features/analytics/OverviewCards.svelte';
	import DistributionChart from '$lib/features/analytics/DistributionChart.svelte';
	import BurnupChart from '$lib/features/analytics/BurnupChart.svelte';
	import InsightsExplorer from '$lib/features/analytics/InsightsExplorer.svelte';
	import AnalyticsDateRangePicker from '$lib/features/analytics/AnalyticsDateRangePicker.svelte';

	const slug = $derived(page.params.workspaceSlug ?? '');
	type Tab = 'overview' | 'explore';
	type BurnupInterval = 'day' | 'week' | 'month';

	const initialTab = page.url.searchParams.get('tab');
	let activeTab = $state<Tab>(initialTab === 'explore' ? 'explore' : 'overview');
	let selectedTeamId = $state(page.url.searchParams.get('team') ?? 'workspace');
	let teams = $state<Team[]>([]);
	let statuses = $state<TeamStatus[]>([]);
	let teamsLoading = $state(true);
	const selectedTeam = $derived(teams.find((team) => team.id === selectedTeamId));
	const teamScoped = $derived(selectedTeamId !== 'workspace');
	const analyticsScope = $derived(teamScoped ? { team_id: selectedTeamId } : {});

	let overview = $state<AnalyticsOverview | null>(null);
	let distribution = $state<AnalyticsDistribution | null>(null);
	let overviewLoading = $state(true);
	let overviewError = $state<string | null>(null);
	let overviewLoadId = 0;

	const defaultRange = defaultDateRange(90);
	let burnupFrom = $state(defaultRange.from);
	let burnupTo = $state(defaultRange.to);
	let burnupInterval = $state<BurnupInterval>('week');
	let burnup = $state<AnalyticsBurnup | null>(null);
	let burnupLoading = $state(false);
	let burnupError = $state<string | null>(null);
	let burnupLoadId = 0;

	function errorMessage(error: unknown, fallback: string): string {
		if (error && typeof error === 'object' && 'error' in error) {
			const detail = error.error;
			if (detail && typeof detail === 'object' && 'message' in detail && typeof detail.message === 'string') {
				return detail.message;
			}
		}
		return error instanceof Error ? error.message : fallback;
	}

	async function loadTeams(requestSlug: string) {
		teamsLoading = true;
		try {
			teams = await listTeams(requestSlug);
			if (selectedTeamId !== 'workspace' && !teams.some((team) => team.id === selectedTeamId)) {
				selectedTeamId = 'workspace';
			}
		} catch {
			teams = [];
		} finally {
			teamsLoading = false;
		}
	}

	async function loadOverview(requestSlug = slug, teamId = selectedTeamId) {
		const id = ++overviewLoadId;
		overviewLoading = true;
		overviewError = null;
		try {
			const scope = teamId === 'workspace' ? {} : { team_id: teamId };
			const [nextOverview, nextDistribution] = await Promise.all([
				getAnalyticsOverview(requestSlug, scope),
				getAnalyticsDistribution(requestSlug, scope)
			]);
			if (id === overviewLoadId) {
				overview = nextOverview;
				distribution = nextDistribution;
			}
		} catch (error: unknown) {
			if (id === overviewLoadId) {
				overviewError = errorMessage(error, 'Failed to load analytics');
				overview = null;
				distribution = null;
			}
		} finally {
			if (id === overviewLoadId) overviewLoading = false;
		}
	}

	async function loadBurnup(
		requestSlug = slug,
		teamId = selectedTeamId,
		from = burnupFrom,
		to = burnupTo,
		interval = burnupInterval
	) {
		const id = ++burnupLoadId;
		burnupLoading = true;
		burnupError = null;
		if (!from || !to) {
			burnupError = 'Choose a date range';
			burnupLoading = false;
			return;
		}
		try {
			const nextBurnup = await getAnalyticsBurnup(requestSlug, {
				from,
				to,
				interval,
				...(teamId === 'workspace' ? {} : { team_id: teamId })
			});
			if (id === burnupLoadId) burnup = nextBurnup;
		} catch (error: unknown) {
			if (id === burnupLoadId) {
				burnupError = errorMessage(error, 'Failed to load burn-up');
				burnup = null;
			}
		} finally {
			if (id === burnupLoadId) burnupLoading = false;
		}
	}

	$effect(() => {
		const requestSlug = slug;
		if (requestSlug) void loadTeams(requestSlug);
	});

	$effect(() => {
		const teamId = selectedTeamId;
		if (teamId === 'workspace') {
			statuses = [];
			return;
		}
		statuses = [];
		let current = true;
		void listTeamStatuses(slug, teamId)
			.then((nextStatuses) => {
				if (current && teamId === selectedTeamId) statuses = nextStatuses;
			})
			.catch(() => {
				if (current && teamId === selectedTeamId) statuses = [];
			});
		return () => {
			current = false;
		};
	});

	$effect(() => {
		void loadOverview(slug, selectedTeamId);
		return () => {
			overviewLoadId += 1;
		};
	});

	$effect(() => {
		void loadBurnup(slug, selectedTeamId, burnupFrom, burnupTo, burnupInterval);
		return () => {
			burnupLoadId += 1;
		};
	});

	$effect(() => {
		const url = new URL(page.url.href);
		const currentTab = url.searchParams.get('tab') ?? 'overview';
		const currentTeam = url.searchParams.get('team') ?? 'workspace';
		if (currentTab === activeTab && currentTeam === selectedTeamId) return;
		if (activeTab === 'overview') url.searchParams.delete('tab');
		else url.searchParams.set('tab', activeTab);
		if (selectedTeamId === 'workspace') url.searchParams.delete('team');
		else url.searchParams.set('team', selectedTeamId);
		void goto(url.pathname + url.search, { replaceState: true, noScroll: true, keepFocus: true });
	});
</script>

<div class="flex h-full flex-col bg-[var(--color-bg)]">
	<header class="flex h-[49px] shrink-0 items-center gap-2 border-b border-[var(--app-border)] px-4 sm:px-6">
		<SidebarToggle />
		<BarChart3 size={16} class="text-[var(--color-text-secondary)]" />
		<h1 class="text-sm font-medium text-[var(--color-text-primary)]">Insights</h1>
	</header>

	<Tabs.Root value={activeTab} onValueChange={(value) => { if (value === 'overview' || value === 'explore') activeTab = value; }}>
		<div class="shrink-0 border-b border-[var(--app-border)] bg-[var(--color-bg-secondary)]/35">
			<div class="mx-auto flex w-full max-w-[1440px] flex-col gap-3 px-4 py-3 sm:flex-row sm:items-center sm:justify-between sm:px-6">
				<div class="flex min-w-0 items-center gap-3">
					<Select.Root type="single" value={selectedTeamId} disabled={teamsLoading} onValueChange={(value) => value && (selectedTeamId = value)}>
						<Select.Trigger size="sm" aria-label="Team scope" class="w-[220px] max-w-full bg-[var(--color-bg)]">
							{#if selectedTeam}
								<TeamIcon team={selectedTeam} size={14} />
								<span class="truncate">{selectedTeam.name}</span>
							{:else}
								<Building2 size={14} class="text-[var(--color-text-tertiary)]" />
								<span>All workspace teams</span>
							{/if}
						</Select.Trigger>
						<Select.Content>
							<Select.Item value="workspace">
								<Building2 size={14} class="text-[var(--color-text-tertiary)]" />
								All workspace teams
							</Select.Item>
							{#each teams as team}
								<Select.Item value={team.id}>
									<TeamIcon {team} size={14} />
									{team.name}
								</Select.Item>
							{/each}
						</Select.Content>
					</Select.Root>
					<div class="hidden min-w-0 md:block">
						<p class="truncate text-xs font-medium text-[var(--color-text-primary)]">
							{selectedTeam?.name ?? 'Workspace overview'}
						</p>
						<p class="truncate text-[11px] text-[var(--color-text-tertiary)]">
							{teamScoped ? "Using this team's workflow and members" : 'Statuses are grouped by workflow type across teams'}
						</p>
					</div>
				</div>

				<Tabs.List class="h-8 w-fit rounded-lg border border-[var(--app-border)] bg-[var(--color-bg)] p-0.5">
					<Tabs.Trigger value="overview" class="h-6 gap-1.5 rounded-md px-2.5 text-xs">
						<LayoutDashboard size={13} />
						Overview
					</Tabs.Trigger>
					<Tabs.Trigger value="explore" class="h-6 gap-1.5 rounded-md px-2.5 text-xs">
						<SlidersHorizontal size={13} />
						Explore
					</Tabs.Trigger>
				</Tabs.List>
			</div>
		</div>

		<main class="flex-1 overflow-y-auto">
			<div class="mx-auto w-full max-w-[1440px] space-y-8 px-4 py-6 sm:px-6">
				{#if activeTab === 'overview'}
					<section>
						<div class="mb-3 flex items-center gap-2">
							<Activity size={14} class="text-[var(--app-accent-light)]" />
							<div>
								<h2 class="text-xs font-medium text-[var(--color-text-primary)]">Current state</h2>
								<p class="text-[11px] text-[var(--color-text-tertiary)]">Live issue and delivery health for this scope</p>
							</div>
						</div>
						{#if overviewLoading}
							<LoadingState />
						{:else if overviewError}
							<ErrorState message={overviewError} onretry={loadOverview} />
						{:else}
							<OverviewCards {overview} {teamScoped} />
						{/if}
					</section>

					{#if distribution}
						<section>
							<div class="mb-3 flex items-center gap-2">
								<ChartNoAxesCombined size={14} class="text-[var(--app-accent-light)]" />
								<div>
									<h2 class="text-xs font-medium text-[var(--color-text-primary)]">Work distribution</h2>
									<p class="text-[11px] text-[var(--color-text-tertiary)]">
										{teamScoped ? `Using ${selectedTeam?.name ?? 'team'} custom statuses` : 'Workflow types are comparable across team-specific statuses'}
									</p>
								</div>
							</div>
							<DistributionChart {distribution} {teamScoped} />
						</section>
					{/if}

					<section>
						<div class="mb-3 flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
							<div class="flex items-center gap-2">
								<TrendingUp size={14} class="text-[var(--app-accent-light)]" />
								<div>
									<h2 class="text-xs font-medium text-[var(--color-text-primary)]">Delivery trend</h2>
									<p class="text-[11px] text-[var(--color-text-tertiary)]">Created work, completed work, and remaining scope</p>
								</div>
							</div>
							<div class="flex flex-wrap items-center gap-2">
								<AnalyticsDateRangePicker
									startDate={burnupFrom}
									endDate={burnupTo}
									onchange={(start, end) => { burnupFrom = start; burnupTo = end; }}
								/>
								<Select.Root type="single" value={burnupInterval} onValueChange={(value) => value && (burnupInterval = value as BurnupInterval)}>
									<Select.Trigger size="sm" aria-label="Burn-up interval" class="w-[112px] bg-[var(--color-bg-secondary)]">
										<TrendingUp size={13} />
										{burnupInterval === 'day' ? 'Daily' : burnupInterval === 'week' ? 'Weekly' : 'Monthly'}
									</Select.Trigger>
									<Select.Content>
										<Select.Item value="day">Daily</Select.Item>
										<Select.Item value="week">Weekly</Select.Item>
										<Select.Item value="month">Monthly</Select.Item>
									</Select.Content>
								</Select.Root>
							</div>
						</div>
						{#if burnupLoading}
							<LoadingState />
						{:else if burnupError}
							<ErrorState message={burnupError} onretry={loadBurnup} />
						{:else}
							<BurnupChart {burnup} />
						{/if}
					</section>
				{:else}
					<section>
						<div class="mb-4 flex items-center gap-2">
							<UsersRound size={14} class="text-[var(--app-accent-light)]" />
							<div>
								<h2 class="text-xs font-medium text-[var(--color-text-primary)]">Build an insight</h2>
								<p class="text-[11px] text-[var(--color-text-tertiary)]">Choose a measure, grouping, and optional comparison</p>
							</div>
						</div>
						<InsightsExplorer {slug} filters={analyticsScope} {statuses} />
					</section>
				{/if}
			</div>
		</main>
	</Tabs.Root>
</div>
