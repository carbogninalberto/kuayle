<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { getOverview, getIssueDistribution, type AnalyticsOverview, type IssueDistribution } from '$lib/api/analytics';
	import { STATUS_LABELS, PRIORITY_LABELS, type IssueStatus, type IssuePriority } from '$lib/types/issue';
	import * as Card from '$lib/components/ui/card';
	import { Progress } from '$lib/components/ui/progress';
	import LoadingState from '$lib/components/shared/LoadingState.svelte';
	import {
		LayoutGrid,
		CheckCircle2,
		Circle,
		AlertTriangle,
		FolderKanban,
		Users
	} from 'lucide-svelte';

	const slug = $derived(page.params.workspaceSlug ?? '');
	let overview = $state<AnalyticsOverview | null>(null);
	let distribution = $state<IssueDistribution | null>(null);
	let loading = $state(true);

	onMount(async () => {
		try {
			const [o, d] = await Promise.all([
				getOverview(slug),
				getIssueDistribution(slug)
			]);
			overview = o;
			distribution = d;
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
</script>

<div class="flex h-full flex-col">
	<div class="flex h-[49px] items-center border-b border-[var(--app-border)] px-6">
		<h1 class="text-sm font-medium text-[var(--color-text-primary)]">Dashboard</h1>
	</div>

	{#if loading}
		<LoadingState />
	{:else if overview}
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
