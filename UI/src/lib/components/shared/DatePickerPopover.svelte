<script lang="ts">
	import * as Popover from '$lib/components/ui/popover';
	import { Calendar } from '$lib/components/ui/calendar';
	import { CalendarDate } from '@internationalized/date';
	import type { DateValue } from '@internationalized/date';
	import { CalendarIcon, X } from 'lucide-svelte';

	let {
		value = null,
		onchange,
		placeholder = 'Set date'
	}: {
		value: string | null;
		onchange: (date: string | null) => void;
		placeholder?: string;
	} = $props();

	let open = $state(false);

	const calendarValue = $derived.by(() => {
		if (!value) return undefined;
		try {
			const d = new Date(value);
			return new CalendarDate(d.getFullYear(), d.getMonth() + 1, d.getDate());
		} catch {
			return undefined;
		}
	});

	const displayDate = $derived.by(() => {
		if (!value) return null;
		try {
			return new Date(value).toLocaleDateString('en-US', {
				month: 'short',
				day: 'numeric',
				year: 'numeric'
			});
		} catch {
			return null;
		}
	});

	function handleSelect(dateValue: DateValue | undefined) {
		if (dateValue) {
			const date = `${dateValue.year}-${String(dateValue.month).padStart(2, '0')}-${String(dateValue.day).padStart(2, '0')}`;
			onchange(date);
			open = false;
		}
	}

	function handleClear(e: MouseEvent) {
		e.stopPropagation();
		onchange(null);
		open = false;
	}
</script>

<Popover.Root bind:open>
	<Popover.Trigger>
		<button class="flex items-center gap-1.5 rounded-md border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-2.5 py-1 text-xs text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]">
			<CalendarIcon size={12} />
			{#if displayDate}
				{displayDate}
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
		<Calendar
			type="single"
			value={calendarValue}
			onValueChange={handleSelect}
		/>
	</Popover.Content>
</Popover.Root>
