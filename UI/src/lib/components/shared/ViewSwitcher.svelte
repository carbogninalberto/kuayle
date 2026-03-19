<script lang="ts">
	import * as ToggleGroup from '$lib/components/ui/toggle-group';
	import { List, LayoutGrid } from 'lucide-svelte';
	import type { ViewLayout } from '$lib/types/view';

	let {
		layout = $bindable<ViewLayout>('list'),
		onchange
	}: {
		layout: ViewLayout;
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
	value={layout}
	onValueChange={handleChange}
	variant="outline"
	size="sm"
>
	<ToggleGroup.Item value="list" aria-label="List view">
		<List size={14} />
	</ToggleGroup.Item>
	<ToggleGroup.Item value="board" aria-label="Board view">
		<LayoutGrid size={14} />
	</ToggleGroup.Item>
</ToggleGroup.Root>
