<script lang="ts">
	import { Checkbox } from '$lib/components/ui/checkbox/index.js';
	import * as Dialog from '$lib/components/ui/dialog';
	import { Button } from '$lib/components/ui/button';
	import { Label } from '$lib/components/ui/label';
	import { Textarea } from '$lib/components/ui/textarea';
	import type { Cycle } from '$lib/types/cycle';
	import { m } from '$lib/paraglide/messages.js';
	import { getLocale, setLocale } from '$lib/paraglide/runtime.js';

	let {
		open = $bindable(false),
		cycle,
		incompleteCount = 0,
		nextUpcomingCycle = null,
		onsubmit
	}: {
		open: boolean;
		cycle: Cycle | null;
		incompleteCount: number;
		nextUpcomingCycle: Cycle | null;
		onsubmit: (data: { retrospective?: string; carry_over: boolean }) => void;
	} = $props();

	let retrospective = $state('');
	let carryOver = $state(false);

	$effect(() => {
		if (open) {
			retrospective = '';
			carryOver = false;
		}
	});

	function handleSubmit(e: Event) {
		e.preventDefault();
		onsubmit({
			retrospective: retrospective.trim() || undefined,
			carry_over: carryOver
		});
		open = false;
	}
</script>

<Dialog.Root bind:open>
	<Dialog.Content class="sm:max-w-[420px] border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-0 overflow-hidden rounded-xl">
		<form onsubmit={handleSubmit}>
			<div class="px-5 pt-5 pb-4 space-y-4">
				<div>
					<h2 class="text-base font-semibold text-[var(--color-text-primary)]">{m['cycles.complete.title']()}</h2>
					<p class="mt-0.5 text-xs text-[var(--color-text-tertiary)]">
						{#if cycle}{m['cycles.complete.mark_prefix']()}<strong>{cycle.name}</strong>{m['cycles.complete.mark_suffix']()}{/if}
					</p>
				</div>

				{#if cycle?.progress}
					<div class="rounded-md border border-[var(--app-border)] bg-[var(--color-bg)] px-3 py-2 text-xs text-[var(--color-text-secondary)]">
						<div class="flex justify-between">
							<span>{m['cycles.status.completed']()}</span>
							<span class="text-[var(--color-text-primary)]">{cycle.progress.completed} of {cycle.progress.total}</span>
						</div>
						{#if incompleteCount > 0}
							<div class="flex justify-between mt-1">
								<span>{m['cycles.incomplete']()}</span>
								<span class="text-[var(--color-text-primary)]">{incompleteCount}</span>
							</div>
						{/if}
					</div>
				{/if}

				{#if incompleteCount > 0 && nextUpcomingCycle}
					<label class="flex items-start gap-2 cursor-pointer">
						<Checkbox

							bind:checked={carryOver}
							class="mt-0.5"
						/>
						<span class="text-xs text-[var(--color-text-secondary)]">
							{m['cycles.complete.carry_over_prefix']()}{incompleteCount}{incompleteCount > 1 ? m['cycles.complete.incomplete_to']() : m['cycles.complete.incomplete_to']()}{m['cycles.complete.to']()}<strong>{nextUpcomingCycle.name}</strong>
						</span>
					</label>
				{:else if incompleteCount > 0}
					<p class="text-xs text-[var(--color-text-tertiary)]">
						{incompleteCount}{incompleteCount > 1 ? m['cycles.complete.incomplete_to']() : m['cycles.complete.incomplete_to']()}{m['cycles.complete.no_upcoming_suffix']()}
					</p>
				{/if}

				<div class="space-y-1.5">
					<Label class="text-xs text-[var(--color-text-secondary)]">{m['cycles.retrospective']()} <span class="text-[var(--color-text-tertiary)]">{m['cycles.field.optional']()}</span></Label>
					<Textarea
						bind:value={retrospective}
						placeholder={m['cycles.edit.retrospective_placeholder']()}
						rows={3}
						class="resize-none"
					/>
				</div>
			</div>

			<div class="flex justify-end gap-2 border-t border-[var(--app-border)] px-5 py-3">
				<Button variant="outline" size="sm" type="button" onclick={() => (open = false)}>{m['common.cancel']()}</Button>
				<Button size="sm" type="submit">{m['cycles.complete.title']()}</Button>
			</div>
		</form>
	</Dialog.Content>
</Dialog.Root>
