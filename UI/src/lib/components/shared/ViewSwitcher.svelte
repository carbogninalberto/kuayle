<script lang="ts">
	import * as ToggleGroup from '$lib/components/ui/toggle-group';
	import { List, LayoutGrid } from '@lucide/svelte';
	import type { ViewLayout } from '$lib/types/view';
	import { m } from '$lib/paraglide/messages.js';
	import { getLocale, setLocale } from '$lib/paraglide/runtime.js';

	let {
		layout = $bindable<ViewLayout>('list'),
		onchange,
		inactive = false
	}: {
		layout: ViewLayout;
		inactive?: boolean;
		onchange?: (layout: ViewLayout) => void;
	} = $props();

	function handleChange(value: string | undefined) {
		if (value && (value === 'list' || value === 'board')) {
			layout = value;
			onchange?.(value);
		}
	}
</script>

<ToggleGroup.Root
	type="single"
	value={inactive ? undefined : layout}
	onValueChange={handleChange}
	variant="outline"
	size="sm"
>
	<ToggleGroup.Item value="list" aria-label={m['sharedComponents.view_switcher.list_view']()}>
		<List size={14} />
	</ToggleGroup.Item>
	<ToggleGroup.Item value="board" aria-label={m['sharedComponents.view_switcher.board_view']()}>
		<LayoutGrid size={14} />
	</ToggleGroup.Item>
</ToggleGroup.Root>
