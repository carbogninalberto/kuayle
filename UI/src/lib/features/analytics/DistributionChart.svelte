<script lang="ts">
	import { onMount } from 'svelte';
	import * as echarts from 'echarts';
	import type { AnalyticsDistribution } from '$lib/api/analytics';
	import { PRIORITY_LABELS } from '$lib/types/issue';
	import { CATEGORY_LABELS, type StatusCategory } from '$lib/types/team-status';
	import {
		getAnalyticsChartTheme,
		observeAnalyticsTheme,
		priorityChartColor,
		statusChartColor
	} from './chart-theme';

	let {
		distribution,
		teamScoped = false
	}: { distribution: AnalyticsDistribution | null; teamScoped?: boolean } = $props();

	type Tab = 'status' | 'priority';
	let activeTab = $state<Tab>('status');

	// svelte-ignore non_reactive_update
	let container: HTMLDivElement;
	let chart: echarts.ECharts | null = null;

	const statusData = $derived.by(() => {
		const groups = new Map<string, NonNullable<AnalyticsDistribution['by_status']>[number]>();
		for (const status of distribution?.by_status ?? []) {
			if (status.count === 0) continue;
			const key = teamScoped ? `${status.category}:${status.name.trim().toLowerCase()}` : status.category;
			const normalized = teamScoped
				? status
				: {
						...status,
						status_id: status.category,
						name: CATEGORY_LABELS[status.category as StatusCategory] ?? status.category,
						color: null
					};
			const existing = groups.get(key);
			if (existing) {
				existing.count += normalized.count;
				if (teamScoped) existing.color ||= normalized.color;
			} else {
				groups.set(key, { ...normalized });
			}
		}
		return [...groups.values()];
	});
	const priorityData = $derived((distribution?.by_priority ?? []).filter((item) => item.count > 0));

	function tooltipTheme(theme: ReturnType<typeof getAnalyticsChartTheme>) {
		return {
			trigger: 'axis' as const,
			backgroundColor: theme.background,
			borderColor: theme.border,
			borderWidth: 1,
			borderRadius: 8,
			padding: [6, 10],
			axisPointer: { lineStyle: { color: theme.textTertiary, type: 'dotted' as const, opacity: 0.45 } },
			textStyle: { color: theme.textPrimary, fontSize: 11 }
		};
	}

	function buildStatusOption() {
		const theme = getAnalyticsChartTheme();
		const items = statusData;
		const names = items.map((d) => d.name ?? d.status_id ?? '-');
		const counts = items.map((d) => d.count);
		const colors = items.map((d) => statusChartColor(d.category, d.color, theme));

		return {
			backgroundColor: 'transparent',
			animationDuration: 250,
			tooltip: tooltipTheme(theme),
			grid: { left: 12, right: 12, top: 16, bottom: 12, containLabel: true },
			xAxis: {
				type: 'category' as const,
				data: names,
				axisLabel: {
					fontSize: 10,
					color: theme.textTertiary,
					interval: 0,
					rotate: names.length > 7 ? 25 : 0,
					formatter: (value: string) => (value.length > 16 ? `${value.slice(0, 15)}...` : value)
				},
				axisTick: { show: false },
				axisLine: { lineStyle: { color: theme.border } }
			},
			yAxis: {
				type: 'value' as const,
				minInterval: 1,
				axisLine: { show: false },
				axisTick: { show: false },
				axisLabel: { fontSize: 10, color: theme.textTertiary },
				splitLine: { lineStyle: { color: theme.border, opacity: 0.35 } }
			},
			series: [
				{
					type: 'bar',
					data: counts.map((val, i) => ({
						value: val,
						itemStyle: { color: colors[i], borderRadius: [4, 4, 0, 0] }
					})),
					barMaxWidth: 36
				}
			]
		};
	}

	function buildPriorityOption() {
		const theme = getAnalyticsChartTheme();
		const items = priorityData;
		const names = items.map((d) => PRIORITY_LABELS[d.priority as keyof typeof PRIORITY_LABELS] ?? `P${d.priority}`);
		const counts = items.map((d) => d.count);

		return {
			backgroundColor: 'transparent',
			animationDuration: 250,
			tooltip: tooltipTheme(theme),
			grid: { left: 12, right: 12, top: 16, bottom: 12, containLabel: true },
			xAxis: {
				type: 'category' as const,
				data: names,
				axisLabel: { fontSize: 10, color: theme.textTertiary, interval: 0 },
				axisTick: { show: false },
				axisLine: { lineStyle: { color: theme.border } }
			},
			yAxis: {
				type: 'value' as const,
				minInterval: 1,
				axisLine: { show: false },
				axisTick: { show: false },
				axisLabel: { fontSize: 10, color: theme.textTertiary },
				splitLine: { lineStyle: { color: theme.border, opacity: 0.35 } }
			},
			series: [
				{
					type: 'bar',
					data: counts.map((val, i) => ({
						value: val,
						itemStyle: { color: priorityChartColor(items[i].priority, theme), borderRadius: [4, 4, 0, 0] }
					})),
					barMaxWidth: 36
				}
			]
		};
	}

	function renderChart() {
		if (!container || !chart) return;
		const items = activeTab === 'status' ? statusData : priorityData;
		if (items.length === 0) {
			chart.clear();
			return;
		}
		const option = activeTab === 'status' ? buildStatusOption() : buildPriorityOption();
		chart.setOption(option, true);
	}

	$effect(() => {
		distribution;
		activeTab;
		renderChart();
	});

	onMount(() => {
		if (container) {
			chart = echarts.init(container, undefined, { renderer: 'canvas' });
			renderChart();
		}
		const resizeObserver = new ResizeObserver(() => chart?.resize());
		resizeObserver.observe(container);
		const stopThemeObserver = observeAnalyticsTheme(renderChart);
		return () => {
			resizeObserver.disconnect();
			stopThemeObserver();
			chart?.dispose();
			chart = null;
		};
	});
</script>

<div class="rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
	<div class="flex items-center gap-1 border-b border-[var(--app-border)] px-3 py-2">
		<button
			onclick={() => (activeTab = 'status')}
			class="rounded px-2 py-0.5 text-xs {activeTab === 'status'
				? 'bg-[var(--app-accent)]/10 text-[var(--color-text-primary)]'
				: 'text-[var(--color-text-tertiary)] hover:text-[var(--color-text-secondary)]'}"
		>
			{teamScoped ? 'By Status' : 'By Status Type'}
		</button>
		<button
			onclick={() => (activeTab = 'priority')}
			class="rounded px-2 py-0.5 text-xs {activeTab === 'priority'
				? 'bg-[var(--app-accent)]/10 text-[var(--color-text-primary)]'
				: 'text-[var(--color-text-tertiary)] hover:text-[var(--color-text-secondary)]'}"
		>
			By Priority
		</button>
	</div>
	<div bind:this={container} class="h-60 w-full"></div>
</div>
