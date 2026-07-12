<script lang="ts">
	import { onMount } from 'svelte';
	import * as echarts from 'echarts';
	import type { AnalyticsBurnup } from '$lib/api/analytics';
	import { getAnalyticsChartTheme, observeAnalyticsTheme } from './chart-theme';

	let { burnup }: { burnup: AnalyticsBurnup | null } = $props();

	// svelte-ignore non_reactive_update
	let container: HTMLDivElement;
	let chart: echarts.ECharts | null = null;

	function buildOption(): echarts.EChartsOption | null {
		if (!burnup?.points || burnup.points.length === 0) return null;

		const theme = getAnalyticsChartTheme();
		const dates = burnup.points.map((p) => {
			const date = new Date(`${p.date}T00:00:00`);
			return date.toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
		});
		const totalCreated = burnup.points.map((p) => p.total_created ?? 0);
		const totalCompleted = burnup.points.map((p) => p.total_completed ?? 0);
		const scope = burnup.points.map((p) => p.scope ?? 0);

		return {
			backgroundColor: 'transparent',
			animationDuration: 250,
			tooltip: {
				trigger: 'axis' as const,
				backgroundColor: theme.background,
				borderColor: theme.border,
				borderWidth: 1,
				borderRadius: 8,
				padding: [6, 10],
				axisPointer: { lineStyle: { color: theme.textTertiary, type: 'dotted', opacity: 0.45 } },
				textStyle: { color: theme.textPrimary, fontSize: 11 }
			},
			legend: {
				data: ['Total created', 'Total completed', 'Scope'],
				left: 12,
				top: 8,
				icon: 'circle',
				itemWidth: 8,
				itemHeight: 8,
				itemGap: 18,
				textStyle: { fontSize: 10, color: theme.textSecondary }
			},
			grid: { left: 12, right: 16, top: 44, bottom: 12, containLabel: true },
			xAxis: {
				type: 'category' as const,
				data: dates,
				boundaryGap: false,
				axisLabel: {
					fontSize: 10,
					color: theme.textTertiary,
					rotate: dates.length > 12 ? 30 : 0,
					hideOverlap: true
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
					name: 'Total created',
					type: 'line',
					data: totalCreated,
					smooth: true,
					showSymbol: true,
					symbol: 'circle',
					symbolSize: 5,
					lineStyle: { color: theme.accentLight, width: 2 },
					itemStyle: { color: theme.accentLight },
					areaStyle: {
						color: new echarts.graphic.LinearGradient(0, 0, 0, 1, [
							{ offset: 0, color: echarts.color.modifyAlpha(theme.accentLight, 0.14) },
							{ offset: 1, color: echarts.color.modifyAlpha(theme.accentLight, 0.01) }
						])
					}
				},
				{
					name: 'Total completed',
					type: 'line',
					data: totalCompleted,
					smooth: true,
					showSymbol: true,
					symbol: 'circle',
					symbolSize: 5,
					lineStyle: { color: theme.success, width: 2 },
					itemStyle: { color: theme.success },
					areaStyle: {
						color: new echarts.graphic.LinearGradient(0, 0, 0, 1, [
							{ offset: 0, color: echarts.color.modifyAlpha(theme.success, 0.1) },
							{ offset: 1, color: echarts.color.modifyAlpha(theme.success, 0.01) }
						])
					}
				},
				{
					name: 'Scope',
					type: 'line',
					data: scope,
					smooth: true,
					showSymbol: true,
					symbol: 'circle',
					symbolSize: 5,
					lineStyle: { color: theme.warning, width: 1.5, type: 'dashed' },
					itemStyle: { color: theme.warning }
				}
			]
		};
	}

	function renderChart() {
		if (!container || !chart) return;
		const option = buildOption();
		if (option) {
			chart.setOption(option, true);
		} else {
			chart.clear();
		}
	}

	$effect(() => {
		burnup;
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

<div class="relative rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
	<div class="border-b border-[var(--app-border)] px-3 py-2">
		<span class="text-xs font-medium text-[var(--color-text-secondary)]">Burn-up</span>
	</div>
	<div bind:this={container} class="h-72 w-full {burnup?.points?.length ? '' : 'invisible'}"></div>
	{#if !burnup?.points?.length}
		<div class="absolute inset-x-0 bottom-0 flex h-72 items-center justify-center">
			<p class="text-sm text-[var(--color-text-tertiary)]">No burn-up data available</p>
		</div>
	{/if}
</div>
