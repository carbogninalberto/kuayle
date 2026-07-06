<script lang="ts">
	import * as Dialog from '$lib/components/ui/dialog';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Checkbox } from '$lib/components/ui/checkbox';
	import { createView } from '$lib/api/views';
	import type { ViewFilter } from '$lib/types/view';
	import { toast } from 'svelte-sonner';

	let {
		open = $bindable(false),
		filters,
		slug
	}: {
		open: boolean;
		filters: ViewFilter;
		slug: string;
	} = $props();

	let name = $state('');
	let description = $state('');
	let isShared = $state(false);

	$effect(() => {
		if (open) {
			name = '';
			description = '';
			isShared = false;
		}
	});

	async function handleSubmit(e: Event) {
		e.preventDefault();
		if (!name.trim()) return;
		try {
			await createView(slug, {
				name: name.trim(),
				description: description.trim() || undefined,
				filters,
				is_shared: isShared
			});
			toast.success('View saved');
			open = false;
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to save view');
		}
	}
</script>

<Dialog.Root bind:open>
	<Dialog.Content class="sm:max-w-[420px] border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-0 overflow-hidden rounded-xl">
		<form onsubmit={handleSubmit}>
			<div class="px-5 pt-5 pb-4 space-y-4">
				<div>
					<h2 class="text-base font-semibold text-[var(--color-text-primary)]">Save view</h2>
					<p class="mt-0.5 text-xs text-[var(--color-text-tertiary)]">Save the current filters as a reusable view.</p>
				</div>

				<div class="space-y-1.5">
					<Label class="text-xs text-[var(--color-text-secondary)]">Name</Label>
					<Input
						bind:value={name}
						placeholder="e.g. Active bugs"
						required
						class="bg-[var(--color-bg)] border-[var(--app-border)] text-[var(--color-text-primary)]"
					/>
				</div>

				<div class="space-y-1.5">
					<Label class="text-xs text-[var(--color-text-secondary)]">Description <span class="text-[var(--color-text-tertiary)]">(optional)</span></Label>
					<Input
						bind:value={description}
						placeholder="What does this view show?"
						class="bg-[var(--color-bg)] border-[var(--app-border)] text-[var(--color-text-primary)]"
					/>
				</div>

				<div class="flex items-center gap-2">
					<Checkbox bind:checked={isShared} />
					<span class="text-xs text-[var(--color-text-secondary)]">Share with workspace</span>
				</div>
			</div>

			<div class="flex justify-end gap-2 border-t border-[var(--app-border)] px-5 py-3">
				<Button variant="outline" size="sm" type="button" onclick={() => (open = false)}>Cancel</Button>
				<Button size="sm" type="submit" disabled={!name.trim()}>Save view</Button>
			</div>
		</form>
	</Dialog.Content>
</Dialog.Root>
