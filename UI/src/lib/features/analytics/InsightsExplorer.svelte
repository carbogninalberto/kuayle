<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import * as echarts from 'echarts';
	import {
		CircleDashed,
		Flag,
		FolderKanban,
		Gauge,
		GitBranch,
		Hash,
		Hourglass,
		Inbox,
		Layers3,
		RefreshCcw,
		Tag,
		Timer,
		User,
		Users
	} from 'lucide-svelte';
	import * as Select from '$lib/components/ui/select';
	import {
		getAnalyticsInsights,
		type AnalyticsFilterParams,
		type AnalyticsInsights,
		type InsightsParams,
		type AnalyticsMeasure,
		type AnalyticsSlice
	} from '$lib/api/analytics';
	import LoadingState from '$lib/components/shared/LoadingState.svelte';
	import ErrorState from '$lib/components/shared/ErrorState.svelte';
	import IssueStatusIcon from '$lib/features/issues/IssueStatusIcon.svelte';
	import IssuePriorityIcon from '$lib/features/issues/IssuePriorityIcon.svelte';
	import type { IssuePriority } from '$lib/types/issue';
	import type { TeamStatus } from '$lib/types/team-status';
	import AnalyticsDateRangePicker from './AnalyticsDateRangePicker.svelte';
	import {
		getAnalyticsChartTheme,
		observeAnalyticsTheme,
		seriesChartColor,
		statusChartColor
	} from './chart-theme';

	let {
		slug,
		filters = {},
		embedded = false,
		statuses = []
	}: { slug: string; filters?: AnalyticsFilterParams; embedded?: boolean; statuses?: TeamStatus[] } = $props();

	let loading = $state(false);
	let error = $state<string | null>(null);
	let result = $state<AnalyticsInsights | null>(null);
	let loadId = 0;

	// svelte-ignore non_reactive_update
	let container: HTMLDivElement;
	let chart: echarts.ECharts | null = null;

	const searchParams = $derived(new URLSearchParams(page.url.search));

	function readUrlParam(key: string): string | null {
		if (embedded) return null;
		const sp = searchParams;
		return sp.get(key);
	}

	const MEASURES = [
		{ value: 'issue_count' as const, label: 'Issue count', icon: Hash },
		{ value: 'issue_age' as const, label: 'Issue age', icon: Hourglass },
		{ value: 'lead_time' as const, label: 'Lead time', icon: Timer },
		{ value: 'cycle_time' as const, label: 'Cycle time', icon: Gauge },
		{ value: 'triage_time' as const, label: 'Triage time', icon: Inbox }
	];

	const SLICES = [
		{ value: 'none' as const, label: 'None', icon: CircleDashed },
		{ value: 'status' as const, label: 'Status', icon: GitBranch },
		{ value: 'status_type' as const, label: 'Status type', icon: Layers3 },
		{ value: 'priority' as const, label: 'Priority', icon: Flag },
		{ value: 'assignee' as const, label: 'Assignee', icon: User },
		{ value: 'team' as const, label: 'Team', icon: Users },
		{ value: 'project' as const, label: 'Project', icon: FolderKanban },
		{ value: 'cycle' as const, label: 'Cycle', icon: RefreshCcw },
		{ value: 'label' as const, label: 'Label', icon: Tag },
		{ value: 'creator' as const, label: 'Creator', icon: User }
	];
	const availableSlices = $derived(SLICES.filter((item) => item.value !== 'status' || !!filters.team_id));

	function isMeasure(value: string | null): value is AnalyticsMeasure {
		return value !== null && MEASURES.some((item) => item.value === value);
	}

	function isSlice(value: string | null): value is AnalyticsSlice {
		return value !== null && SLICES.some((item) => item.value === value);
	}

	function initialMeasure(): AnalyticsMeasure {
		const value = readUrlParam('measure');
		return isMeasure(value) ? value : 'issue_count';
	}

	function initialSlice(key: 'slice' | 'segment', fallback: AnalyticsSlice): AnalyticsSlice {
		const value = readUrlParam(key);
		return isSlice(value) ? value : fallback;
	}

	// Read state from URL (or defaults)
	let measure = $state<AnalyticsMeasure>(initialMeasure());
	let slice = $state<AnalyticsSlice>(initialSlice('slice', 'status_type'));
	let segment = $state<AnalyticsSlice>(initialSlice('segment', 'none'));
	let fromDate = $state(readUrlParam('from') ?? '');
	let toDate = $state(readUrlParam('to') ?? '');
	const selectedMeasure = $derived(MEASURES.find((item) => item.value === measure) ?? MEASURES[0]);
	const selectedSlice = $derived(SLICES.find((item) => item.value === slice) ?? SLICES[0]);
	const selectedSegment = $derived(SLICES.find((item) => item.value === segment) ?? SLICES[0]);
	const MeasureIcon = $derived(selectedMeasure.icon);
	const SliceIcon = $derived(selectedSlice.icon);
	const SegmentIcon = $derived(selectedSegment.icon);

	// Prevent invalid segment = slice
	$effect(() => {
		if (!filters.team_id && slice === 'status') slice = 'status_type';
		if (!filters.team_id && segment === 'status') segment = 'none';
		if (slice === 'none' || segment === slice) {
			segment = 'none';
		}
	});

	// Sync state -> URL (only on full insights page, not embedded)
	$effect(() => {
		if (embedded) return;
		const sp = new URLSearchParams(page.url.search);
		let dirty = false;
		const set = (k: string, v: string, d: string) => {
			if (v !== d && sp.get(k) !== v) {
				sp.set(k, v);
				dirty = true;
			} else if (v === d && sp.has(k)) {
				sp.delete(k);
				dirty = true;
			}
		};
		set('measure', measure, 'issue_count');
		set('slice', slice, filters.team_id ? 'status' : 'status_type');
		set('segment', segment, 'none');
		set('from', fromDate, '');
		set('to', toDate, '');
		if (dirty) {
			const url = new URL(page.url.href);
			url.search = sp.toString();
			void goto(url.pathname + url.search, { replaceState: true, noScroll: true, keepFocus: true });
		}
	});

	function buildParams(): InsightsParams {
		const params: InsightsParams = { ...filters, measure };
		if (slice !== 'none') params.slice = slice;
		if (slice !== 'none' && segment !== 'none' && segment !== slice) params.segment = segment;
		if (fromDate) params.from = fromDate;
		if (toDate) params.to = toDate;
		return params;
	}

	function errorMessage(err: unknown): string {
		if (err && typeof err === 'object' && 'error' in err) {
			const detail = err.error;
			if (detail && typeof detail === 'object' && 'message' in detail && typeof detail.message === 'string') {
				return detail.message;
			}
		}
		return err instanceof Error ? err.message : 'Failed to load insights';
	}

	async function load() {
		const id = ++loadId;
		loading = true;
		error = null;
		result = null;
		chart?.clear();
		try {
			const data = await getAnalyticsInsights(slug, buildParams());
			if (id === loadId) {
				result = data;
				loading = false;
			}
		} catch (err: unknown) {
			if (id === loadId) {
				error = errorMessage(err);
				loading = false;
			}
		}
	}

	$effect(() => {
		measure;
		slice;
		segment;
		fromDate;
		toDate;
		filters;
		load();
	});

	function unitLabel(): string {
		if (!result?.unit) return '';
		return result.unit === 'issues' ? 'issues' : result.unit;
	}

	function fmtValue(val: number | null | undefined): string {
		if (val == null) return '-';
		if (result?.measure === 'issue_count') return val.toLocaleString();
		if (val < 1) return '<1h';
		const days = Math.floor(val / 24);
		const hrs = Math.round(val % 24);
		if (days > 0) return `${days}d ${hrs}h`;
		return `${hrs}h`;
	}

	function isDurationMeasure(): boolean {
		return result?.measure !== 'issue_count';
	}

	function buildChartOption(): echarts.EChartsOption | null {
		const groups = result?.groups;
		if (!groups || groups.length === 0) return null;

		const theme = getAnalyticsChartTheme();
		const names = groups.map((g) => g.label);
		const isDuration = isDurationMeasure();
		const tooltip = {
			trigger: 'axis' as const,
			backgroundColor: theme.background,
			borderColor: theme.border,
			borderWidth: 1,
			borderRadius: 8,
			padding: [6, 10],
			axisPointer: { lineStyle: { color: theme.textTertiary, type: 'dotted' as const, opacity: 0.45 } },
			textStyle: { color: theme.textPrimary, fontSize: 11 }
		};
		const colorForGroup = (key: string, color: string | null | undefined, index: number) =>
			color?.trim() ||
			(result?.slice === 'status_type'
				? statusChartColor(key, null, theme)
				: seriesChartColor(index, theme));

		// Build group key -> index for scatterplot
		const keyIndex = new Map<string, number>();
		groups.forEach((g, i) => keyIndex.set(g.key, i));

		if (result?.segment && result.segment !== 'none') {
			// Build union of segment keys/labels
			const segMap = new Map<string, { label: string; color?: string | null }>();
			for (const g of groups) {
				for (const s of g.segments ?? []) {
					const existing = segMap.get(s.key);
					if (!existing) {
						segMap.set(s.key, { label: s.label, color: s.color });
					} else if (!existing.color && s.color) {
						existing.color = s.color;
					}
				}
			}
			const segKeys = [...segMap.keys()];
			const segLabels = segKeys.map((key) => segMap.get(key)?.label ?? key);

			return {
				backgroundColor: 'transparent',
				tooltip,
				legend: {
					data: segLabels,
					bottom: 0,
					icon: 'circle',
					itemWidth: 8,
					itemHeight: 8,
					textStyle: { fontSize: 10, color: theme.textSecondary }
				},
				grid: { left: 12, right: 12, top: 16, bottom: 44, containLabel: true },
				xAxis: {
					type: 'category' as const,
					data: names,
					axisLabel: { fontSize: 10, color: theme.textTertiary, rotate: names.length > 6 ? 30 : 0 },
					axisTick: { show: false },
					axisLine: { lineStyle: { color: theme.border } }
				},
				yAxis: {
					type: 'value' as const,
					axisLine: { show: false },
					axisTick: { show: false },
					axisLabel: { fontSize: 10, color: theme.textTertiary },
					splitLine: { lineStyle: { color: theme.border, opacity: 0.35 } }
				},
				series: segKeys.map((segmentKey, segmentIndex) => ({
					name: segLabels[segmentIndex],
					type: 'bar' as const,
					stack: 'total',
					data: groups.map((group) => {
						const segmentGroup = group.segments?.find((item) => item.key === segmentKey);
						return segmentGroup
							? isDuration
								? (segmentGroup.p50 ?? segmentGroup.value ?? 0)
								: (segmentGroup.count ?? 0)
							: 0;
					}),
					itemStyle: {
						color:
							segMap.get(segmentKey)?.color?.trim() ||
							(result?.segment === 'status_type'
								? statusChartColor(segmentKey, null, theme)
								: seriesChartColor(segmentIndex, theme)),
						borderRadius: [3, 3, 0, 0]
					},
					barMaxWidth: 36
				}))
			};
		}

		if (isDuration && (result?.points ?? []).length > 0) {
			// Scatter plot: map point slice_key to its group index
			const points = result?.points ?? [];
			const hasSlice = result?.slice !== undefined && result.slice !== 'none';
			// Single series for all points, using xAxis category index
			const scatterData: Array<{ value: number[]; itemStyle?: { color: string } }> = [];
			for (const p of points) {
				const xIdx = p.slice_key ? keyIndex.get(p.slice_key) : hasSlice ? undefined : 0;
				if (xIdx === undefined) continue;
				scatterData.push({
					value: [xIdx, p.value ?? 0],
					itemStyle: { color: '#6366f1' }
				});
			}
			return {
				backgroundColor: 'transparent',
				tooltip: {
					trigger: 'item' as const,
					backgroundColor: theme.background,
					borderColor: theme.border,
					borderWidth: 1,
					borderRadius: 8,
					padding: [6, 10],
					textStyle: { color: theme.textPrimary, fontSize: 11 },
					formatter: (p: unknown) => {
						if (!p || typeof p !== 'object' || !('value' in p) || !Array.isArray(p.value)) return '';
						const [xValue, yValue] = p.value;
						if (typeof xValue !== 'number' || typeof yValue !== 'number') return '';
						const xIdx = Math.round(xValue);
						const name = xIdx >= 0 && xIdx < names.length ? names[xIdx] : 'Other';
						return `${name}<br/>${result?.unit ?? ''}: ${fmtValue(yValue)}`;
					}
				},
				grid: { left: 12, right: 12, top: 16, bottom: 12, containLabel: true },
				xAxis: {
					type: 'category' as const,
					data: names,
					axisLabel: { fontSize: 10, color: theme.textTertiary },
					axisTick: { show: false },
					axisLine: { lineStyle: { color: theme.border } }
				},
				yAxis: {
					type: 'value' as const,
					name: result?.unit ?? '',
					axisLine: { show: false },
					axisTick: { show: false },
					axisLabel: { fontSize: 10, color: theme.textTertiary },
					splitLine: { lineStyle: { color: theme.border, opacity: 0.35 } }
				},
				series: [
					{
						type: 'scatter' as const,
						data: scatterData.map((point) => ({
							...point,
							itemStyle: {
								color: colorForGroup(groups[Math.round(point.value[0])]?.key ?? '', null, Math.round(point.value[0]))
							}
						})),
						symbolSize: 6
					}
				]
			};
		}

		// Simple bar chart
		const values = groups.map((g) => g.count ?? 0);
		const colors = groups.map((g, index) => colorForGroup(g.key, g.color, index));

		return {
			backgroundColor: 'transparent',
			tooltip,
			grid: { left: 12, right: 12, top: 16, bottom: 12, containLabel: true },
			xAxis: {
				type: 'category' as const,
				data: names,
				axisLabel: { fontSize: 10, color: theme.textTertiary, rotate: names.length > 6 ? 30 : 0 },
				axisTick: { show: false },
				axisLine: { lineStyle: { color: theme.border } }
			},
			yAxis: {
				type: 'value' as const,
				axisLine: { show: false },
				axisTick: { show: false },
				axisLabel: { fontSize: 10, color: theme.textTertiary },
				splitLine: { lineStyle: { color: theme.border, opacity: 0.35 } }
			},
			series: [
				{
					type: 'bar' as const,
					data: values.map((val, i) => ({
						value: val,
						itemStyle: { color: colors[i], borderRadius: [4, 4, 0, 0] }
					})),
					barMaxWidth: 36
				}
			]
		};
	}

	function renderChart() {
		if (!container || !chart) return;
		if (loading || error || !result?.groups?.length) {
			chart.clear();
			return;
		}
		const option = buildChartOption();
		if (option) {
			chart.setOption(option, true);
		} else {
			chart.clear();
		}
	}

	const handleResize = () => chart?.resize();

	$effect(() => {
		result;
		loading;
		error;
		renderChart();
	});

	onMount(() => {
		if (container) {
			chart = echarts.init(container, undefined, { renderer: 'canvas' });
			renderChart();
		}
		const resizeObserver = new ResizeObserver(handleResize);
		resizeObserver.observe(container);
		const stopThemeObserver = observeAnalyticsTheme(renderChart);
		return () => {
			loadId += 1;
			resizeObserver.disconnect();
			stopThemeObserver();
			chart?.dispose();
			chart = null;
		};
	});

	function handlePointClick(identifier: string) {
		const ws = page.params.workspaceSlug ?? '';
		goto(`/${ws}/issue/${identifier}`);
	}

	function handleSliceClick(key: string) {
		const ws = slug;
		const p = new URLSearchParams();
		if (result?.slice === 'status') p.set('status', key);
		else if (result?.slice === 'priority') p.set('priority', key);
		else if (result?.slice === 'assignee') p.set('assignee', key);
		else if (result?.slice === 'project') p.set('project', key);
		else if (result?.slice === 'team') p.set('team', key);
		else if (result?.slice === 'label') p.set('label', key);
		else if (result?.slice === 'creator') p.set('creator', key);
		const qs = p.toString();
		goto(`/${ws}/my-issues${qs ? `?${qs}` : ''}`);
	}

	function statusForGroup(key: string): TeamStatus | undefined {
		return statuses.find((status) => status.id === key);
	}
