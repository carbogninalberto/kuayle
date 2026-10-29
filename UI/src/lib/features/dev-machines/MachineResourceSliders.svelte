<script lang="ts">
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Slider } from '$lib/components/ui/slider';
	import { m } from '$lib/paraglide/messages.js';
	import {
		formatMachineResource,
		memoryGb,
		minimumMemoryIndexForCpu,
		normalizeMachineResources,
		snapDiskGb,
		type MachineSizeOptions
	} from '$lib/features/dev-machines/machine-size';

	let {
		sizeOptions,
		inputId = 'machine-resources',
		disabled = false,
		cpu_millis = $bindable(),
		memory_mb = $bindable(),
		disk_gb = $bindable()
	}: {
		sizeOptions: MachineSizeOptions;
		// Prefixes control ids when several instances share a page.
		inputId?: string;
		disabled?: boolean;
		cpu_millis: number;
		memory_mb: number;
		disk_gb: number;
	} = $props();

	const resources = $derived(normalizeMachineResources({ cpu_millis, memory_mb, disk_gb }, sizeOptions));
	const cpuIndex = $derived(Math.max(0, sizeOptions.cpu_millis.indexOf(resources.cpu_millis)));
	const memoryIndex = $derived(Math.max(0, sizeOptions.memory_mb.indexOf(resources.memory_mb)));
	const diskIndex = $derived(Math.max(0, sizeOptions.disk_gb.indexOf(resources.disk_gb)));
	const minimumMemoryIndex = $derived(minimumMemoryIndexForCpu(resources.cpu_millis, sizeOptions.memory_mb));
	const cpuValue = $derived(formatMachineResource(resources.cpu_millis / 1000, m['machines.cpu_unit']()));
	const memoryValue = $derived(formatMachineResource(memoryGb(resources.memory_mb), m['machines.memory_unit']()));
	const diskValue = $derived(formatMachineResource(resources.disk_gb, m['machines.disk_unit']()));
	const cpuRange = $derived(
		m['machines.resource_range']({
			min: sizeOptions.cpu_millis[0] / 1000,
			max: sizeOptions.cpu_millis[sizeOptions.cpu_millis.length - 1] / 1000,
			unit: m['machines.cpu_unit']()
		})
	);
	const memoryRange = $derived(
		m['machines.resource_range']({
			min: memoryGb(sizeOptions.memory_mb[0]),
			max: memoryGb(sizeOptions.memory_mb[sizeOptions.memory_mb.length - 1]),
			unit: m['machines.memory_unit']()
		})
	);
	const diskRange = $derived(
		sizeOptions.disk_gb.length
			? m['machines.resource_range']({
					min: sizeOptions.disk_gb[0],
					max: sizeOptions.disk_gb[sizeOptions.disk_gb.length - 1],
					unit: m['machines.disk_unit']()
				})
			: m['machines.no_disk_options']()
	);
	const memoryFloor = $derived(
		formatMachineResource(
			memoryGb(sizeOptions.memory_mb[minimumMemoryIndex] ?? resources.memory_mb),
			m['machines.memory_unit']()
		)
	);
	const resourceSummary = $derived(
		m['machines.resource_summary']({
			cpu: cpuValue,
			memory: formatMachineResource(memoryGb(resources.memory_mb), m['machines.memory_summary_unit']()),
			disk: formatMachineResource(resources.disk_gb, m['machines.disk_summary_unit']())
		})
	);

	function selectCpu(index: number) {
		const nextCpu = sizeOptions.cpu_millis[index];
		if (nextCpu === undefined) return;
		const currentMemory = resources.memory_mb;
		cpu_millis = nextCpu;
		const floorIndex = minimumMemoryIndexForCpu(nextCpu, sizeOptions.memory_mb);
		const floor = sizeOptions.memory_mb[floorIndex];
		if (floor !== undefined && currentMemory < floor) memory_mb = floor;
	}

	function selectMemory(index: number) {
		const safeIndex = Math.max(index, minimumMemoryIndex);
		const nextMemory = sizeOptions.memory_mb[safeIndex];
		if (nextMemory !== undefined) memory_mb = nextMemory;
	}

	function selectDisk(index: number) {
		const nextDisk = sizeOptions.disk_gb[index];
		if (nextDisk !== undefined) disk_gb = nextDisk;
	}

	// The typed size is applied on commit, snapped to an offered size (2 GB
	// steps). The input has no min/max/step, so a form never refuses to submit.
	function typeDisk(event: Event & { currentTarget: HTMLInputElement }) {
		if (sizeOptions.disk_gb.length === 0) return;
		disk_gb = snapDiskGb(event.currentTarget.valueAsNumber, sizeOptions.disk_gb, resources.disk_gb);
		event.currentTarget.value = String(disk_gb);
	}
