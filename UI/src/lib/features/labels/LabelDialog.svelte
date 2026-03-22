<script lang="ts">
	import * as Dialog from '$lib/components/ui/dialog';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';

	let {
		open = $bindable(false),
		mode = 'create',
		initialName = '',
		initialColor = '#6366f1',
		initialDescription = '',
		onsubmit
	}: {
		open: boolean;
		mode?: 'create' | 'edit';
		initialName?: string;
		initialColor?: string;
		initialDescription?: string;
		onsubmit: (data: { name: string; color: string; description?: string }) => void;
	} = $props();

	let name = $state('');
	let color = $state('#6366f1');
	let description = $state('');

	const PRESET_COLORS = [
		'#ef4444', '#f97316', '#eab308', '#22c55e', '#06b6d4',
		'#3b82f6', '#6366f1', '#8b5cf6', '#ec4899', '#6b7280'
	];

	$effect(() => {
		if (open) {
			name = initialName;
			color = initialColor;
			description = initialDescription;
		}
	});

	function handleSubmit(e: Event) {
		e.preventDefault();
		if (!name.trim()) return;
		onsubmit({
			name: name.trim(),
			color,
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
					<h2 class="text-base font-semibold text-[var(--color-text-primary)]">
						{mode === 'create' ? 'Create label' : 'Edit label'}
					</h2>
					<p class="mt-0.5 text-xs text-[var(--color-text-tertiary)]">Labels help categorize and filter issues.</p>
				</div>

				<div class="space-y-1.5">
					<Label class="text-xs text-[var(--color-text-secondary)]">Name</Label>
					<Input
						bind:value={name}
						placeholder="e.g. Bug, Feature, Documentation"
						required
						class="bg-[var(--color-bg)] border-[var(--app-border)] text-[var(--color-text-primary)]"
					/>
				</div>

				<div class="space-y-1.5">
					<Label class="text-xs text-[var(--color-text-secondary)]">Color</Label>
					<div class="flex items-center gap-2">
						<div class="flex gap-1.5">
							{#each PRESET_COLORS as preset}
								<button
									type="button"
									onclick={() => (color = preset)}
									class="h-6 w-6 rounded-full border-2 transition-transform {color === preset ? 'border-white scale-110' : 'border-transparent'}"
									style="background-color: {preset}"
									aria-label="Select color {preset}"
								></button>
							{/each}
						</div>
						<input
							type="color"
							bind:value={color}
							class="h-6 w-6 cursor-pointer rounded border-0 p-0"
						/>
					</div>
				</div>

				<div class="space-y-1.5">
					<Label class="text-xs text-[var(--color-text-secondary)]">Description <span class="text-[var(--color-text-tertiary)]">(optional)</span></Label>
					<Input
						bind:value={description}
						placeholder="What is this label for?"
						class="bg-[var(--color-bg)] border-[var(--app-border)] text-[var(--color-text-primary)]"
					/>
				</div>

				<!-- Preview -->
				<div class="flex items-center gap-2">
					<span class="text-xs text-[var(--color-text-tertiary)]">Preview:</span>
					<span
						class="rounded-full px-2.5 py-0.5 text-xs font-medium"
						style="background-color: {color}20; color: {color}"
					>
						{name || 'Label'}
					</span>
				</div>
			</div>

			<div class="flex justify-end gap-2 border-t border-[var(--app-border)] px-5 py-3">
				<Button variant="outline" size="sm" type="button" onclick={() => (open = false)}>Cancel</Button>
				<Button size="sm" type="submit" disabled={!name.trim()}>
					{mode === 'create' ? 'Create label' : 'Save changes'}
				</Button>
			</div>
		</form>
	</Dialog.Content>
</Dialog.Root>