</script>

<div class="space-y-4">
	<div class="flex flex-wrap items-end gap-3 rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]/50 p-3">
		<div class="flex flex-col gap-1">
			<span class="text-[11px] font-medium text-[var(--color-text-tertiary)]">Measure</span>
			<Select.Root type="single" value={measure} onValueChange={(value) => value && (measure = value as AnalyticsMeasure)}>
				<Select.Trigger size="sm" aria-label="Measure" class="w-[170px] bg-[var(--color-bg)]">
					<MeasureIcon size={13} />
					{selectedMeasure.label}
				</Select.Trigger>
				<Select.Content>
					{#each MEASURES as item}
						{@const Icon = item.icon}
						<Select.Item value={item.value}><Icon size={13} />{item.label}</Select.Item>
					{/each}
				</Select.Content>
			</Select.Root>
		</div>
		<div class="flex flex-col gap-1">
			<span class="text-[11px] font-medium text-[var(--color-text-tertiary)]">Group by</span>
			<Select.Root type="single" value={slice} onValueChange={(value) => value && (slice = value as AnalyticsSlice)}>
				<Select.Trigger size="sm" aria-label="Group by" class="w-[160px] bg-[var(--color-bg)]">
					<SliceIcon size={13} />
					{selectedSlice.label}
				</Select.Trigger>
				<Select.Content>
					{#each availableSlices as item}
						{@const Icon = item.icon}
						<Select.Item value={item.value}><Icon size={13} />{item.label}</Select.Item>
					{/each}
				</Select.Content>
			</Select.Root>
		</div>
		{#if slice !== 'none'}
			<div class="flex flex-col gap-1">
				<span class="text-[11px] font-medium text-[var(--color-text-tertiary)]">Segment by</span>
				<Select.Root type="single" value={segment} onValueChange={(value) => value && (segment = value as AnalyticsSlice)}>
					<Select.Trigger size="sm" aria-label="Segment by" class="w-[160px] bg-[var(--color-bg)]">
						<SegmentIcon size={13} />
						{selectedSegment.label}
					</Select.Trigger>
					<Select.Content>
						{#each availableSlices.filter((item) => item.value !== slice) as item}
							{@const Icon = item.icon}
							<Select.Item value={item.value}><Icon size={13} />{item.label}</Select.Item>
						{/each}
					</Select.Content>
				</Select.Root>
			</div>
		{/if}
		<div class="flex min-w-[220px] flex-1 flex-col gap-1 sm:ml-auto sm:flex-none">
			<span class="text-[11px] font-medium text-[var(--color-text-tertiary)]">Date range</span>
			<AnalyticsDateRangePicker
				startDate={fromDate}
				endDate={toDate}
				allowClear
				onchange={(start, end) => { fromDate = start; toDate = end; }}
			/>
		</div>
	</div>

	<!-- Aggregate -->
	{#if result}
		<div class="flex gap-4 text-xs text-[var(--color-text-secondary)]">
			<span>Total: <strong class="text-[var(--color-text-primary)]">{result.total_count ?? '-'}</strong></span>
			{#if result.aggregate != null}
				<span
					>Aggregate: <strong class="text-[var(--color-text-primary)]">{fmtValue(result.aggregate)}</strong>
					{unitLabel()}</span
				>
			{/if}
		</div>
	{/if}

	<!-- Chart -->
	<div class="relative min-h-72 rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
		<div
			bind:this={container}
			class="h-72 w-full {loading || error || !result || (!result.groups?.length && !result.points?.length)
				? 'invisible'
				: ''}"
		></div>
		{#if loading}
			<div class="absolute inset-0"><LoadingState /></div>
		{:else if error}
			<div class="absolute inset-0"><ErrorState message={error} onretry={load} /></div>
		{:else if !result || (!((result.groups ?? []).length > 0) && !((result.points ?? []).length > 0))}
			<div class="absolute inset-0 flex items-center justify-center">
				<p class="text-sm text-[var(--color-text-tertiary)]">
					{#if result && isDurationMeasure() && !((result.points ?? []).length > 0)}
						No data points for this measure yet
					{:else}
						No data available
					{/if}
				</p>
			</div>
		{/if}
	</div>

	<!-- Data Table -->
	{#if result && (result.groups ?? []).length > 0}
		<div class="overflow-x-auto rounded-lg border border-[var(--app-border)]">
			<table class="w-full text-xs">
				<thead>
					<tr class="border-b border-[var(--app-border)] bg-[var(--color-bg-tertiary)]/40">
						<th class="px-3 py-2 text-left font-medium text-[var(--color-text-secondary)]">
							{slice === 'none' ? 'Group' : slice.charAt(0).toUpperCase() + slice.slice(1)}
						</th>
						{#if isDurationMeasure()}
							<th class="px-3 py-2 text-right font-medium text-[var(--color-text-secondary)]">P50</th>
							<th class="px-3 py-2 text-right font-medium text-[var(--color-text-secondary)]">P75</th>
							<th class="px-3 py-2 text-right font-medium text-[var(--color-text-secondary)]">P95</th>
						{:else}
							<th class="px-3 py-2 text-right font-medium text-[var(--color-text-secondary)]">Count</th>
						{/if}
					</tr>
				</thead>
				<tbody>
					{#each result.groups as group}
						{@const groupStatus = statusForGroup(group.key)}
						<tr
							class="border-b border-[var(--app-border)] last:border-0 hover:bg-[var(--color-bg-hover)] cursor-pointer"
							onclick={() => handleSliceClick(group.key)}
						>
							<td class="px-3 py-2">
								<div class="flex items-center gap-2">
									{#if slice === 'status'}
										<IssueStatusIcon category={groupStatus?.category} color={groupStatus?.color ?? group.color} size={14} />
									{:else if slice === 'status_type'}
										<IssueStatusIcon category={group.key} size={14} />
									{:else if slice === 'priority'}
										<span class="text-[var(--color-text-secondary)]"><IssuePriorityIcon priority={Number(group.key) as IssuePriority} size={14} /></span>
									{:else if group.color}
										<span class="inline-block h-2.5 w-2.5 shrink-0 rounded-full" style="background-color: {group.color}"
										></span>
									{/if}
									<span class="text-[var(--color-text-primary)]">{group.label}</span>
								</div>
							</td>
							{#if isDurationMeasure()}
								<td class="px-3 py-2 text-right tabular-nums text-[var(--color-text-secondary)]"
									>{fmtValue(group.p50)}</td
								>
								<td class="px-3 py-2 text-right tabular-nums text-[var(--color-text-secondary)]"
									>{fmtValue(group.p75)}</td
								>
								<td class="px-3 py-2 text-right tabular-nums text-[var(--color-text-secondary)]"
									>{fmtValue(group.p95)}</td
								>
							{:else}
								<td class="px-3 py-2 text-right tabular-nums text-[var(--color-text-secondary)]"
									>{group.count ?? '-'}</td
								>
							{/if}
						</tr>
					{/each}
				</tbody>
			</table>
		</div>

		<!-- Points (individual issues) -->
		{#if (result.points ?? []).length > 0}
			<div class="rounded-lg border border-[var(--app-border)]">
				<div class="border-b border-[var(--app-border)] bg-[var(--color-bg-tertiary)]/40 px-3 py-1.5">
					<span class="text-xs font-medium text-[var(--color-text-secondary)]">Issues ({result.points?.length})</span>
				</div>
				<table class="w-full text-xs">
					<thead>
						<tr class="border-b border-[var(--app-border)]">
							<th class="px-3 py-1.5 text-left font-medium text-[var(--color-text-tertiary)]">Issue</th>
							<th class="px-3 py-1.5 text-left font-medium text-[var(--color-text-tertiary)]">Title</th>
							<th class="px-3 py-1.5 text-right font-medium text-[var(--color-text-tertiary)]">Value</th>
						</tr>
					</thead>
					<tbody>
						{#each result.points as point}
							<tr
								class="border-b border-[var(--app-border)] last:border-0 hover:bg-[var(--color-bg-hover)] cursor-pointer"
								onclick={() => handlePointClick(point.identifier)}
							>
								<td class="px-3 py-1.5 font-mono text-[var(--app-accent-light)]">{point.identifier}</td>
								<td class="px-3 py-1.5 text-[var(--color-text-primary)]">{point.title}</td>
								<td class="px-3 py-1.5 text-right tabular-nums text-[var(--color-text-secondary)]"
									>{fmtValue(point.value)}</td
								>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}
	{/if}
</div>
