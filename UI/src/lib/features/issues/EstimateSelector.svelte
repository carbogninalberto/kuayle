<script lang="ts">
	import type { EstimateScale } from '$lib/types/team';
	import * as Popover from '$lib/components/ui/popover';

	let {
		scale,
		value = $bindable<number | null>(null),
		onchange
	}: {
		scale: EstimateScale;
		value?: number | null;
		onchange?: (value: number | null) => void;
	} = $props();

	let open = $state(false);

	const SCALE_OPTIONS: Record<EstimateScale, { label: string; value: number }[]> = {
		linear: [
			{ label: '0', value: 0 },
			{ label: '1', value: 1 },
			{ label: '2', value: 2 },
			{ label: '3', value: 3 },
			{ label: '4', value: 4 },
			{ label: '5', value: 5 }
		],
		exponential: [
			{ label: '1', value: 1 },
			{ label: '2', value: 2 },
			{ label: '4', value: 4 },
			{ label: '8', value: 8 },
			{ label: '16', value: 16 }
		],
		fibonacci: [
			{ label: '1', value: 1 },
			{ label: '2', value: 2 },
			{ label: '3', value: 3 },
			{ label: '5', value: 5 },
			{ label: '8', value: 8 },
			{ label: '13', value: 13 },
			{ label: '21', value: 21 }
		],
		tshirt: [
			{ label: 'XS', value: 1 },
			{ label: 'S', value: 2 },
			{ label: 'M', value: 3 },
			{ label: 'L', value: 4 },
			{ label: 'XL', value: 5 }
		]
	};

	let options = $derived(SCALE_OPTIONS[scale] ?? SCALE_OPTIONS.linear);
	let displayLabel = $derived(
		value !== null && value !== undefined
			? options.find((o) => o.value === value)?.label ?? String(value)
			: 'No estimate'
	);

	function selectValue(v: number | null) {
		value = v;
		onchange?.(v);
		open = false;
	}
</script>

<Popover.Root bind:open>
	<Popover.Trigger>
		<button class="flex h-8 items-center gap-1.5 rounded-md border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-2 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)]">
			{displayLabel}
		</button>
	</Popover.Trigger>
	<Popover.Content class="w-36 p-1" align="start">
		<button
			onclick={() => selectValue(null)}
			class="flex w-full items-center rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] {value === null ? 'bg-[var(--color-bg-hover)]' : ''}"
		>
			No estimate
		</button>
		{#each options as option}
			<button
				onclick={() => selectValue(option.value)}
				class="flex w-full items-center rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] {value === option.value ? 'bg-[var(--color-bg-hover)]' : ''}"
			>
				{option.label}
			</button>
		{/each}
	</Popover.Content>
</Popover.Root>