</script>

<div class="space-y-4">
	<div class="space-y-4 rounded-lg border border-border p-4">
		<Label>{m['machines.machine_size']()}</Label>
		<div class="space-y-2">
			<div class="flex items-baseline justify-between gap-4">
				<Label>{m['machines.cpu_label']()}</Label>
				<span class="text-sm font-medium tabular-nums">{cpuValue}</span>
			</div>
			<Slider
				type="single"
				min={0}
				max={Math.max(0, sizeOptions.cpu_millis.length - 1)}
				step={1}
				value={cpuIndex}
				onValueChange={selectCpu}
				{disabled}
				aria-label={m['machines.cpu_label']()}
				aria-valuetext={cpuValue}
			/>
			<p class="text-xs text-muted-foreground">{cpuRange}</p>
		</div>
		<div class="space-y-2">
			<div class="flex items-baseline justify-between gap-4">
				<Label>{m['machines.memory_label']()}</Label>
				<span class="text-sm font-medium tabular-nums">{memoryValue}</span>
			</div>
			<Slider
				type="single"
				min={minimumMemoryIndex}
				max={Math.max(minimumMemoryIndex, sizeOptions.memory_mb.length - 1)}
				step={1}
				value={memoryIndex}
				onValueChange={selectMemory}
				{disabled}
				aria-label={m['machines.memory_label']()}
				aria-valuetext={memoryValue}
			/>
			<div class="space-y-1 text-xs text-muted-foreground">
				<p>{memoryRange}</p>
				<p>{m['machines.memory_floor_hint']({ memory: memoryFloor })}</p>
			</div>
		</div>
		<div class="space-y-2">
			<div class="flex items-center justify-between gap-4">
				<Label for="{inputId}-disk">{m['machines.disk_label']()}</Label>
				<div class="flex items-center gap-1.5">
					<Input
						id="{inputId}-disk"
						type="number"
						inputmode="numeric"
						class="h-8 w-24 text-right tabular-nums"
						value={resources.disk_gb}
						onchange={typeDisk}
						disabled={disabled || sizeOptions.disk_gb.length === 0}
					/>
					<span class="text-sm text-muted-foreground">{m['machines.disk_unit']()}</span>
				</div>
			</div>
			<Slider
				type="single"
				min={0}
				max={Math.max(0, sizeOptions.disk_gb.length - 1)}
				step={1}
				value={diskIndex}
				onValueChange={selectDisk}
				disabled={disabled || sizeOptions.disk_gb.length === 0}
				aria-label={m['machines.disk_label']()}
				aria-valuetext={diskValue}
			/>
			<p class={sizeOptions.disk_gb.length ? 'text-xs text-muted-foreground' : 'text-xs text-destructive'}>
				{diskRange}
			</p>
		</div>
	</div>

	<div class="rounded-lg border border-border bg-muted/40 px-4 py-3" aria-live="polite">
		<p class="text-xs font-medium text-muted-foreground">{m['machines.resource_summary_label']()}</p>
		<p class="mt-1 text-sm font-medium">{resourceSummary}</p>
	</div>
</div>
