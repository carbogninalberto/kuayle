<script lang="ts">
	import { ChevronsUpDown, RefreshCw } from '@lucide/svelte';
	import * as Command from '$lib/components/ui/command/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import ComboboxPopover from '$lib/components/shared/ComboboxPopover.svelte';
	import type { CatalogModel } from '$lib/types/dev-machine';
	import { m } from '$lib/features/i18n/messages';
	import { filterModels, typedChoice, type CatalogStatus } from './model-catalog';

	// A model field with suggestions: the catalog's models, filtered by id and
	// name as the user types. It stays free text: what was typed can always be
	// used as it is, and an empty value leaves the model to the agent. Built on
	// the shared combobox (Popover + Command), which owns keyboard navigation
	// and focus return.
	let {
		id,
		value,
		models,
		status,
		placeholder,
		disabled = false,
		onselect,
		onrefresh,
		...triggerProps
	}: {
		id: string;
		value: string;
		models: CatalogModel[];
		status: CatalogStatus;
		/** Shown while no model is set, and as the choice that clears it. */
		placeholder: string;
		disabled?: boolean;
		onselect: (model: string) => void;
		onrefresh: () => void;
		'aria-describedby'?: string;
	} = $props();

	let open = $state(false);
	const statusText = $derived(
		status === 'loading'
			? m['profiles.models_loading']()
			: status === 'unavailable'
				? m['profiles.models_unavailable']()
				: status === 'stale'
					? m['profiles.models_stale']()
					: ''
	);

	function choose(model: string) {
		open = false;
		if (model !== value) onselect(model);
	}
</script>

<ComboboxPopover
	bind:open
	filter={false}
	width="w-[min(26rem,calc(100vw-2rem))]"
	placeholder={m['profiles.model_search']()}
	emptyMessage={status === 'loading' ? m['profiles.models_loading']() : m['profiles.models_none']()}
>
	{#snippet trigger(props)}
		<Button
			{...props}
			{...triggerProps}
			{id}
			type="button"
			variant="outline"
			role="combobox"
			aria-expanded={open}
			{disabled}
			class="w-full justify-between gap-2 px-2.5 font-mono font-normal"
			data-testid="model-combobox"
		>
			<span class="truncate {value ? '' : 'font-sans text-muted-foreground'}">{value || placeholder}</span>
			<ChevronsUpDown class="size-4 shrink-0 opacity-50" />
		</Button>
	{/snippet}
	{#snippet children(search)}
		{@const typed = typedChoice(models, search)}
		{#if value && !search.trim()}
			<Command.Item value="default:" onSelect={() => choose('')}>
				<span class="text-xs">{placeholder}</span>
			</Command.Item>
		{/if}
		{#each filterModels(models, search) as model (model.id)}
			<Command.Item value={`model:${model.id}`} data-checked={model.id === value} onSelect={() => choose(model.id)}>
				<span class="min-w-0">
					<span class="block truncate font-mono text-xs">{model.id}</span>
					{#if model.name && model.name !== model.id}
						<span class="block truncate text-xs text-muted-foreground">{model.name}</span>
					{/if}
				</span>
			</Command.Item>
		{/each}
		{#if typed}
			<Command.Item value={`typed:${typed}`} data-checked={typed === value} onSelect={() => choose(typed)}>
				<span class="min-w-0 truncate text-xs">{m['profiles.model_use_typed']({ model: typed })}</span>
			</Command.Item>
		{/if}
	{/snippet}
	{#snippet footer()}
		<div class="flex items-center justify-between gap-2 px-1.5 pt-1.5">
			<p role="status" class="min-w-0 text-xs text-muted-foreground" data-testid="model-combobox-status">{statusText}</p>
			<Button
				type="button"
				variant="ghost"
				size="sm"
				class="shrink-0"
				disabled={status === 'loading'}
				onclick={onrefresh}
			>
				<RefreshCw class="size-3.5 {status === 'loading' ? 'animate-spin' : ''}" />{m['common.refresh']()}
			</Button>
		</div>
	{/snippet}
</ComboboxPopover>
