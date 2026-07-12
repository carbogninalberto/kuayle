<script lang="ts">
	import { CalendarDate } from '@internationalized/date';
	import type { DateValue } from '@internationalized/date';
	import { CalendarRange, X } from 'lucide-svelte';
	import { Button } from '$lib/components/ui/button';
	import * as Popover from '$lib/components/ui/popover';
	import { RangeCalendar } from '$lib/components/ui/range-calendar';

	let {
		startDate = '',
		endDate = '',
		onchange,
		allowClear = false
	}: {
		startDate: string;
		endDate: string;
		onchange: (start: string, end: string) => void;
		allowClear?: boolean;
	} = $props();

	let open = $state(false);

	function parseDate(value: string): CalendarDate | undefined {
		const [year, month, day] = value.split('-').map(Number);
		if (!year || !month || !day) return undefined;
		return new CalendarDate(year, month, day);
	}

	function serializeDate(value: DateValue): string {
		return `${value.year}-${String(value.month).padStart(2, '0')}-${String(value.day).padStart(2, '0')}`;
	}

	function formatDate(value: string): string {
		const parsed = parseDate(value);
		if (!parsed) return value;
		return new Date(parsed.year, parsed.month - 1, parsed.day).toLocaleDateString(undefined, {
			month: 'short',
			day: 'numeric',
			year: 'numeric'
		});
	}

	const calendarValue = $derived.by(() => {
		const start = parseDate(startDate);
		const end = parseDate(endDate);
		return start || end ? { start, end } : undefined;
	});

	const displayText = $derived(
		startDate && endDate ? `${formatDate(startDate)} - ${formatDate(endDate)}` : 'Select date range'
	);

	function handleValueChange(range: { start: DateValue | undefined; end: DateValue | undefined } | undefined) {
		if (!range?.start || !range.end) return;
		onchange(serializeDate(range.start), serializeDate(range.end));
		open = false;
	}

	function applyPreset(days: number) {
		const end = new Date();
		end.setHours(12, 0, 0, 0);
		const start = new Date(end);
		start.setDate(start.getDate() - days + 1);
		const serialize = (date: Date) =>
			`${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`;
		onchange(serialize(start), serialize(end));
		open = false;
	}
</script>

<Popover.Root bind:open>
	<Popover.Trigger>
		{#snippet child({ props })}
			<Button {...props} variant="outline" size="sm" aria-label="Date range" class="min-w-[220px] justify-start font-normal">
				<CalendarRange data-icon="inline-start" />
				<span class="truncate">{displayText}</span>
			</Button>
		{/snippet}
	</Popover.Trigger>
	<Popover.Content align="start" class="w-auto p-0">
		<div class="flex items-center gap-1 border-b border-[var(--app-border)] p-2">
			<Button variant="ghost" size="xs" onclick={() => applyPreset(30)}>30 days</Button>
			<Button variant="ghost" size="xs" onclick={() => applyPreset(90)}>90 days</Button>
			<Button variant="ghost" size="xs" onclick={() => applyPreset(180)}>6 months</Button>
			{#if allowClear && (startDate || endDate)}
				<Button variant="ghost" size="xs" class="ml-auto" onclick={() => { onchange('', ''); open = false; }}>
					<X data-icon="inline-start" />
					Clear
				</Button>
			{/if}
		</div>
		{#key `${startDate}-${endDate}`}
			<RangeCalendar value={calendarValue} onValueChange={handleValueChange} numberOfMonths={2} />
		{/key}
	</Popover.Content>
</Popover.Root>
