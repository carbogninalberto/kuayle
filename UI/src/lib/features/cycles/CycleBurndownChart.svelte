<script lang="ts">
	import type { Cycle, CycleBurndownPoint } from '$lib/types/cycle';
	import * as echarts from 'echarts';

	let {
		cycle,
		data
	}: {
		cycle: Cycle;
		data: CycleBurndownPoint[];
	} = $props();

	let chartEl: HTMLDivElement | undefined = $state();
	let chart: echarts.ECharts | undefined;

	function getColor(varName: string): string {
		return getComputedStyle(document.documentElement).getPropertyValue(varName).trim();
	}

	const lastPoint = $derived(data.length > 0 ? data[data.length - 1] : null);
	const startedPct = $derived(
		lastPoint && lastPoint.scope > 0
			? Math.round((lastPoint.started / lastPoint.scope) * 100)
			: 0
	);
	const completedPct = $derived(
		lastPoint && lastPoint.scope > 0
			? Math.round((lastPoint.completed / lastPoint.scope) * 100)
			: 0
	);

	function formatDate(dateStr: string): string {
		const d = new Date(dateStr + 'T00:00:00');
		return d.toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
	}

	$effect(() => {
		if (!chartEl || data.length === 0) return;

		const colorScope = getColor('--color-text-tertiary') || '#8c8c8c';
		const colorStarted = '#f59e0b';
		const colorCompleted = getColor('--app-accent') || '#6650eb';
		const colorBorder = getColor('--app-border') || '#333333';
		const colorText = getColor('--color-text-tertiary') || '#8c8c8c';
		const colorBg = getColor('--color-bg') || '#1e1e1e';

		const dates = data.map((d) => formatDate(d.date));
		const scopeData = data.map((d) => d.scope);
		const startedData = data.map((d) => d.started);
		const completedData = data.map((d) => d.completed);

		// Ideal line: linear from max scope to 0
		const maxScope = Math.max(...scopeData, 1);
		const idealData = data.map((_, i) =>
			Math.round(maxScope * (1 - i / Math.max(data.length - 1, 1)))
		);

		if (chart) {
			chart.dispose();
		}

		chart = echarts.init(chartEl, undefined, { renderer: 'svg' });

		chart.setOption({
			backgroundColor: 'transparent',
			grid: {
				left: 35,
				right: 16,
				top: 12,
				bottom: 28
			},
			xAxis: {
				type: 'category',
				data: dates,
				axisLine: { lineStyle: { color: colorBorder } },
				axisTick: { show: false },
				axisLabel: {
					color: colorText,
					fontSize: 10,
					interval: Math.max(Math.floor(dates.length / 4) - 1, 0)
				},
				boundaryGap: false
			},
			yAxis: {
				type: 'value',
				splitLine: { lineStyle: { color: colorBorder, opacity: 0.3 } },
				axisLine: { show: false },
				axisTick: { show: false },
				axisLabel: { color: colorText, fontSize: 10 }
			},
			tooltip: {
				trigger: 'axis',
				backgroundColor: colorBg,
				borderColor: colorBorder,
				textStyle: { color: colorText, fontSize: 11 },
				formatter: (params: any) => {
					const date = params[0]?.axisValue ?? '';
					let html = `<div style="font-weight:500;margin-bottom:4px">${date}</div>`;
					for (const p of params) {
						html += `<div style="display:flex;align-items:center;gap:6px">
							<span style="width:8px;height:8px;border-radius:50%;background:${p.color};display:inline-block"></span>
							${p.seriesName}: <strong>${p.value}</strong>
						</div>`;
					}
					return html;
				}
			},
			series: [
				{
					name: 'Scope',
					type: 'line',
					data: scopeData,
					smooth: true,
					symbol: 'none',
					lineStyle: { color: colorScope, width: 1.5 },
					itemStyle: { color: colorScope }
				},
				{
					name: 'Started',
					type: 'line',
					data: startedData,
					smooth: true,
					symbol: 'none',
					lineStyle: { color: colorStarted, width: 2 },
					itemStyle: { color: colorStarted }
				},
				{
					name: 'Completed',
					type: 'line',
					data: completedData,
					smooth: true,
					symbol: 'none',
					lineStyle: { color: colorCompleted, width: 2 },
					itemStyle: { color: colorCompleted }
				},
				{
					name: 'Ideal',
					type: 'line',
					data: idealData,
					smooth: false,
					symbol: 'none',
					lineStyle: { color: colorCompleted, width: 1, type: 'dashed', opacity: 0.4 },
					itemStyle: { color: colorCompleted }
				}
			]
		});

		const ro = new ResizeObserver(() => chart?.resize());
		ro.observe(chartEl);

		return () => {
			ro.disconnect();
			chart?.dispose();
			chart = undefined;
		};
	});
</script>

<div class="flex gap-4">
	<div class="flex-1 min-w-0">
		<div bind:this={chartEl} class="h-[200px] w-full"></div>
	</div>
	{#if lastPoint}
		<div class="flex w-[140px] shrink-0 flex-col justify-center gap-2 text-xs">
			<div class="flex items-center justify-between gap-2">
				<span class="flex items-center gap-1.5">
					<span class="inline-block h-2 w-2 rounded-sm bg-[var(--color-text-tertiary)]"></span>
					<span class="text-[var(--color-text-secondary)]">Scope</span>
				</span>
				<span class="font-medium text-[var(--app-accent)]">{lastPoint.scope}</span>
			</div>
			<div class="flex items-center justify-between gap-2">
				<span class="flex items-center gap-1.5">
					<span class="inline-block h-2 w-2 rounded-sm bg-amber-500"></span>
					<span class="text-[var(--color-text-secondary)]">Started</span>
				</span>
				<span class="text-[var(--color-text-primary)]">{lastPoint.started} <span class="text-[var(--color-text-tertiary)]">· {startedPct}%</span></span>
			</div>
			<div class="flex items-center justify-between gap-2">
				<span class="flex items-center gap-1.5">
					<span class="inline-block h-2 w-2 rounded-sm bg-[var(--app-accent)]"></span>
					<span class="text-[var(--color-text-secondary)]">Completed</span>
				</span>
				<span class="text-[var(--color-text-primary)]">{lastPoint.completed} <span class="text-[var(--color-text-tertiary)]">· {completedPct}%</span></span>
			</div>
		</div>
	{/if}
</div>
