<script lang="ts">
	import * as Dialog from '$lib/components/ui/dialog';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import SelectField from '$lib/components/shared/SelectField.svelte';
	import type { Project } from '$lib/types/project';
	import { m } from '$lib/paraglide/messages.js';
	import { getLocale, setLocale } from '$lib/paraglide/runtime.js';

	let {
		open = $bindable(false),
		mode = 'create',
		initialName = '',
		initialColor = '#6366f1',
		initialDescription = '',
		initialProjectId = null,
		projects = [],
		onsubmit
	}: {
		open: boolean;
		mode?: 'create' | 'edit';
		initialName?: string;
		initialColor?: string;
		initialDescription?: string;
		/** The label's project; null = workspace-wide. Fixed once the label exists. */
		initialProjectId?: string | null;
		/** When given, create mode offers a project to scope the label to. */
		projects?: Project[];
		onsubmit: (data: { name: string; color: string; description?: string; project_id?: string }) => void;
	} = $props();

	let name = $state('');
	let color = $state('#6366f1');
	let description = $state('');
	let projectId = $state<string | null>(null);
	/** Scoped to a project the user cannot list: shown honestly instead of as "All projects". */
	let unknownScope = $derived(projectId !== null && !projects.some((p) => p.id === projectId));
	let scopeOptions = $derived([
		{ value: 'workspace', label: m['settings.labels.scope_workspace']() },
		...projects.map((p) => ({ value: p.id, label: p.name })),
		...(unknownScope && projectId ? [{ value: projectId, label: m['settings.labels.other_projects'](), disabled: true }] : [])
	]);

	const PRESET_COLORS = [
		'#ef4444', '#f97316', '#eab308', '#22c55e', '#06b6d4',
		'#3b82f6', '#6366f1', '#8b5cf6', '#ec4899', '#6b7280'
	];

	$effect(() => {
		if (open) {
			name = initialName;
			color = initialColor;
			description = initialDescription;
			projectId = initialProjectId;
		}
	});

	function handleSubmit(e: Event) {
		e.preventDefault();
		if (!name.trim()) return;
		onsubmit({
			name: name.trim(),
			color,
			description: description.trim() || undefined,
			project_id: mode === 'create' ? (projectId ?? undefined) : undefined
		});
		open = false;
	}
</script>

<Dialog.Root bind:open>
	<Dialog.Content class="sm:max-w-[420px] border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-0 overflow-hidden rounded-xl">
		<form onsubmit={handleSubmit}>
			<div class="px-5 pt-5 pb-4 space-y-4">
				<div>
					<Dialog.Title class="text-base font-semibold leading-normal text-[var(--color-text-primary)]">
						{mode === 'create' ? 'Create label' : 'Edit label'}
					</Dialog.Title>
					<p class="mt-0.5 text-xs text-[var(--color-text-tertiary)]">Labels help categorize and filter issues.</p>
				</div>

				<div class="space-y-1.5">
					<Label class="text-xs text-[var(--color-text-secondary)]">Name</Label>
					<Input
						bind:value={name}
						placeholder="e.g. Bug, Feature, Documentation"
						required
						class=""
					/>
				</div>

				{#if projects.length > 0}
					<div class="space-y-1.5">
						<Label class="text-xs text-[var(--color-text-secondary)]">{m['settings.labels.scope']()}</Label>
						<SelectField
							aria-label={m['settings.labels.scope']()}
							class="w-full rounded-md border border-[var(--app-border)] bg-[var(--color-bg)] px-3 py-2 text-sm text-[var(--color-text-primary)] outline-none"
							disabled={mode === 'edit'}
							bind:value={() => projectId ?? 'workspace', (value) => (projectId = value === 'workspace' ? null : value)}
							options={scopeOptions}
						/>
						<p class="text-xs text-[var(--color-text-tertiary)]">{m['settings.labels.scope_hint']()}</p>
					</div>
				{/if}

				<div class="space-y-1.5">
					<Label class="text-xs text-[var(--color-text-secondary)]">Color</Label>
					<div class="flex items-center gap-2">
						<div class="flex gap-1.5">
							{#each PRESET_COLORS as preset}
								<Button variant="plain" type="button"
									onclick={() => (color = preset)}
									class="h-6 w-6 rounded-full border-2 transition-transform {color === preset ? 'border-white scale-110' : 'border-transparent'}"
									style="background-color: {preset}"
									aria-label="Select color {preset}"
								></Button>
							{/each}
						</div>
						<Input variant="plain"
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
						class=""
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
