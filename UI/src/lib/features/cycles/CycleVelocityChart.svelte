<script lang="ts">
	import type { VelocityPoint } from '$lib/types/cycle';
	import * as echarts from 'echarts';

	let {
		data
	}: {
		data: VelocityPoint[];
	} = $props();

	let chartEl: HTMLDivElement | undefined = $state();
	let chart: echarts.ECharts | undefined;

	function getColor(varName: string): string {
		return getComputedStyle(document.documentElement).getPropertyValue(varName).trim();
	}

	$effect(() => {
		if (!chartEl || data.length === 0) return;

		const colorCompleted = '#22c55e';
		const colorCancelled = getColor('--color-text-tertiary') || '#8c8c8c';
		const colorRemaining = '#f59e0b';
		const colorBorder = getColor('--app-border') || '#333333';
		const colorText = getColor('--color-text-tertiary') || '#8c8c8c';
		const colorBg = getColor('--color-bg') || '#1e1e1e';

		const labels = data.map((d) => d.cycle_name);
		const completedData = data.map((d) => d.completed);
		const cancelledData = data.map((d) => d.cancelled);
		const remainingData = data.map((d) => Math.max(0, d.scope - d.completed - d.cancelled));

		if (chart) {
			chart.dispose();
		}

		chart = echarts.init(chartEl, undefined, { renderer: 'canvas' });

		chart.setOption({
			backgroundColor: 'transparent',
			animation: false,
			grid: {
				left: 12,
				right: 12,
				top: 12,
				bottom: 28
			},
			xAxis: {
				type: 'category',
				data: labels,
				axisLine: { lineStyle: { color: colorBorder } },
				axisTick: { show: false },
				axisLabel: {
					color: colorText,
					fontSize: 10,
					interval: 0,
					rotate: data.length > 8 ? 30 : 0
				}
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
				borderWidth: 1,
				borderRadius: 8,
				padding: [6, 12],
				textStyle: { color: colorText, fontSize: 11 },
				formatter: (params: any) => {
					const name = params[0]?.axisValue ?? '';
					const completed = params.find((p: any) => p.seriesName === 'Completed')?.value ?? 0;
					const cancelled = params.find((p: any) => p.seriesName === 'Cancelled')?.value ?? 0;
					const remaining = params.find((p: any) => p.seriesName === 'Remaining')?.value ?? 0;
					const total = completed + cancelled + remaining;
					return `<div><strong>${name}</strong></div>`
						+ `<div style="margin-top:4px">Completed: ${completed}/${total}</div>`
						+ `<div>Cancelled: ${cancelled}</div>`
						+ `<div>Remaining: ${remaining}</div>`;
				}
			},
			series: [
				{
					name: 'Completed',
					type: 'bar',
					stack: 'total',
					data: completedData,
					itemStyle: { color: colorCompleted, borderRadius: [0, 0, 0, 0] },
					barMaxWidth: 32
				},
				{
					name: 'Cancelled',
					type: 'bar',
					stack: 'total',
					data: cancelledData,
					itemStyle: { color: colorCancelled }
				},
				{
					name: 'Remaining',
					type: 'bar',
					stack: 'total',
					data: remainingData,
					itemStyle: { color: colorRemaining, borderRadius: [4, 4, 0, 0] }
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

<div bind:this={chartEl} class="h-[200px] w-full"></div>
