<script lang="ts" module>
	import type { XYPosition } from '@xyflow/svelte';

	/** The parts of Svelte Flow's API the editor's menus and toolbar use. */
	export interface FlowApi {
		toFlow: (client: XYPosition) => XYPosition;
		fit: () => void;
		zoomIn: () => void;
		zoomOut: () => void;
	}
</script>

<script lang="ts">
	import { useSvelteFlow } from '@xyflow/svelte';

	// Rendered inside <SvelteFlow>, where its API is available, and hands it
	// to the editor outside.
	let { api = $bindable() }: { api?: FlowApi } = $props();
	const flow = useSvelteFlow();
	api = {
		toFlow: (client) => flow.screenToFlowPosition(client),
		fit: () => void flow.fitView({ padding: 0.25, maxZoom: 1, duration: 200 }),
		zoomIn: () => void flow.zoomIn({ duration: 150 }),
		zoomOut: () => void flow.zoomOut({ duration: 150 })
	};
</script>
