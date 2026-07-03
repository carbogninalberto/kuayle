<script lang="ts">
	import type { Issue, IssueStatus } from '$lib/types/issue';
	import { STATUS_LABELS } from '$lib/types/issue';
	import type { Cycle } from '$lib/types/cycle';
	import * as echarts from 'echarts';
	import { Filter, X } from 'lucide-svelte';

	let {
		issues,
		cycles = [],
		onissueclick
	}: {
		issues: Issue[];
		cycles?: Cycle[];
		onissueclick?: (issue: Issue) => void;
	} = $props();

	let chartEl: HTMLDivElement | undefined = $state();
	let containerEl: HTMLDivElement | undefined = $state();
	let chart: echarts.ECharts | undefined;

	// Filters
	let showFilters = $state(false);
	let filterStatus = $state<Set<IssueStatus>>(new Set());
	let filterHasDueDate = $state<'all' | 'yes' | 'no'>('all');

	const statusOptions: IssueStatus[] = ['in_progress', 'in_review', 'todo', 'backlog', 'done', 'cancelled'];

	function toggleStatus(s: IssueStatus) {
		const next = new Set(filterStatus);
		if (next.has(s)) next.delete(s); else next.add(s);
		filterStatus = next;
	}

	function clearFilters() {
		filterStatus = new Set();
		filterHasDueDate = 'all';
	}

	const hasActiveFilters = $derived(filterStatus.size > 0 || filterHasDueDate !== 'all');

	const filteredIssues = $derived.by(() => {
		let result = issues;
		if (filterStatus.size > 0) {
			result = result.filter(i => filterStatus.has(i.status));
		}
		if (filterHasDueDate === 'yes') {
			result = result.filter(i => i.due_date);
		} else if (filterHasDueDate === 'no') {
			result = result.filter(i => !i.due_date);
		}
		return result;
	});

	const MS_PER_DAY = 86400000;

	function getColor(varName: string): string {
		return getComputedStyle(document.documentElement).getPropertyValue(varName).trim();
	}

	function formatDate(d: Date): string {
		return d.toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
	}

	function escapeHtml(value: string): string {
		return value.replace(/[&<>"']/g, (char) => ({
			'&': '&amp;',
			'<': '&lt;',
			'>': '&gt;',
			'"': '&quot;',
			"'": '&#39;'
		}[char] ?? char));
	}

	// Sorted: by due_date first, then created_at, reversed for ECharts bottom-up y-axis
	const sortedIssues = $derived.by(() => {
		const withDue = filteredIssues.filter(i => i.due_date).sort((a, b) => a.due_date!.localeCompare(b.due_date!));
		const withoutDue = filteredIssues.filter(i => !i.due_date).sort((a, b) => a.created_at.localeCompare(b.created_at));
		return [...withDue, ...withoutDue].reverse();
	});

	const dateRange = $derived.by(() => {
		let min = new Date();
		let max = new Date();
		let hasDate = false;

		for (const issue of filteredIssues) {
			const created = new Date(issue.created_at);
			const due = issue.due_date ? new Date(issue.due_date) : null;
			if (!hasDate) {
				min = created;
				max = due ?? created;
				hasDate = true;
			} else {
				if (created < min) min = created;
				if (due && due > max) max = due;
				if (created > max) max = created;
			}
		}

		for (const cycle of cycles) {
			if (cycle.start_date) {
				const d = new Date(cycle.start_date);
				if (d < min) min = d;
			}
			if (cycle.end_date) {
				const d = new Date(cycle.end_date);
				if (d > max) max = d;
			}
		}

		const padded_min = new Date(min);
		padded_min.setDate(padded_min.getDate() - 5);
		const padded_max = new Date(max);
		padded_max.setDate(padded_max.getDate() + 5);

		return { min: padded_min, max: padded_max };
	});

	function statusColor(status: string): string {
		switch (status) {
			case 'done': return getColor('--color-success') || '#22c55e';
			case 'in_progress': case 'in_review': return getColor('--app-accent') || '#6650eb';
			case 'cancelled': return getColor('--color-text-tertiary') || '#8c8c8c';
			default: return getColor('--color-text-secondary') || '#a0a0a0';
		}
	}

	function truncateText(text: string, maxLen: number): string {
		return text.length > maxLen ? text.slice(0, maxLen - 1) + '…' : text;
	}

	$effect(() => {
		if (!chartEl || sortedIssues.length === 0) return;

		const colorBorder = getColor('--app-border') || '#333333';
		const colorText = getColor('--color-text-tertiary') || '#8c8c8c';
		const colorTextPrimary = getColor('--color-text-primary') || '#ffffff';
		const colorBg = getColor('--color-bg') || '#1e1e1e';
		const colorAccent = getColor('--app-accent') || '#6650eb';

		const categories = sortedIssues.map((_, i) => String(i));
		const barHeight = 22;
		const rowHeight = 32;

		if (chart) {
			chart.dispose();
		}

		chart = echarts.init(chartEl, undefined, { renderer: 'canvas' });

		const issueData: any[] = sortedIssues.map((issue, idx) => {
			const created = new Date(issue.created_at);
			const due = issue.due_date ? new Date(issue.due_date) : null;
			const barStart = created.getTime();
			const barEnd = due ? due.getTime() : created.getTime() + MS_PER_DAY;
			return {
				value: [idx, barStart, barEnd, due ? 1 : 0],
				itemStyle: {
					color: statusColor(issue.status),
					opacity: due ? 0.75 : 0.3,
					borderRadius: 3
				}
			};
		});

		const cycleColors = ['#6366f1', '#8b5cf6', '#a855f7'];
		// Group overlapping cycles by date range so we merge their labels
		const cycleGroups = new Map<string, { names: string[]; start: number; end: number }>();
		for (const cycle of cycles) {
			if (cycle.start_date && cycle.end_date) {
				const start = new Date(cycle.start_date).getTime();
				const end = new Date(cycle.end_date).getTime();
				const key = `${start}-${end}`;
				const existing = cycleGroups.get(key);
				if (existing) {
					existing.names.push(cycle.name);
				} else {
					cycleGroups.set(key, { names: [cycle.name], start, end });
				}
			}
		}
		const cycleAreas: any[] = [];
		let cycleIdx = 0;
		for (const group of cycleGroups.values()) {
			const cColor = cycleColors[cycleIdx % cycleColors.length];
			const label = group.names.join(' / ');
			cycleAreas.push([
				{
					xAxis: group.start,
					itemStyle: { color: cColor + '18', borderWidth: 1, borderType: 'dashed', borderColor: cColor + '35' },
					label: {
						show: true,
						position: 'insideTop',
						formatter: label,
						fontSize: 10,
						fontWeight: 500,
						color: cColor + '99',
						padding: [4, 8]
					}
				},
				{ xAxis: group.end, label: { show: false } }
			]);
			cycleIdx++;
		}

		const today = new Date();
		const todayInRange = today >= dateRange.min && today <= dateRange.max;

		chart.setOption({
			animation: false,
			backgroundColor: 'transparent',
			grid: {
				left: 12,
				right: 12,
				top: cycles.length > 0 ? 28 : 16,
				bottom: 44
			},
			xAxis: {
				type: 'time',
				min: dateRange.min.getTime(),
				max: dateRange.max.getTime(),
				axisLine: { lineStyle: { color: 'transparent' } },
				axisTick: {
					show: true,
					lineStyle: { color: colorText, opacity: 0.5, width: 1 },
					length: 6
				},
				axisLabel: {
					color: colorText,
					fontSize: 10,
					formatter: (value: number) => formatDate(new Date(value))
				},
				splitLine: {
					show: true,
					lineStyle: { color: colorBorder, opacity: 0.2, type: 'dotted' }
				}
			},
			yAxis: {
				type: 'category',
				data: categories,
				axisLine: { show: false },
				axisTick: { show: false },
				axisLabel: { show: false },
				splitLine: { show: false }
			},
			tooltip: {
				backgroundColor: colorBg,
				borderColor: colorBorder,
				borderWidth: 1,
				borderRadius: 8,
				padding: [6, 12],
				textStyle: { color: colorText, fontSize: 11 },
				formatter: (params: any) => {
					const idx = params.value[0];
					const issue = sortedIssues[idx];
					if (!issue) return '';
					const created = formatDate(new Date(issue.created_at));
					const due = issue.due_date ? formatDate(new Date(issue.due_date)) : 'No due date';
					return `<div style="max-width:280px">
						<div style="font-weight:500;color:${colorTextPrimary};margin-bottom:3px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">${escapeHtml(issue.title)}</div>
						<div style="display:flex;gap:8px;font-size:10px">
							<span>${issue.identifier}</span>
							<span>${created} → ${due}</span>
						</div>
					</div>`;
				}
			},
			dataZoom: [
				{
					type: 'inside',
					xAxisIndex: 0,
					filterMode: 'none',
					zoomOnMouseWheel: 'shift',
					moveOnMouseMove: false,
					moveOnMouseWheel: true,
					preventDefaultMouseMove: false
				},
				{
					type: 'slider',
					xAxisIndex: 0,
					height: 16,
					bottom: 4,
					borderColor: colorBorder,
					backgroundColor: colorBg,
					fillerColor: colorAccent + '20',
					handleStyle: { color: colorAccent, borderColor: colorAccent },
					dataBackground: {
						lineStyle: { color: 'transparent' },
						areaStyle: { color: 'transparent' }
					},
					selectedDataBackground: {
						lineStyle: { color: 'transparent' },
						areaStyle: { color: 'transparent' }
					},
					textStyle: { color: colorText, fontSize: 9 },
					labelFormatter: (value: string) => formatDate(new Date(value)),
					filterMode: 'none'
				}
			],
			series: [
				{
					type: 'custom',
					renderItem: (params: any, api: any) => {
						const categoryIndex = api.value(0);
						const startTime = api.value(1);
						const endTime = api.value(2);
						const hasDue = api.value(3);

						const start = api.coord([startTime, categoryIndex]);
						const end = api.coord([endTime, categoryIndex]);
						const barW = Math.max(end[0] - start[0], hasDue ? 6 : 10);

						const rectShape = echarts.graphic.clipRectByRect(
							{
								x: start[0],
								y: start[1] - barHeight / 2,
								width: barW,
								height: barHeight
							},
							{
								x: params.coordSys.x,
								y: params.coordSys.y,
								width: params.coordSys.width,
								height: params.coordSys.height
							}
						);

						if (!rectShape) return;

						// Only add text label if bar is wide enough
						const minTextWidth = 36;
						if (rectShape.width >= minTextWidth && hasDue) {
							const issue = sortedIssues[categoryIndex];
							const title = issue?.title ?? '';
							const maxChars = Math.floor((rectShape.width - 12) / 6);

							return {
								type: 'group',
								children: [
									{
										type: 'rect',
										shape: { ...rectShape, r: 3 },
										style: api.style()
									},
									{
										type: 'text',
										style: {
											text: truncateText(title, maxChars),
											x: rectShape.x + 6,
											y: rectShape.y + barHeight / 2,
											textVerticalAlign: 'middle',
											fill: '#fff',
											fontSize: 10,
											fontWeight: 500,
											opacity: 0.9
										}
									}
								]
							};
						}

						return {
							type: 'rect',
							shape: { ...rectShape, r: 3 },
							style: api.style()
						};
					},
					data: issueData,
					encode: {
						x: [1, 2],
						y: 0
					},
					markArea: cycleAreas.length > 0 ? { silent: true, data: cycleAreas } : undefined,
					markLine: todayInRange ? {
						silent: true,
						symbol: 'none',
						label: {
							show: true,
							position: 'start',
							formatter: 'Today',
							fontSize: 9,
							color: '#ef4444',
							padding: [0, 0, 0, 4]
						},
						data: [{ xAxis: today.getTime() }],
						lineStyle: { color: '#ef4444', width: 1, type: 'dashed', opacity: 0.5 }
					} : undefined
				}
			]
		});

		chart.on('click', (params: any) => {
			if (params.componentType === 'series' && params.value) {
				const idx = params.value[0];
				const issue = sortedIssues[idx];
				if (issue) onissueclick?.(issue);
			}
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

<div bind:this={containerEl} class="flex h-full flex-col">
	<!-- Toolbar -->
	<div class="flex items-center gap-2 pb-3">
		<button
			onclick={() => showFilters = !showFilters}
			class="flex items-center gap-1.5 rounded-md border border-[var(--app-border)] px-2.5 py-1 text-xs text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] {hasActiveFilters ? 'border-[var(--app-accent)] text-[var(--app-accent)]' : ''}"
		>
			<Filter size={12} />
			Filter
			{#if hasActiveFilters}
				<span class="rounded-full bg-[var(--app-accent)] px-1.5 text-[9px] font-medium text-white">{filterStatus.size + (filterHasDueDate !== 'all' ? 1 : 0)}</span>
			{/if}
		</button>
		{#if hasActiveFilters}
			<button
				onclick={clearFilters}
				class="flex items-center gap-1 rounded-md px-2 py-1 text-xs text-[var(--color-text-tertiary)] hover:text-[var(--color-text-secondary)]"
			>
				<X size={12} />
				Clear
			</button>
		{/if}
		<span class="text-[11px] text-[var(--color-text-tertiary)]">{filteredIssues.length} issue{filteredIssues.length !== 1 ? 's' : ''}</span>
	</div>

	<!-- Filter bar -->
	{#if showFilters}
		<div class="flex flex-wrap items-center gap-3 rounded-md border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-3 py-2 mb-3">
			<div class="flex items-center gap-1.5">
				<span class="text-[10px] font-medium uppercase text-[var(--color-text-tertiary)]">Status</span>
				{#each statusOptions as s}
					<button
						onclick={() => toggleStatus(s)}
						class="rounded-md px-2 py-0.5 text-[11px] {filterStatus.has(s) ? 'bg-[var(--app-accent)] text-white' : 'bg-[var(--color-bg-tertiary)] text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]'}"
					>
						{STATUS_LABELS[s]}
					</button>
				{/each}
			</div>
			<div class="h-4 w-px bg-[var(--app-border)]"></div>
			<div class="flex items-center gap-1.5">
				<span class="text-[10px] font-medium uppercase text-[var(--color-text-tertiary)]">Due date</span>
				{#each [['all', 'All'], ['yes', 'Has due'], ['no', 'No due']] as [val, label]}
					<button
						onclick={() => filterHasDueDate = val as any}
						class="rounded-md px-2 py-0.5 text-[11px] {filterHasDueDate === val ? 'bg-[var(--app-accent)] text-white' : 'bg-[var(--color-bg-tertiary)] text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]'}"
					>
						{label}
					</button>
				{/each}
			</div>
		</div>
	{/if}

	<!-- Chart -->
	<div class="flex-1 min-h-0">
		<div bind:this={chartEl} class="h-full w-full"></div>
	</div>

	{#if filteredIssues.length === 0}
		<div class="flex h-24 items-center justify-center text-sm text-[var(--color-text-tertiary)]">
			{hasActiveFilters ? 'No issues match the current filters' : 'No issues to display'}
		</div>
	{/if}
</div>
