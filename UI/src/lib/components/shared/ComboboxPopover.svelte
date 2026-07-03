<script lang="ts">
	import { Command as CommandPrimitive } from 'bits-ui';
	import * as Popover from '$lib/components/ui/popover/index.js';
	import type { Snippet } from 'svelte';

	let {
		open = $bindable(false),
		placeholder = 'Search...',
		emptyMessage = 'No results.',
		width = 'w-48',
		align = 'start' as 'start' | 'center' | 'end',
		showSearch = true,
		shortcutKey,
		trigger,
		children,
	}: {
		open?: boolean;
		placeholder?: string;
		emptyMessage?: string;
		width?: string;
		align?: 'start' | 'center' | 'end';
		showSearch?: boolean;
		shortcutKey?: string;
		trigger: Snippet;
		children: Snippet<[string]>;
	} = $props();

	let searchValue = $state('');
	let inputRef = $state<HTMLInputElement | null>(null);

	$effect(() => {
		if (open && showSearch) {
			requestAnimationFrame(() => inputRef?.focus());
		}
		if (!open) {
			searchValue = '';
		}
	});
</script>

<Popover.Root bind:open>
	<Popover.Trigger>
		{@render trigger()}
	</Popover.Trigger>
	<Popover.Content class="{width} p-1.5" {align}>
		<CommandPrimitive.Root class="flex size-full flex-col overflow-hidden" shouldFilter={showSearch}>
			{#if showSearch}
				<div class="flex items-center gap-2 px-1.5 pb-1.5">
					<CommandPrimitive.Input
						data-slot="command-input"
						class="w-full bg-transparent text-sm text-[var(--color-text-primary)] outline-none placeholder:text-[var(--color-text-tertiary)]"
						bind:ref={inputRef}
						bind:value={searchValue}
						{placeholder}
					/>
					{#if shortcutKey}
						<kbd class="shrink-0 rounded border border-[var(--app-border)] bg-[var(--color-bg-tertiary)] px-1.5 py-0.5 text-[10px] font-medium text-[var(--color-text-tertiary)]">
							{shortcutKey}
						</kbd>
					{/if}
				</div>
				<div class="h-px bg-[var(--app-border)] -mx-1.5 mb-1"></div>
			{:else if shortcutKey}
				<div class="flex justify-end px-1.5 pb-1">
					<kbd class="shrink-0 rounded border border-[var(--app-border)] bg-[var(--color-bg-tertiary)] px-1.5 py-0.5 text-[10px] font-medium text-[var(--color-text-tertiary)]">
						{shortcutKey}
					</kbd>
				</div>
			{/if}
			<CommandPrimitive.List class="no-scrollbar max-h-72 scroll-py-1 outline-none overflow-x-hidden overflow-y-auto space-y-0.5">
				<CommandPrimitive.Empty class="py-4 text-center text-xs text-[var(--color-text-tertiary)]">{emptyMessage}</CommandPrimitive.Empty>
				{@render children(searchValue)}
			</CommandPrimitive.List>
		</CommandPrimitive.Root>
	</Popover.Content>
</Popover.Root>
