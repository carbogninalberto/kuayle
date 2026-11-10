<script lang="ts">
	import { m, getLocale, setLocale } from '$lib/features/i18n/messages';
	import type { AnalyticsOverview } from '$lib/api/analytics';

	let { overview, teamScoped = false }: { overview: AnalyticsOverview | null; teamScoped?: boolean } = $props();

	function fmt(num: number | undefined | null): string {
		if (num == null) return '-';
		return num.toLocaleString();
	}

	function pct(num: number | undefined | null): string {
		if (num == null) return '-';
		return `${Math.round(num)}%`;
	}

	function hours(num: number | undefined | null): string {
		if (num == null) return '-';
		if (num < 1) return '<1h';
		const totalHours = Math.round(num);
		const days = Math.floor(totalHours / 24);
		const hrs = totalHours % 24;
		if (days > 0) return hrs > 0 ? `${days}d ${hrs}h` : `${days}d`;
		return `${totalHours}h`;
	}

	const cards: { label: string; value: string; sub?: string }[] = $derived([
		{ label: m['insights.total_issues'](), value: fmt(overview?.total_issues) },
		{ label: m['insights.open_issues'](), value: fmt(overview?.open_issues) },
		{ label: m['insights.completed_issues'](), value: fmt(overview?.completed_issues) },
		{ label: m['insights.overdue_issues'](), value: fmt(overview?.overdue_issues) },
		{ label: m['insights.started_issues'](), value: fmt(overview?.started_issues) },
		{ label: m['insights.unassigned_issues'](), value: fmt(overview?.unassigned_issues) },
		{ label: m['insights.completion_rate'](), value: pct(overview?.completion_rate) },
		{ label: m['insights.avg_lead_time'](), value: hours(overview?.avg_lead_time_hours) },
		{ label: m['insights.avg_cycle_time'](), value: hours(overview?.avg_cycle_time_hours) },
		{ label: m['insights.total_projects'](), value: fmt(overview?.total_projects) },
		{ label: teamScoped ? m['insights.team_members']() : m['insights.members'](), value: fmt(overview?.total_members) }
	]);
</script>

<div class="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-6">
	{#each cards as card}
		<div class="rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-3">
			<p class="text-xs text-[var(--color-text-tertiary)]">{card.label}</p>
			<p class="mt-1 text-lg font-semibold text-[var(--color-text-primary)]">{card.value}</p>
			{#if card.sub}
				<p class="text-xs text-[var(--color-text-tertiary)]">{card.sub}</p>
			{/if}
		</div>
	{/each}
</div>
