<script lang="ts">
	import { onMount } from 'svelte';
	import * as echarts from 'echarts';
	import { observeAnalyticsTheme } from '$lib/features/analytics/chart-theme';
	import type { ChartSpec } from './rich-blocks';

	let { spec, onfail }: { spec: ChartSpec; onfail: () => void } = $props();

	// svelte-ignore non_reactive_update
	let container: HTMLDivElement;
	let chart: echarts.ECharts | null = null;
	let failed = false;

	const fallbackColors = ['#7ea6f7', '#4f7df3', '#2f5de0', '#2447c4', '#1c368f'];

	function fail() {
		if (failed) return;
		failed = true;
		onfail();
	}

	// The tokens are oklch, which echarts' colour code may not parse: paint one
	// pixel and read its sRGB bytes back.
	function resolveColor(name: string, fallback: string): string {
		const raw = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
		if (!raw) return fallback;
		const canvas = document.createElement('canvas');
		canvas.width = 1;
		canvas.height = 1;
		const context = canvas.getContext('2d');
		if (!context) return fallback;
		context.fillStyle = fallback;
		context.fillStyle = raw;
		context.fillRect(0, 0, 1, 1);
		const [red, green, blue] = context.getImageData(0, 0, 1, 1).data;
		return `rgb(${red}, ${green}, ${blue})`;
	}

	function buildOption(): echarts.EChartsOption {
		const colors = fallbackColors.map((fallback, index) => resolveColor(`--chart-${index + 1}`, fallback));
		const foreground = resolveColor('--foreground', '#1a1a1a');
		const muted = resolveColor('--muted-foreground', '#6b6b6b');
		const border = resolveColor('--border', '#d4d4d4');
		const pie = spec.type === 'pie';

		return {
			backgroundColor: 'transparent',
			animation: false,
			title: spec.title
				? { text: spec.title, left: 8, top: 4, textStyle: { color: foreground, fontSize: 13, fontWeight: 600 } }
				: undefined,
			tooltip: {
				trigger: pie ? 'item' : 'axis',
				// richText draws model-provided names on the canvas instead of innerHTML.
				renderMode: 'richText',
				backgroundColor: border,
				borderColor: border,
				borderWidth: 1,
				borderRadius: 6,
				padding: [5, 8],
				textStyle: { color: foreground, fontSize: 12 }
			},
			legend:
				pie || spec.series.length > 1
					? {
							top: spec.title ? 28 : 4,
							left: 8,
							icon: 'circle',
							itemWidth: 8,
							itemHeight: 8,
							itemGap: 14,
							textStyle: { color: muted, fontSize: 11 }
						}
					: undefined,
			grid: { left: 8, right: 16, top: spec.title ? 52 : 16, bottom: 8, containLabel: true },
			xAxis: pie
				? undefined
				: {
						type: 'category',
						data: spec.categories,
						axisLabel: { color: muted, fontSize: 10, hideOverlap: true },
						axisTick: { show: false },
						axisLine: { lineStyle: { color: border } }
					},
			yAxis: pie
				? undefined
				: {
						type: 'value',
						axisLine: { show: false },
						axisTick: { show: false },
						axisLabel: {
							color: muted,
							fontSize: 10,
							formatter: spec.unit ? (value: number) => `${value} ${spec.unit}` : undefined
						},
						splitLine: { lineStyle: { color: border, opacity: 0.35 } }
					},
			series: pie
				? [
						{
							type: 'pie',
							name: spec.series[0].name,
							radius: ['32%', '62%'],
							center: ['50%', '55%'],
							data: spec.categories.map((name, index) => ({
								name,
								value: spec.series[0].data[index],
								itemStyle: { color: colors[index % colors.length] }
							})),
							label: { color: muted, fontSize: 10 },
							itemStyle: { borderColor: 'transparent', borderWidth: 1 }
						}
					]
				: spec.series.map((series, index) => {
						const base = {
							name: series.name,
							data: series.data,
							itemStyle: { color: colors[index] }
						};
						if (spec.type === 'bar') return { ...base, type: 'bar' as const, barMaxWidth: 32 };
						return {
							...base,
							type: 'line' as const,
							smooth: false,
							symbol: 'circle',
							symbolSize: 5,
							lineStyle: { color: colors[index], width: 2 },
							areaStyle:
								spec.type === 'area'
									? { color: echarts.color.modifyAlpha(colors[index], 0.18) }
									: undefined
						};
					})
		};
	}

	function render() {
		if (!chart) return;
		chart.setOption(buildOption(), true);
	}

	onMount(() => {
		try {
			chart = echarts.init(container, undefined, { renderer: 'canvas' });
			render();
		} catch {
			fail();
			return;
		}
		const resizeObserver = new ResizeObserver(() => chart?.resize());
		resizeObserver.observe(container);
		const stopThemeObserver = observeAnalyticsTheme(() => {
			try {
				render();
			} catch {
				fail();
			}
		});
		return () => {
			resizeObserver.disconnect();
			stopThemeObserver();
			chart?.dispose();
			chart = null;
		};
	});

	const label = $derived([spec.title, ...spec.series.map((series) => series.name)].filter(Boolean).join(', '));
</script>

<div bind:this={container} class="h-64 w-full" role="img" aria-label={label}></div>
