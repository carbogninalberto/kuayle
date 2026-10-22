<script lang="ts">
	import { Button } from '$lib/components/ui/button/index.js';
	import { Calendar } from '$lib/components/ui/calendar';
	import { preferencesState } from '$lib/features/preferences/preferences.state.svelte';
	import { CalendarDate } from '@internationalized/date';
	import type { DateValue } from '@internationalized/date';
	import { CalendarDays, X } from '@lucide/svelte';
	import { m } from '$lib/paraglide/messages.js';
	import { getLocale, setLocale } from '$lib/paraglide/runtime.js';

	let {
		value = null,
		onchange,
		close,
		clearLabel
	}: {
		value: string | null;
		onchange: (date: string | null) => void | Promise<void>;
		close?: () => void;
		clearLabel?: string;
	} = $props();

	const effectiveClearLabel = $derived(clearLabel ?? m['sharedComponents.due_date_picker.no_due_date']());

	const presets = $derived([
		{ label: m['sharedComponents.due_date_picker.today'](), value: formatLocalDate(new Date()) },
		{ label: m['sharedComponents.due_date_picker.tomorrow'](), value: formatLocalDate(addDays(new Date(), 1)) },
		{ label: m['sharedComponents.due_date_picker.next_week'](), value: formatLocalDate(addDays(new Date(), 7)) },
	]);
	const recentDueDates = $derived(preferencesState.recentDueDates.filter((date) => !presets.some((preset) => preset.value === date)));
	const calendarValue = $derived.by(() => {
		if (!value) return undefined;
		const [year, month, day] = value.split('-').map(Number);
		if (!year || !month || !day) return undefined;
		return new CalendarDate(year, month, day);
	});

	async function selectDate(date: string | null) {
		await onchange(date);
		if (date) preferencesState.addRecentDueDate(date);
		close?.();
	}

	function handleCalendarSelect(dateValue: DateValue | undefined) {
		if (!dateValue) return;
		void selectDate(`${dateValue.year}-${String(dateValue.month).padStart(2, '0')}-${String(dateValue.day).padStart(2, '0')}`);
	}

	function addDays(date: Date, days: number) {
		const next = new Date(date);
		next.setDate(next.getDate() + days);
		return next;
	}

	function formatLocalDate(date: Date) {
		return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`;
	}

	function formatDisplayDate(date: string) {
		return new Date(`${date}T00:00:00`).toLocaleDateString(getLocale(), {
			month: 'short',
			day: 'numeric',
			year: 'numeric'
		});
	}
</script>

<div class="mx-auto grid w-fit gap-2 p-2 sm:grid-cols-[10.75rem_max-content]">
	<div class="min-w-0 space-y-2">
		{#if recentDueDates.length > 0}
			<div>
				<div class="px-2 pb-1 text-[10px] font-medium uppercase text-[var(--color-text-tertiary)]">{m['sharedComponents.due_date_picker.recent']()}</div>
				<div class="space-y-0.5">
					{#each recentDueDates as date (date)}
						<Button variant="ghost" size="sm"
							type="button"
							class="h-auto justify-start whitespace-normal flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-xs text-[var(--color-text-secondary)] hover:text-[var(--color-text-primary)]"
							onclick={() => selectDate(date)}
						>
							<CalendarDays size={13} />
							<span class="truncate">{formatDisplayDate(date)}</span>
						</Button>
					{/each}
				</div>
			</div>
		{/if}

		<div>
			<div class="px-2 pb-1 text-[10px] font-medium uppercase text-[var(--color-text-tertiary)]">{m['sharedComponents.due_date_picker.presets']()}</div>
			<div class="space-y-0.5">
				{#each presets as preset (preset.label)}
					<Button variant="ghost" size="sm"
						type="button"
						class="h-auto justify-start whitespace-normal flex w-full items-center justify-between gap-2 rounded-md px-2 py-1.5 text-left text-xs text-[var(--color-text-secondary)] hover:text-[var(--color-text-primary)]"
						onclick={() => selectDate(preset.value)}
					>
						<span>{preset.label}</span>
						<span class="text-[10px] text-[var(--color-text-tertiary)]">{formatDisplayDate(preset.value)}</span>
					</Button>
				{/each}

				<Button variant="ghost" size="sm"
					type="button"
					class="h-auto justify-start whitespace-normal flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-xs text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)]"
					onclick={() => selectDate(null)}
				>
					<X size={13} />
					{effectiveClearLabel}
				</Button>
			</div>
		</div>
	</div>

	<div class="w-max overflow-hidden rounded-xl border border-[var(--app-border)] bg-[var(--color-bg)]/40">
		{#key value}
			<Calendar
				type="single"
				value={calendarValue}
				onValueChange={handleCalendarSelect}
			/>
		{/key}
	</div>
</div>
