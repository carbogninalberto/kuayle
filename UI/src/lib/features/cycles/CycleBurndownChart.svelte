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

		const colorFinish = getColor('--color-text-tertiary') || '#8c8c8c';
		const colorStarted = '#f59e0b';
		const colorCompleted = getColor('--app-accent') || '#6650eb';
		const colorProjection = getColor('--app-accent') || '#6650eb';
		const colorBorder = getColor('--app-border') || '#333333';
		const colorText = getColor('--color-text-tertiary') || '#8c8c8c';
		const colorBg = getColor('--color-bg') || '#1e1e1e';

		const dates = data.map((d) => formatDate(d.date));
		const completedData: (number | null)[] = data.map((d) => d.completed);
		const startedData: (number | null)[] = data.map((d) => d.started);

		// The last actual data index before we extend the x-axis with future dates
		const lastActualIndex = data.length - 1;

		// Extend x-axis to cycle end_date so the chart shows the full cycle
		if (cycle.end_date && data.length > 0) {
			const lastDataDate = new Date(data[data.length - 1].date + 'T00:00:00');
			const cycleEnd = new Date(cycle.end_date.slice(0, 10) + 'T00:00:00');
			const d = new Date(lastDataDate);
			d.setDate(d.getDate() + 1);
			while (d <= cycleEnd) {
				dates.push(formatDate(d.toISOString().slice(0, 10)));
				completedData.push(null);
				startedData.push(null);
				d.setDate(d.getDate() + 1);
			}
		}

		// Finish line: horizontal line at the current scope across the full cycle
		const lastScope = data[lastActualIndex].scope;
		const finishData = dates.map(() => lastScope);

		// TODO: Replace linear projection with AI-based predictive model that
		// uses historical velocity patterns, scope change trends, and team
		// capacity data to generate a more accurate completion forecast.

		// Projection: dashed line from day 0 through current rate, extended to end
		// using average daily completion rate (linear extrapolation)
		const projectionData: (number | null)[] = dates.map(() => null);
		const lastCompleted = data[lastActualIndex].completed;
		const daysElapsed = data.length;

		// Index where projection meets the finish line (projected completion day)
		let projectionMetIndex = -1;

		if (daysElapsed > 1) {
			const dailyRate = lastCompleted / (daysElapsed - 1);
			for (let i = 0; i < dates.length; i++) {
				const val = dailyRate * i;
				projectionData[i] = parseFloat(Math.min(val, lastScope).toFixed(1));
				if (projectionMetIndex === -1 && val >= lastScope) {
					projectionMetIndex = i;
				}
			}
		}

		if (chart) {
			chart.dispose();
		}

		// Build buffer zone data: fills from finish line down to 0 only after projection meets scope
		const bufferData: (number | null)[] = dates.map((_, i) =>
			projectionMetIndex >= 0 && i >= projectionMetIndex ? lastScope : null
		);

		chart = echarts.init(chartEl, undefined, { renderer: 'canvas' });

		// Create canvas pattern for single-direction oblique stripes
		const patternSize = 10;
		const patternCanvas = document.createElement('canvas');
		patternCanvas.width = patternSize;
		patternCanvas.height = patternSize;
		const pctx = patternCanvas.getContext('2d')!;
		pctx.clearRect(0, 0, patternSize, patternSize);
		pctx.strokeStyle = colorFinish;
		pctx.lineWidth = 2;
		pctx.globalAlpha = 0.4;
		// Single diagonal line (top-right to bottom-left)
		pctx.beginPath();
		pctx.moveTo(0, patternSize);
		pctx.lineTo(patternSize, 0);
		pctx.stroke();
		// Wrap edges for seamless tiling
		pctx.beginPath();
		pctx.moveTo(-patternSize, patternSize);
		pctx.lineTo(0, 0);
		pctx.stroke();
		pctx.beginPath();
		pctx.moveTo(patternSize, patternSize * 2);
		pctx.lineTo(patternSize * 2, patternSize);
		pctx.stroke();

		chart.setOption({
			backgroundColor: 'transparent',
			grid: {
				left: 12,
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
					interval: (index: number) => index === 0 || index === dates.length - 1
				},
				boundaryGap: false
			},
			yAxis: {
				type: 'value',
				splitLine: { show: false },
				axisLine: { show: false },
				axisTick: { show: false },
				axisLabel: { show: false }
			},
			tooltip: {
				trigger: 'axis',
				position: (point: number[]) => [point[0] - 30, 'bottom'],
				backgroundColor: colorBg,
				borderColor: colorBorder,
				borderWidth: 1,
				borderRadius: 8,
				padding: [4, 10],
				axisPointer: {
					lineStyle: { color: colorText, type: 'dotted', opacity: 0.3 }
				},
				textStyle: { color: colorText, fontSize: 11 },
				formatter: (params: any) => {
					const date = params[0]?.axisValue ?? '';
					const todayLabel = formatDate(new Date().toISOString().slice(0, 10));
					const label = date === todayLabel ? 'Today' : date;
					const started = params.find((p: any) => p.seriesName === 'Started');
					const completed = params.find((p: any) => p.seriesName === 'Completed');
					const sv = started?.value ?? started?.value?.value ?? '';
					const cv = completed?.value ?? completed?.value?.value ?? '';
					if (sv === '' && cv === '') return `<span style="font-weight:500">${label}</span>`;
					return `<div style="display:flex;align-items:center;gap:8px"><span style="font-weight:500">${label}</span>`
						+ `<span style="display:flex;align-items:center;gap:3px"><svg width="10" height="10"><circle cx="5" cy="5" r="4" fill="${colorStarted}"/></svg>${sv}</span>`
						+ `<span style="display:flex;align-items:center;gap:3px"><svg width="10" height="10"><circle cx="5" cy="5" r="4" fill="${colorCompleted}"/></svg>${cv}</span></div>`;
				}
			},
			series: [
				{
					name: 'Finish line',
					type: 'line',
					data: finishData,
					smooth: false,
					symbol: 'none',
					lineStyle: { color: colorFinish, width: 1.5, opacity: 0.35 },
					itemStyle: { color: colorFinish },
					areaStyle: {
						color: new echarts.graphic.LinearGradient(0, 0, 0, 1, [
							{ offset: 0, color: colorFinish + '0a' },
							{ offset: 0.4, color: colorFinish + '00' }
						])
					},
					markLine: {
						silent: true,
						symbol: 'none',
						label: { show: false },
						data: [{ xAxis: lastActualIndex }],
						lineStyle: { color: colorText, width: 1, type: 'dotted', opacity: 0.2 }
					}
				},
				{
					name: 'Buffer',
					type: 'line',
					data: bufferData,
					smooth: false,
					symbol: 'none',
					lineStyle: { width: 0 },
					areaStyle: {
						color: {
							image: patternCanvas,
							repeat: 'repeat'
						} as any
					},
					silent: true
				},
				{
					name: 'Started',
					type: 'line',
					data: startedData.map((v, i) =>
						i === lastActualIndex
							? { value: v, symbol: 'circle', symbolSize: 8 }
							: v
					),
					smooth: true,
					symbol: 'none',
					lineStyle: { color: colorStarted, width: 2 },
					itemStyle: { color: colorStarted }
				},
				{
					name: 'Completed',
					type: 'line',
					data: completedData.map((v, i) =>
						i === lastActualIndex
							? { value: v, symbol: 'circle', symbolSize: 8 }
							: v
					),
					smooth: true,
					symbol: 'none',
					lineStyle: { color: colorCompleted, width: 2 },
					itemStyle: { color: colorCompleted }
				},
				{
					name: 'Projection',
					type: 'line',
					data: projectionData,
					smooth: false,
					symbol: 'none',
					lineStyle: { color: colorProjection, width: 1, type: 'dashed' },
					itemStyle: { color: colorProjection }
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
					<span class="inline-block h-2 w-2 rounded-sm bg-[var(--color-text-tertiary)]" style="border: 1px dotted var(--color-text-tertiary);"></span>
					<span class="text-[var(--color-text-secondary)]">Finish line</span>
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
			<div class="flex items-center justify-between gap-2">
				<span class="flex items-center gap-1.5">
					<span class="inline-block h-2 w-2 rounded-sm bg-[var(--app-accent)]"></span>
					<span class="text-[var(--color-text-secondary)]">Projection</span>
				</span>
				<span class="text-[var(--color-text-tertiary)]">avg/day</span>
			</div>
		</div>
	{/if}
</div>
