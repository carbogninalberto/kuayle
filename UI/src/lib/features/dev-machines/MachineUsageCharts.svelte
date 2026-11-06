<script lang="ts">
	import { AreaChart } from 'layerchart';
	import { Activity, Cpu, HardDrive, MemoryStick } from '@lucide/svelte';
	import * as Chart from '$lib/components/ui/chart/index.js';
	import * as Card from '$lib/components/ui/card/index.js';
	import * as ToggleGroup from '$lib/components/ui/toggle-group/index.js';
	import type { ResourceUsageWindow } from '$lib/api/dev-machines';
	import type { DevMachine, ResourceSample } from '$lib/types/dev-machine';
	import { getLocale } from '$lib/paraglide/runtime.js';
	import { m } from '$lib/paraglide/messages.js';

	// CPU, memory and disk history of one machine: oldest first, one sample per
	// time bucket of the selected window. Each chart's ceiling is the machine's
	// limit, so a full chart means the machine is at its size.
	let {
		machine,
		samples,
		window = $bindable('1h'),
		loaded = true,
		diskLimitLabel,
		diskNearLimit = false
	}: {
		machine: DevMachine;
		samples: ResourceSample[];
		window?: ResourceUsageWindow;
		loaded?: boolean;
		diskLimitLabel: string;
		diskNearLimit?: boolean;
	} = $props();

	const GIB = 1024 ** 3;
	const windows: ResourceUsageWindow[] = ['1h', '6h', '24h'];
	const windowMs: Record<ResourceUsageWindow, number> = { '1h': 3_600_000, '6h': 21_600_000, '24h': 86_400_000 };
	// The x axis spans the whole window up to now (renewed with each refresh),
	// so a new machine's first samples start at the right edge instead of
	// stretching across the chart.
	const xDomain = $derived.by(() => {
		void samples;
		const end = new Date();
		return [new Date(end.getTime() - windowMs[window]), end];
	});
	const latest = $derived(samples.at(-1));
	const cpuLimit = $derived(machine.cpu_millis / 1000);
	const memoryLimit = $derived(machine.memory_mb / 1024);

	const round = (value: number) => Math.round(value * 100) / 100;
	const points = (value: (sample: ResourceSample) => number) =>
		samples.map((sample) => ({ date: new Date(sample.created_at), value: round(value(sample)) }));

	const number = (value: number, digits = 1) =>
		value.toLocaleString(getLocale(), { maximumFractionDigits: digits, minimumFractionDigits: digits });
	const time = (value: Date) => value.toLocaleTimeString(getLocale(), { hour: '2-digit', minute: '2-digit' });

	const charts = $derived([
		{
			key: 'cpu',
			icon: Cpu,
			unit: m['machines.cpu_unit'](),
			title: m['machines.chart_cpu'](),
			color: 'var(--chart-2)',
			data: points((sample) => sample.cpu_percent / 100),
			limit: cpuLimit,
			current: m['machines.usage_amount']({
				used: number((latest?.cpu_percent ?? 0) / 100),
				unit: m['machines.cpu_unit']()
			}),
			limitLabel: m['machines.chart_limit']({ limit: number(cpuLimit, 0), unit: m['machines.cpu_unit']() }),
			warning: false
		},
		{
			key: 'memory',
			icon: MemoryStick,
			unit: m['machines.gb_unit'](),
			title: m['machines.chart_memory'](),
			color: 'var(--chart-3)',
			data: points((sample) => sample.memory_bytes / GIB),
			limit: memoryLimit,
			current: m['machines.usage_amount']({
				used: number((latest?.memory_bytes ?? 0) / GIB),
				unit: m['machines.gb_unit']()
			}),
			limitLabel: m['machines.chart_limit']({ limit: number(memoryLimit, 0), unit: m['machines.gb_unit']() }),
			warning: false
		},
		{
			key: 'disk',
			icon: HardDrive,
			unit: m['machines.gb_unit'](),
			title: m['machines.workspace_disk'](),
			color: 'var(--chart-4)',
			data: points((sample) => sample.disk_bytes / GIB),
			limit: machine.disk_gb,
			current: m['machines.usage_amount']({
				used: number((latest?.disk_bytes ?? 0) / GIB),
				unit: m['machines.gb_unit']()
			}),
			limitLabel: diskLimitLabel,
			warning: diskNearLimit
		}
	]);
