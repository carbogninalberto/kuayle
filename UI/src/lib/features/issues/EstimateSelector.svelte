<script lang="ts">
	import type { EstimateScale } from '$lib/types/team';

	let {
		scale,
		value = $bindable<number | null>(null),
		onchange
	}: {
		scale: EstimateScale;
		value?: number | null;
		onchange?: (value: number | null) => void;
	} = $props();

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

	let selectValue = $derived(value !== null && value !== undefined ? String(value) : '');

	function handleChange(e: Event) {
		const target = e.target as HTMLSelectElement;
		if (target.value === '') {
			value = null;
			onchange?.(null);
		} else {
			const numValue = Number(target.value);
			value = numValue;
			onchange?.(numValue);
		}
	}
</script>

<select
	value={selectValue}
	onchange={handleChange}
	class="h-8 rounded-md border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-2 text-sm text-[var(--color-text-primary)] outline-none focus:border-[var(--app-accent)]"
>
	<option value="">No estimate</option>
	{#each options as option}
		<option value={String(option.value)}>{option.label}</option>
	{/each}
</select>
