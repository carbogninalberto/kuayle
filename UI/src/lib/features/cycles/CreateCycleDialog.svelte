<script lang="ts">
	import * as Dialog from '$lib/components/ui/dialog';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';

	let {
		open = $bindable(false),
		onsubmit
	}: {
		open: boolean;
		onsubmit: (data: { name: string; description?: string; start_date?: string; end_date?: string }) => void;
	} = $props();

	let name = $state('');
	let description = $state('');
	let startDate = $state('');
	let endDate = $state('');

	$effect(() => {
		if (open) {
			name = '';
			description = '';
			startDate = '';
			endDate = '';
		}
	});

	function handleSubmit(e: Event) {
		e.preventDefault();
		if (!name.trim()) return;
		onsubmit({
			name: name.trim(),
			description: description.trim() || undefined,
			start_date: startDate || undefined,
			end_date: endDate || undefined
		});
		open = false;
	}
</script>

<Dialog.Root bind:open>
	<Dialog.Content class="sm:max-w-[420px] border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-0 overflow-hidden rounded-xl">
		<form onsubmit={handleSubmit}>
			<div class="px-5 pt-5 pb-4 space-y-4">
				<div>
					<h2 class="text-base font-semibold text-[var(--color-text-primary)]">Create cycle</h2>
					<p class="mt-0.5 text-xs text-[var(--color-text-tertiary)]">Cycles help you plan work in time-boxed iterations.</p>
				</div>

				<div class="space-y-1.5">
					<Label class="text-xs text-[var(--color-text-secondary)]">Name</Label>
					<Input
						bind:value={name}
						placeholder="e.g. Sprint 1"
						required
						class="bg-[var(--color-bg)] border-[var(--app-border)] text-[var(--color-text-primary)]"
					/>
				</div>

				<div class="space-y-1.5">
					<Label class="text-xs text-[var(--color-text-secondary)]">Description <span class="text-[var(--color-text-tertiary)]">(optional)</span></Label>
					<Input
						bind:value={description}
						placeholder="What's the goal for this cycle?"
						class="bg-[var(--color-bg)] border-[var(--app-border)] text-[var(--color-text-primary)]"
					/>
				</div>

				<div class="grid grid-cols-2 gap-3">
					<div class="space-y-1.5">
						<Label class="text-xs text-[var(--color-text-secondary)]">Start date</Label>
						<Input
							type="date"
							bind:value={startDate}
							class="bg-[var(--color-bg)] border-[var(--app-border)] text-[var(--color-text-primary)]"
						/>
					</div>
					<div class="space-y-1.5">
						<Label class="text-xs text-[var(--color-text-secondary)]">End date</Label>
						<Input
							type="date"
							bind:value={endDate}
							class="bg-[var(--color-bg)] border-[var(--app-border)] text-[var(--color-text-primary)]"
						/>
					</div>
				</div>
			</div>

			<div class="flex justify-end gap-2 border-t border-[var(--app-border)] px-5 py-3">
				<Button variant="outline" size="sm" type="button" onclick={() => (open = false)}>Cancel</Button>
				<Button size="sm" type="submit" disabled={!name.trim()}>Create cycle</Button>
			</div>
		</form>
	</Dialog.Content>
</Dialog.Root>
