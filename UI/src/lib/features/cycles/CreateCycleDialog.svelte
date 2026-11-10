<script lang="ts">
	import * as Dialog from '$lib/components/ui/dialog';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Textarea } from '$lib/components/ui/textarea';
	import DateRangePickerPopover from '$lib/components/shared/DateRangePickerPopover.svelte';
	import type { Cycle } from '$lib/types/cycle';
	import type { DateValue } from '@internationalized/date';
	import { m, getLocale, setLocale } from '$lib/features/i18n/messages';

	let {
		open = $bindable(false),
		cycles = [],
		nextNumber = 1,
		onsubmit
	}: {
		open: boolean;
		cycles: Cycle[];
		nextNumber: number;
		onsubmit: (data: { name: string; description?: string; goals?: string; start_date: string; end_date: string }) => void;
	} = $props();

	let name = $state('');
	let description = $state('');
	let goals = $state('');
	let startDate = $state('');
	let endDate = $state('');

	$effect(() => {
		if (open) {
			name = m['cycles.title']() + ' ' + nextNumber;
			description = '';
			goals = '';
			startDate = '';
			endDate = '';
		}
	});

	function isDateDisabled(date: DateValue): boolean {
		const d = `${date.year}-${String(date.month).padStart(2, '0')}-${String(date.day).padStart(2, '0')}`;
		return cycles
			.filter((c) => c.status !== 'completed')
			.some((c) => {
				if (!c.start_date || !c.end_date) return false;
				return d >= c.start_date.slice(0, 10) && d <= c.end_date.slice(0, 10);
			});
	}

	function handleSubmit(e: Event) {
		e.preventDefault();
		if (!name.trim() || !startDate || !endDate) return;
		onsubmit({
			name: name.trim(),
			description: description.trim() || undefined,
			goals: goals.trim() || undefined,
			start_date: startDate,
			end_date: endDate
		});
		open = false;
	}
</script>

<Dialog.Root bind:open>
	<Dialog.Content class="sm:max-w-[420px] border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-0 overflow-hidden rounded-xl">
		<form onsubmit={handleSubmit}>
			<div class="px-5 pt-5 pb-4 space-y-4">
				<div>
					<h2 class="text-base font-semibold text-[var(--color-text-primary)]">{m['cycles.create.title']()}</h2>
					<p class="mt-0.5 text-xs text-[var(--color-text-tertiary)]">{m['cycles.create.description']()}</p>
				</div>

				<div class="space-y-1.5">
					<Label class="text-xs text-[var(--color-text-secondary)]">{m['cycles.field.name']()}</Label>
					<Input
						bind:value={name}
						placeholder={m['cycles.create.name_placeholder']()}
						required
						class="bg-[var(--color-bg)] border-[var(--app-border)] text-[var(--color-text-primary)]"
					/>
				</div>

				<div class="space-y-1.5">
					<Label class="text-xs text-[var(--color-text-secondary)]">{m['cycles.field.description']()} <span class="text-[var(--color-text-tertiary)]">{m['cycles.field.optional']()}</span></Label>
					<Input
						bind:value={description}
						placeholder={m['cycles.create.description_placeholder']()}
						class="bg-[var(--color-bg)] border-[var(--app-border)] text-[var(--color-text-primary)]"
					/>
				</div>

				<div class="space-y-1.5">
					<Label class="text-xs text-[var(--color-text-secondary)]">{m['cycles.goals']()} <span class="text-[var(--color-text-tertiary)]">{m['cycles.field.optional']()}</span></Label>
					<Textarea
						bind:value={goals}
						placeholder={m['cycles.create.goals_placeholder']()}
						rows={2}
						class="bg-[var(--color-bg)] border-[var(--app-border)] text-[var(--color-text-primary)] resize-none text-sm"
					/>
				</div>

				<div class="space-y-1.5">
					<Label class="text-xs text-[var(--color-text-secondary)]">{m['cycles.field.date_range']()}</Label>
					<DateRangePickerPopover
						startDate={startDate || null}
						endDate={endDate || null}
						onchange={(s, e) => { startDate = s; endDate = e; }}
						{isDateDisabled}
						placeholder={m['cycles.select_date_range']()}
					/>
				</div>
			</div>

			<div class="flex justify-end gap-2 border-t border-[var(--app-border)] px-5 py-3">
				<Button variant="outline" size="sm" type="button" onclick={() => (open = false)}>{m['common.cancel']()}</Button>
				<Button size="sm" type="submit" disabled={!name.trim() || !startDate || !endDate}>{m['cycles.create.title']()}</Button>
			</div>
		</form>
	</Dialog.Content>
</Dialog.Root>
