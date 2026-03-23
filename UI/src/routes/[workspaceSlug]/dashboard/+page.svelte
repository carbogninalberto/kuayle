<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { getOverview, getIssueDistribution, type AnalyticsOverview, type IssueDistribution } from '$lib/api/analytics';
	import { listIssues } from '$lib/api/issues';
	import { STATUS_LABELS, PRIORITY_LABELS, type IssueStatus, type IssuePriority, type Issue } from '$lib/types/issue';
	import IssueStatusIcon from '$lib/features/issues/IssueStatusIcon.svelte';
	import SidebarToggle from '$lib/components/layout/SidebarToggle.svelte';
	import IssuePriorityIcon from '$lib/features/issues/IssuePriorityIcon.svelte';
	import * as Card from '$lib/components/ui/card';
	import { Progress } from '$lib/components/ui/progress';
	import {
		LayoutGrid,
		CheckCircle2,
		Circle,
		AlertTriangle,
		FolderKanban,
		Users,
		Clock,
		CalendarDays
	} from 'lucide-svelte';

	const slug = $derived(page.params.workspaceSlug ?? '');
	let overview = $state<AnalyticsOverview | null>(null);
	let distribution = $state<IssueDistribution | null>(null);
	let recentIssues = $state<Issue[]>([]);
	let upcomingIssues = $state<Issue[]>([]);
	let loading = $state(true);

	onMount(async () => {
		try {
			const [o, d, recent, upcoming] = await Promise.all([
				getOverview(slug),
				getIssueDistribution(slug),
				listIssues(slug, { sort: 'updated_at', order: 'desc', per_page: '8' }),
				listIssues(slug, { due_before: new Date(Date.now() + 7 * 86400000).toISOString().split('T')[0], status: 'backlog,todo,in_progress,in_review', sort: 'sort_order', order: 'asc', per_page: '8' })
			]);
			overview = o;
			distribution = d;
			recentIssues = recent.data;
			upcomingIssues = upcoming.data;
		} finally {
			loading = false;
		}
	});

	const STATUS_COLORS: Record<string, string> = {
		backlog: 'var(--color-text-tertiary)',
		todo: 'var(--color-text-secondary)',
		in_progress: 'var(--app-accent)',
		in_review: 'var(--color-warning)',
		done: 'var(--color-success)',
		cancelled: 'var(--color-error)'
	};

	const PRIORITY_COLORS: Record<number, string> = {
		0: 'var(--color-text-tertiary)',
		1: 'var(--color-urgent)',
		2: 'var(--color-high)',
		3: 'var(--color-medium)',
		4: 'var(--color-low)'
	};

	let completionRate = $derived(
		overview && overview.total_issues > 0
			? Math.round((overview.completed_issues / overview.total_issues) * 100)
			: 0
	);

	function formatShortDate(date: string | null): string {
		if (!date) return '';
		return new Date(date).toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
	}
</script>

