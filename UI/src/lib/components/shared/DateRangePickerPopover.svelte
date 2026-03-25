<script lang="ts">
	import * as Popover from '$lib/components/ui/popover';
	import { RangeCalendar } from '$lib/components/ui/range-calendar';
	import { CalendarDate } from '@internationalized/date';
	import type { DateValue } from '@internationalized/date';
	import { CalendarIcon, X } from 'lucide-svelte';

	let {
		startDate = null,
		endDate = null,
		onchange,
		isDateDisabled,
		numberOfMonths = 2,
		placeholder = 'Select dates'
	}: {
		startDate: string | null;
		endDate: string | null;
		onchange: (start: string, end: string) => void;
		isDateDisabled?: (date: DateValue) => boolean;
		numberOfMonths?: number;
		placeholder?: string;
	} = $props();

	let open = $state(false);

	const calendarValue = $derived.by(() => {
		const start = startDate ? parseDate(startDate) : undefined;
		const end = endDate ? parseDate(endDate) : undefined;
		if (!start && !end) return undefined;
		return { start, end };
	});

	function parseDate(value: string): CalendarDate | undefined {
		try {
			const d = new Date(value);
			return new CalendarDate(d.getFullYear(), d.getMonth() + 1, d.getDate());
		} catch {
			return undefined;
		}
	}

	function formatDate(value: string): string {
		try {
			return new Date(value).toLocaleDateString('en-US', {
				month: 'short',
				day: 'numeric',
				year: 'numeric'
			});
		} catch {
			return value;
		}
	}

	const displayText = $derived.by(() => {
		if (startDate && endDate) {
			return `${formatDate(startDate)} – ${formatDate(endDate)}`;
		}
		if (startDate) return formatDate(startDate);
		return null;
	});

	function handleValueChange(range: { start: DateValue | undefined; end: DateValue | undefined } | undefined) {
		if (range?.start && range?.end) {
			const start = `${range.start.year}-${String(range.start.month).padStart(2, '0')}-${String(range.start.day).padStart(2, '0')}`;
			const end = `${range.end.year}-${String(range.end.month).padStart(2, '0')}-${String(range.end.day).padStart(2, '0')}`;
			onchange(start, end);
			open = false;
		}
	}

	function handleClear(e: MouseEvent) {
		e.stopPropagation();
		// Reset to empty — parent handles the state
		open = false;
	}
</script>

<Popover.Root bind:open>
	<Popover.Trigger>
		<button class="flex items-center gap-1.5 rounded-md border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-2.5 py-1 text-xs hover:bg-[var(--color-bg-hover)] {displayText ? 'text-[var(--color-text-primary)]' : 'text-[var(--color-text-secondary)]'}">
			<CalendarIcon size={12} />
			{#if displayText}
				{displayText}
				<span
					onclick={handleClear}
					onkeydown={(e) => { if (e.key === 'Enter') handleClear(e as unknown as MouseEvent); }}
					role="button"
					tabindex={0}
					class="ml-1 inline-flex rounded p-0.5 hover:bg-[var(--color-bg-hover)]"
				>
					<X size={10} />
				</span>
			{:else}
				{placeholder}
			{/if}
		</button>
	</Popover.Trigger>
	<Popover.Content class="w-auto p-0" align="start">
		{#key `${startDate}-${endDate}`}
			<RangeCalendar
				value={calendarValue}
				onValueChange={handleValueChange}
				{numberOfMonths}
				{isDateDisabled}
			/>
		{/key}
	</Popover.Content>
</Popover.Root>
