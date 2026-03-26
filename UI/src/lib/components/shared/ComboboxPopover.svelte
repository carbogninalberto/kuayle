<script lang="ts">
	import * as Popover from '$lib/components/ui/popover/index.js';
	import * as Command from '$lib/components/ui/command/index.js';
	import type { Snippet } from 'svelte';

	let {
		open = $bindable(false),
		placeholder = 'Search...',
		emptyMessage = 'No results.',
		width = 'w-48',
		align = 'start' as 'start' | 'center' | 'end',
		showSearch = true,
		trigger,
		children,
	}: {
		open?: boolean;
		placeholder?: string;
		emptyMessage?: string;
		width?: string;
		align?: 'start' | 'center' | 'end';
		showSearch?: boolean;
		trigger: Snippet;
		children: Snippet;
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
	<Popover.Content class="{width} p-0" {align}>
		<Command.Root class="rounded-lg shadow-none ring-0" shouldFilter={showSearch}>
			{#if showSearch}
				<Command.Input bind:ref={inputRef} bind:value={searchValue} {placeholder} />
			{/if}
			<Command.List>
				<Command.Empty>{emptyMessage}</Command.Empty>
				{@render children()}
			</Command.List>
		</Command.Root>
	</Popover.Content>
</Popover.Root>