<div class="flex h-full flex-col">
	<div class="flex h-[49px] items-center gap-2 border-b border-[var(--app-border)] px-6">
		<SidebarToggle />
		<h1 class="text-sm font-medium text-[var(--color-text-primary)]">Dashboard</h1>
	</div>

	{#if !loading && overview}
		<div class="flex-1 overflow-y-auto p-6 space-y-6">
			<!-- Summary cards -->
			<div class="grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-6">
				<Card.Root class="border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
					<Card.Content class="p-4">
						<div class="flex items-center gap-2 text-[var(--color-text-tertiary)]">
							<LayoutGrid size={14} />
							<span class="text-xs">Total</span>
						</div>
						<p class="mt-1 text-2xl font-bold tabular-nums text-[var(--color-text-primary)]">{overview.total_issues}</p>
					</Card.Content>
				</Card.Root>

				<Card.Root class="border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
					<Card.Content class="p-4">
						<div class="flex items-center gap-2 text-[var(--color-text-tertiary)]">
							<Circle size={14} />
							<span class="text-xs">Open</span>
						</div>
						<p class="mt-1 text-2xl font-bold tabular-nums text-[var(--color-text-primary)]">{overview.open_issues}</p>
					</Card.Content>
				</Card.Root>

				<Card.Root class="border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
					<Card.Content class="p-4">
						<div class="flex items-center gap-2 text-[var(--color-success)]">
							<CheckCircle2 size={14} />
							<span class="text-xs">Completed</span>
						</div>
						<p class="mt-1 text-2xl font-bold tabular-nums text-[var(--color-text-primary)]">{overview.completed_issues}</p>
					</Card.Content>
				</Card.Root>

				<Card.Root class="border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
					<Card.Content class="p-4">
						<div class="flex items-center gap-2 text-[var(--color-error)]">
							<AlertTriangle size={14} />
							<span class="text-xs">Overdue</span>
						</div>
						<p class="mt-1 text-2xl font-bold tabular-nums text-[var(--color-text-primary)]">{overview.overdue_issues}</p>
					</Card.Content>
				</Card.Root>

				<Card.Root class="border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
					<Card.Content class="p-4">
						<div class="flex items-center gap-2 text-[var(--color-text-tertiary)]">
							<FolderKanban size={14} />
							<span class="text-xs">Projects</span>
						</div>
						<p class="mt-1 text-2xl font-bold tabular-nums text-[var(--color-text-primary)]">{overview.total_projects}</p>
					</Card.Content>
				</Card.Root>

				<Card.Root class="border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
					<Card.Content class="p-4">
						<div class="flex items-center gap-2 text-[var(--color-text-tertiary)]">
							<Users size={14} />
							<span class="text-xs">Members</span>
						</div>
						<p class="mt-1 text-2xl font-bold tabular-nums text-[var(--color-text-primary)]">{overview.total_members}</p>
					</Card.Content>
				</Card.Root>
			</div>

			<!-- Completion rate -->
			<Card.Root class="border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
				<Card.Content class="p-5">
					<div class="flex items-center justify-between mb-3">
						<span class="text-sm font-medium text-[var(--color-text-primary)]">Completion Rate</span>
						<span class="text-sm tabular-nums text-[var(--color-text-tertiary)]">{completionRate}%</span>
					</div>
					<Progress value={completionRate} class="h-2" />
				</Card.Content>
			</Card.Root>

			<div class="grid grid-cols-1 gap-6 lg:grid-cols-2">
				<!-- Recently updated issues -->
				<Card.Root class="border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
					<Card.Content class="p-5">
						<div class="flex items-center gap-2 mb-4">
							<Clock size={14} class="text-[var(--color-text-tertiary)]" />
							<h3 class="text-sm font-medium text-[var(--color-text-primary)]">Recently Updated</h3>
						</div>
						<div class="space-y-1">
							{#each recentIssues as issue}
								<a
									href="/{slug}/issue/{issue.identifier}"
									class="flex items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-[var(--color-bg-hover)]"
								>
									<IssuePriorityIcon priority={issue.priority} size={14} />
									<IssueStatusIcon status={issue.status} size={14} />
									<span class="shrink-0 text-xs text-[var(--color-text-tertiary)]">{issue.identifier}</span>
									<span class="flex-1 truncate text-[var(--color-text-primary)]">{issue.title}</span>
									{#if issue.assignee}
										<div class="flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-[var(--app-accent)] text-[9px] text-[var(--app-accent-foreground)]">
											{(issue.assignee.name ?? 'U').charAt(0).toUpperCase()}
										</div>
									{/if}
								</a>
							{/each}
							{#if recentIssues.length === 0}
								<p class="py-4 text-center text-xs text-[var(--color-text-tertiary)]">No recent issues</p>
							{/if}
						</div>
					</Card.Content>
				</Card.Root>

				<!-- Upcoming due dates -->
				<Card.Root class="border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
					<Card.Content class="p-5">
						<div class="flex items-center gap-2 mb-4">
							<CalendarDays size={14} class="text-[var(--color-text-tertiary)]" />
							<h3 class="text-sm font-medium text-[var(--color-text-primary)]">Due This Week</h3>
						</div>
						<div class="space-y-1">
							{#each upcomingIssues as issue}
								<a
									href="/{slug}/issue/{issue.identifier}"
									class="flex items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-[var(--color-bg-hover)]"
								>
									<IssueStatusIcon status={issue.status} size={14} />
									<span class="shrink-0 text-xs text-[var(--color-text-tertiary)]">{issue.identifier}</span>
									<span class="flex-1 truncate text-[var(--color-text-primary)]">{issue.title}</span>
									{#if issue.due_date}
										<span class="shrink-0 text-xs text-[var(--color-text-tertiary)]">{formatShortDate(issue.due_date)}</span>
									{/if}
								</a>
							{/each}
							{#if upcomingIssues.length === 0}
								<p class="py-4 text-center text-xs text-[var(--color-text-tertiary)]">No upcoming deadlines</p>
							{/if}
						</div>
					</Card.Content>
				</Card.Root>
			</div>

			<div class="grid grid-cols-1 gap-6 lg:grid-cols-2">
				<!-- Status distribution -->
				{#if distribution}
					<Card.Root class="border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
						<Card.Content class="p-5">
							<h3 class="mb-4 text-sm font-medium text-[var(--color-text-primary)]">Issues by Status</h3>
							<div class="space-y-3">
								{#each distribution.by_status as item}
									{@const maxCount = Math.max(...distribution.by_status.map(s => s.count))}
									{@const percentage = maxCount > 0 ? (item.count / maxCount) * 100 : 0}
									<div class="flex items-center gap-3">
										<span class="w-24 shrink-0 text-xs text-[var(--color-text-secondary)]">
											{STATUS_LABELS[item.status as IssueStatus] ?? item.status}
										</span>
										<div class="flex-1 h-5 rounded-sm bg-[var(--color-bg-tertiary)] overflow-hidden">
											<div
												class="h-full rounded-sm transition-all"
												style="width: {percentage}%; background-color: {STATUS_COLORS[item.status] ?? 'var(--color-text-tertiary)'}"
											></div>
										</div>
										<span class="w-8 shrink-0 text-right text-xs tabular-nums text-[var(--color-text-tertiary)]">{item.count}</span>
									</div>
								{/each}
								{#if distribution.by_status.length === 0}
									<p class="text-xs text-[var(--color-text-tertiary)]">No data</p>
								{/if}
							</div>
						</Card.Content>
					</Card.Root>

					<!-- Priority distribution -->
					<Card.Root class="border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
						<Card.Content class="p-5">
							<h3 class="mb-4 text-sm font-medium text-[var(--color-text-primary)]">Issues by Priority</h3>
							<div class="space-y-3">
								{#each distribution.by_priority as item}
									{@const maxCount = Math.max(...distribution.by_priority.map(p => p.count))}
									{@const percentage = maxCount > 0 ? (item.count / maxCount) * 100 : 0}
									<div class="flex items-center gap-3">
										<span class="w-24 shrink-0 text-xs text-[var(--color-text-secondary)]">
											{PRIORITY_LABELS[item.priority as IssuePriority] ?? `P${item.priority}`}
										</span>
										<div class="flex-1 h-5 rounded-sm bg-[var(--color-bg-tertiary)] overflow-hidden">
											<div
												class="h-full rounded-sm transition-all"
												style="width: {percentage}%; background-color: {PRIORITY_COLORS[item.priority] ?? 'var(--color-text-tertiary)'}"
											></div>
										</div>
										<span class="w-8 shrink-0 text-right text-xs tabular-nums text-[var(--color-text-tertiary)]">{item.count}</span>
									</div>
								{/each}
								{#if distribution.by_priority.length === 0}
									<p class="text-xs text-[var(--color-text-tertiary)]">No data</p>
								{/if}
							</div>
						</Card.Content>
					</Card.Root>
				{/if}
			</div>
		</div>
	{/if}
</div>
