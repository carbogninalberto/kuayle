<script lang="ts">
	import type { Snippet } from 'svelte';
	import { Info } from '@lucide/svelte';
	import * as Popover from '$lib/components/ui/popover/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { m } from '$lib/features/i18n/messages';

	// A small (i) next to a title that opens its explanation, so long help
	// text stays out of the way until someone wants it.
	let {
		text,
		label = m['common.more_info'](),
		children
	}: { text?: string; label?: string; children?: Snippet } = $props();
</script>

<Popover.Root>
	<Popover.Trigger>
		{#snippet child({ props })}<Button
				{...props}
				size="icon-xs"
				variant="ghost"
				class="size-5 text-muted-foreground hover:text-foreground"
				aria-label={label}><Info class="size-3.5" /></Button
			>{/snippet}
	</Popover.Trigger>
	<Popover.Content align="start" class="w-80 text-xs leading-relaxed text-muted-foreground">
		{#if children}{@render children()}{:else}{text}{/if}
	</Popover.Content>
</Popover.Root>
