<script lang="ts">
	import type { Issue } from '$lib/types/issue';
	import type { Cycle } from '$lib/types/cycle';
	import IssueStatusIcon from '$lib/features/issues/IssueStatusIcon.svelte';
	import IssuePriorityIcon from '$lib/features/issues/IssuePriorityIcon.svelte';

	let {
		issues,
		cycles = [],
		startDate,
		endDate,
		onissueclick
	}: {
		issues: Issue[];
		cycles?: Cycle[];
		startDate: Date;
		endDate: Date;
		onissueclick?: (issue: Issue) => void;
	} = $props();

	const MS_PER_DAY = 86400000;
	const totalDays = $derived(Math.max(1, Math.ceil((endDate.getTime() - startDate.getTime()) / MS_PER_DAY)));

	const months = $derived.by(() => {
		const result: { label: string; startPct: number; widthPct: number }[] = [];
		const cur = new Date(startDate);
		while (cur < endDate) {
			const monthStart = new Date(cur.getFullYear(), cur.getMonth(), 1);
			const monthEnd = new Date(cur.getFullYear(), cur.getMonth() + 1, 0);
			const visibleStart = Math.max(monthStart.getTime(), startDate.getTime());
			const visibleEnd = Math.min(monthEnd.getTime(), endDate.getTime());
			const startPct = ((visibleStart - startDate.getTime()) / MS_PER_DAY / totalDays) * 100;
			const widthPct = ((visibleEnd - visibleStart) / MS_PER_DAY / totalDays) * 100;
			result.push({
				label: cur.toLocaleDateString('en-US', { month: 'short', year: 'numeric' }),
				startPct,
				widthPct
			});
			cur.setMonth(cur.getMonth() + 1);
			cur.setDate(1);
		}
		return result;
	});

	const todayPct = $derived.by(() => {
		const now = new Date();
		if (now < startDate || now > endDate) return null;
		return ((now.getTime() - startDate.getTime()) / MS_PER_DAY / totalDays) * 100;
	});

	function getBarPosition(dueDateStr: string | null, createdAt: string): { left: number; width: number } | null {
		if (!dueDateStr) return null;
		const created = new Date(createdAt);
		const due = new Date(dueDateStr);
		if (due < startDate || created > endDate) return null;
		const barStart = Math.max(created.getTime(), startDate.getTime());
		const barEnd = Math.min(due.getTime(), endDate.getTime());
		const left = ((barStart - startDate.getTime()) / MS_PER_DAY / totalDays) * 100;
		const width = Math.max(1, ((barEnd - barStart) / MS_PER_DAY / totalDays) * 100);
		return { left, width };
	}

	function getCyclePosition(cycle: Cycle): { left: number; width: number } | null {
		if (!cycle.start_date || !cycle.end_date) return null;
		const cStart = new Date(cycle.start_date);
		const cEnd = new Date(cycle.end_date);
		if (cEnd < startDate || cStart > endDate) return null;
		const barStart = Math.max(cStart.getTime(), startDate.getTime());
		const barEnd = Math.min(cEnd.getTime(), endDate.getTime());
		const left = ((barStart - startDate.getTime()) / MS_PER_DAY / totalDays) * 100;
		const width = Math.max(1, ((barEnd - barStart) / MS_PER_DAY / totalDays) * 100);
		return { left, width };
	}

	function statusColor(status: string): string {
		switch (status) {
			case 'done': return 'var(--color-success)';
			case 'in_progress': case 'in_review': return 'var(--app-accent)';
			case 'cancelled': return 'var(--color-text-tertiary)';
			default: return 'var(--color-text-secondary)';
		}
	}
</script>

<div class="flex flex-col overflow-hidden rounded-lg border border-[var(--app-border)]">
	<!-- Header with months -->
	<div class="relative flex h-8 shrink-0 border-b border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
		{#each months as month}
			<div
				class="absolute top-0 flex h-full items-center border-l border-[var(--app-border)] px-2 text-[10px] font-medium text-[var(--color-text-tertiary)]"
				style="left: {month.startPct}%; width: {month.widthPct}%"
			>
				{month.label}
			</div>
		{/each}
	</div>

	<!-- Cycle bands -->
	{#if cycles.length > 0}
		<div class="relative h-6 shrink-0 border-b border-[var(--app-border)] bg-[var(--color-bg)]">
			{#each cycles as cycle}
				{@const pos = getCyclePosition(cycle)}
				{#if pos}
					<div
						class="absolute top-1 h-4 rounded-sm opacity-20"
						style="left: {pos.left}%; width: {pos.width}%; background-color: var(--app-accent)"
						title="{cycle.name}: {cycle.start_date} → {cycle.end_date}"
					>
						<span class="absolute inset-0 flex items-center justify-center text-[9px] font-medium text-[var(--app-accent)] opacity-100">
							{cycle.name}
						</span>
					</div>
				{/if}
			{/each}
		</div>
	{/if}

	<!-- Issue rows -->
	<div class="flex-1 overflow-y-auto">
		{#each issues as issue (issue.id)}
			{@const bar = getBarPosition(issue.due_date, issue.created_at)}
			<div class="group relative flex h-9 items-center border-b border-[var(--app-border)] hover:bg-[var(--color-bg-hover)]">
				<!-- Issue label (left side) -->
				<div class="flex w-64 shrink-0 items-center gap-2 border-r border-[var(--app-border)] px-3">
					<IssuePriorityIcon priority={issue.priority} size={12} />
					<IssueStatusIcon status={issue.status} size={12} />
					<button
						class="truncate text-xs text-[var(--color-text-primary)] hover:underline"
						onclick={() => onissueclick?.(issue)}
					>
						{issue.title}
					</button>
				</div>

				<!-- Bar area -->
				<div class="relative flex-1">
					{#if bar}
						<div
							class="absolute top-1.5 h-4 rounded-sm transition-colors"
							style="left: {bar.left}%; width: {bar.width}%; background-color: {statusColor(issue.status)}; opacity: 0.7"
							title="{issue.identifier}: {issue.title}"
						></div>
					{:else}
						<!-- No due date: show a dot at creation -->
						{@const created = new Date(issue.created_at)}
						{#if created >= startDate && created <= endDate}
							{@const pct = ((created.getTime() - startDate.getTime()) / MS_PER_DAY / totalDays) * 100}
							<div
								class="absolute top-2.5 h-2 w-2 rounded-full"
								style="left: {pct}%; background-color: {statusColor(issue.status)}; opacity: 0.5"
							></div>
						{/if}
					{/if}
				</div>

				<!-- Today line -->
				{#if todayPct !== null}
					<div
						class="pointer-events-none absolute top-0 h-full w-px bg-red-500 opacity-40"
						style="left: calc({64 / 1}px + {todayPct}% * (1 - {64 / 1}px / 100%))"
					></div>
				{/if}
			</div>
		{/each}

		{#if issues.length === 0}
			<div class="flex h-24 items-center justify-center text-sm text-[var(--color-text-tertiary)]">
				No issues to display
			</div>
		{/if}
	</div>

	<!-- Today line in header -->
	{#if todayPct !== null}
		<div
			class="pointer-events-none absolute top-0 h-full w-px bg-red-500 opacity-60 z-10"
			style="left: calc(256px + (100% - 256px) * {todayPct} / 100)"
		></div>
	{/if}
</div>
