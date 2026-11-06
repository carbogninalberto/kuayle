<script lang="ts">
	import { AreaChart } from 'layerchart';
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
			title: m['machines.chart_cpu'](),
			color: 'var(--chart-2)',
			data: points((sample) => sample.cpu_percent / 100),
			limit: cpuLimit,
			current: m['machines.usage_of_limit']({
				used: number((latest?.cpu_percent ?? 0) / 100),
				limit: number(cpuLimit, 0),
				unit: m['machines.cpu_unit']()
			}),
			detail: m['machines.chart_cpu_desc'](),
			warning: false
		},
		{
			key: 'memory',
			title: m['machines.chart_memory'](),
			color: 'var(--chart-3)',
			data: points((sample) => sample.memory_bytes / GIB),
			limit: memoryLimit,
			current: m['machines.usage_of_limit']({
				used: number((latest?.memory_bytes ?? 0) / GIB),
				limit: number(memoryLimit, 0),
				unit: m['machines.gb_unit']()
			}),
			detail: m['machines.chart_memory_desc'](),
			warning: false
		},
		{
			key: 'disk',
			title: m['machines.workspace_disk'](),
			color: 'var(--chart-4)',
			data: points((sample) => sample.disk_bytes / GIB),
			limit: machine.disk_gb,
			current: m['machines.usage_amount']({
				used: number((latest?.disk_bytes ?? 0) / GIB),
				unit: m['machines.gb_unit']()
			}),
			detail: diskLimitLabel,
			warning: diskNearLimit
		}
	]);
</script>

<Card.Root data-testid="machine-usage">
	<Card.Header class="flex flex-row flex-wrap items-start justify-between gap-3">
		<div class="space-y-1">
			<Card.Title class="text-sm">{m['machines.usage_title']()}</Card.Title>
			<Card.Description>{m['machines.usage_desc']()}</Card.Description>
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
				<p class="text-[11px] font-medium uppercase tracking-wider text-muted-foreground">{chart.title}</p>
				<p class="mt-1 text-sm font-semibold tabular-nums {chart.warning ? 'text-[var(--color-warning)]' : ''}">
					{chart.current}
				</p>
				<p class="text-xs text-muted-foreground">{chart.detail}</p>
				{#if chart.warning}<p class="mt-0.5 text-xs font-medium text-[var(--color-warning)]">
						{m['machines.disk_near_limit']()}
					</p>{/if}
				{#if chart.data.length < 2}
					<p
						class="flex h-32 items-center justify-center text-center text-xs text-muted-foreground"
						data-testid="machine-usage-empty"
					>
						{loaded ? m['machines.usage_empty']() : m['machines.usage_loading']()}
					</p>
				{:else}
					<Chart.Container
						config={{ value: { label: chart.title, color: chart.color } }}
						class="mt-2 aspect-auto h-32 w-full"
					>
						<AreaChart
							data={chart.data}
							x="date"
							y="value"
							yDomain={[0, chart.limit]}
							series={[{ key: 'value', label: chart.title, color: chart.color }]}
							props={{
								area: { 'fill-opacity': 0.25, line: { class: 'stroke-[1.5]' } },
								xAxis: { format: (value: Date) => time(value), ticks: 3 },
								// Zero, half and the machine's limit, so the top tick names the limit.
								yAxis: {
									format: (value: number) => number(value, Number.isInteger(value) ? 0 : 1),
									ticks: [0, chart.limit / 2, chart.limit]
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
