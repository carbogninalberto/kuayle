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
		onsubmit: (data: { name: string; key: string; description?: string }) => void;
	} = $props();

	let name = $state('');
	let key = $state('');
	let description = $state('');
	let keyManuallyEdited = $state(false);

	$effect(() => {
		if (open) {
			name = '';
			key = '';
			description = '';
			keyManuallyEdited = false;
		}
	});

	// Auto-generate key from name (first 3 chars uppercase)
	$effect(() => {
		if (!keyManuallyEdited && name) {
			key = name
				.replace(/[^a-zA-Z]/g, '')
				.slice(0, 3)
				.toUpperCase();
		}
	});

	function handleSubmit(e: Event) {
		e.preventDefault();
		if (!name.trim() || !key.trim()) return;
		onsubmit({
			name: name.trim(),
			key: key.trim().toUpperCase(),
			description: description.trim() || undefined
		});
		open = false;
	}
</script>

<Dialog.Root bind:open>
	<Dialog.Content class="sm:max-w-[420px] border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-0 overflow-hidden rounded-xl">
		<form onsubmit={handleSubmit}>
			<div class="px-5 pt-5 pb-4 space-y-4">
				<div>
					<h2 class="text-base font-semibold text-[var(--color-text-primary)]">Create team</h2>
					<p class="mt-0.5 text-xs text-[var(--color-text-tertiary)]">Teams organize issues and members.</p>
				</div>

				<div class="space-y-1.5">
					<Label class="text-xs text-[var(--color-text-secondary)]">Name</Label>
					<Input
						bind:value={name}
						placeholder="e.g. Engineering"
						required
						class="bg-[var(--color-bg)] border-[var(--app-border)] text-[var(--color-text-primary)]"
					/>
				</div>

				<div class="space-y-1.5">
					<Label class="text-xs text-[var(--color-text-secondary)]">Identifier</Label>
					<Input
						bind:value={key}
						placeholder="e.g. ENG"
						required
						maxlength={10}
						oninput={() => (keyManuallyEdited = true)}
						class="bg-[var(--color-bg)] border-[var(--app-border)] text-[var(--color-text-primary)] uppercase"
					/>
					<p class="text-[10px] text-[var(--color-text-tertiary)]">Used as issue prefix (e.g. ENG-123)</p>
				</div>

				<div class="space-y-1.5">
					<Label class="text-xs text-[var(--color-text-secondary)]">Description <span class="text-[var(--color-text-tertiary)]">(optional)</span></Label>
					<Input
						bind:value={description}
						placeholder="What does this team work on?"
						class="bg-[var(--color-bg)] border-[var(--app-border)] text-[var(--color-text-primary)]"
					/>
				</div>
			</div>

			<div class="flex justify-end gap-2 border-t border-[var(--app-border)] px-5 py-3">
				<Button variant="outline" size="sm" type="button" onclick={() => (open = false)}>Cancel</Button>
				<Button size="sm" type="submit" disabled={!name.trim() || !key.trim()}>Create team</Button>
			</div>
		</form>
	</Dialog.Content>
</Dialog.Root>
