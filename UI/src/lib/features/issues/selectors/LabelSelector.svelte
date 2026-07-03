<script lang="ts">
	import ComboboxPopover from '$lib/components/shared/ComboboxPopover.svelte';
	import * as Command from '$lib/components/ui/command/index.js';
	import { Checkbox } from '$lib/components/ui/checkbox';
	import { createLabel } from '$lib/api/labels';
	import type { Label } from '$lib/types/label';
	import type { Snippet } from 'svelte';
	import { Plus } from 'lucide-svelte';
	import { toast } from 'svelte-sonner';

	let {
		open = $bindable(false),
		labels,
		value = [],
		onchange,
		trigger,
		width = 'w-48',
		align = 'start' as 'start' | 'center' | 'end',
		shortcutKey,
		slug,
		oncreated,
	}: {
		open?: boolean;
		labels: Label[];
		value: string[];
		onchange: (labelId: string) => void;
		trigger: Snippet;
		width?: string;
		align?: 'start' | 'center' | 'end';
		shortcutKey?: string;
		slug?: string;
		oncreated?: (label: Label) => void;
	} = $props();

	let creating = $state(false);

	async function handleCreate(name: string) {
		if (!slug || creating) return;
		creating = true;
		try {
			const label = await createLabel(slug, { name, color: '#6366f1' });
			oncreated?.(label);
			onchange(label.id);
			open = false;
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to create label');
		} finally {
			creating = false;
		}
	}
</script>

<ComboboxPopover bind:open placeholder="Search labels..." emptyMessage="No labels." {width} {align} {shortcutKey} {trigger}>
	{#snippet children(searchValue: string)}
		{@const labelName = searchValue.trim()}
		{@const canCreate = slug && labelName && !labels.some((label) => label.name.toLowerCase() === labelName.toLowerCase())}
		{#if canCreate}
			<Command.Item
				value={labelName}
				onSelect={() => handleCreate(labelName)}
				class="flex items-center gap-2"
			>
				<Plus size={14} />
				<span class="truncate">{creating ? 'Creating...' : `Create label "${labelName}"`}</span>
			</Command.Item>
		{/if}
		{#each labels as label (label.id)}
		{@const isSelected = value.includes(label.id)}
		<Command.Item
			value={label.name}
			onSelect={() => onchange(label.id)}
			class="flex items-center gap-2"
		>
			<Checkbox checked={isSelected} />
			<div class="h-2.5 w-2.5 rounded-full shrink-0" style="background-color: {label.color}"></div>
			<span class="truncate">{label.name}</span>
		</Command.Item>
		{/each}
	{/snippet}
</ComboboxPopover>