</script>

<Card.Root data-testid="machine-usage">
	<Card.Header class="flex flex-row flex-wrap items-start justify-between gap-3">
		<div class="space-y-1">
			<Card.Title class="flex items-center gap-2 text-sm"
				><Activity class="size-4 text-muted-foreground" aria-hidden="true" />{m['machines.usage_title']()}</Card.Title
			>
			<Card.Description class="hidden sm:block">{m['machines.usage_desc']()}</Card.Description>
		</div>
		<ToggleGroup.Root
			type="single"
			variant="outline"
			size="sm"
			value={window}
			onValueChange={(value) => {
				if (value) window = value as ResourceUsageWindow;
			}}
			aria-label={m['machines.usage_window']()}
		>
			{#each windows as option (option)}
				<ToggleGroup.Item value={option} class="px-3 text-xs">{option}</ToggleGroup.Item>
			{/each}
		</ToggleGroup.Root>
	</Card.Header>
	<Card.Content class="grid gap-4 lg:grid-cols-3">
		{#each charts as chart (chart.key)}
			<section class="min-w-0 rounded-lg border p-3" aria-label={chart.title} data-testid="machine-usage-{chart.key}">
				<div class="flex items-start justify-between gap-2">
					<p class="flex items-center gap-1.5 text-[11px] font-medium uppercase tracking-wider text-muted-foreground">
						<chart.icon class="size-3.5" aria-hidden="true" />{chart.title}
					</p>
					<p
						class="text-right text-xs text-muted-foreground tabular-nums"
						data-testid="machine-usage-{chart.key}-limit"
					>
						{chart.limitLabel}
					</p>
				</div>
				<p class="mt-1 text-sm font-semibold tabular-nums {chart.warning ? 'text-[var(--color-warning)]' : ''}">
					{chart.current}
				</p>
				{#if chart.warning}<p class="mt-0.5 text-xs font-medium text-[var(--color-warning)]">
						{m['machines.disk_near_limit']()}
					</p>{/if}
				{#if chart.data.length < 2}
					<p
						class="flex h-40 items-center justify-center sm:h-52 text-center text-xs text-muted-foreground"
						data-testid="machine-usage-empty"
					>
						{loaded ? m['machines.usage_empty']() : m['machines.usage_loading']()}
					</p>
				{:else}
					<Chart.Container
						config={{ value: { label: chart.title, color: chart.color } }}
						class="mt-3 aspect-auto h-40 w-full sm:h-52"
					>
						<AreaChart
							data={chart.data}
							x="date"
							y="value"
							{xDomain}
							yDomain={[0, chart.limit]}
							padding={{ top: 12, left: 68, bottom: 40, right: 32 }}
							series={[{ key: 'value', label: chart.title, color: chart.color }]}
							props={{
								area: { 'fill-opacity': 0.25, line: { class: 'stroke-[1.5]' } },
								// tickLength sets the label's distance from the axis; the marks are hidden.
								xAxis: { format: (value: Date) => time(value), tickSpacing: 110, tickLength: 16, tickMarks: false },
								// Zero, half and the machine's limit, so the top tick names the limit.
								yAxis: {
									format: (value: number) => `${number(value, Number.isInteger(value) ? 0 : 1)} ${chart.unit}`,
									ticks: [0, chart.limit / 2, chart.limit],
									tickLength: 12,
									tickMarks: false
								}
							}}
						>
							{#snippet tooltip()}
								<Chart.Tooltip labelFormatter={(value: Date) => value.toLocaleString(getLocale())} indicator="line" />
							{/snippet}
						</AreaChart>
					</Chart.Container>
				{/if}
			</section>
		{/each}
	</Card.Content>
</Card.Root>
