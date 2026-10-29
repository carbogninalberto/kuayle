<script lang="ts">
	import { useMessageScroller } from '$lib/components/ui/message-scroller/index.js';

	// Exposes the enclosing MessageScroller's commands to the component that
	// renders the provider (the hook needs to run inside it).
	let { scroller = $bindable() }: { scroller?: ReturnType<typeof useMessageScroller> } = $props();
	scroller = useMessageScroller();
</script>
