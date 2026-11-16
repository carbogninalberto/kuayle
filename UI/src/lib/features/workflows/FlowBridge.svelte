<script lang="ts" module>
	import type { Viewport, XYPosition } from '@xyflow/svelte';

	/** The parts of Svelte Flow's API the editor's menus and toolbar, and a task's running chart, use. */
	export interface FlowApi {
		toFlow: (client: XYPosition) => XYPosition;
		fit: () => void;
		zoomIn: () => void;
		zoomOut: () => void;
		viewport: () => Viewport;
		/** Moves to the viewport, animated. */
		go: (viewport: Viewport) => void;
		/** Moves the view by this many screen pixels. */
		panBy: (dx: number, dy: number) => void;
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
		fit: () => void flow.fitView({ padding: 0.1, maxZoom: 1, duration: 200 }),
		zoomIn: () => void flow.zoomIn({ duration: 150 }),
		zoomOut: () => void flow.zoomOut({ duration: 150 }),
		viewport: () => flow.getViewport(),
		go: (viewport) => void flow.setViewport(viewport, { duration: 200 }),
		panBy: (dx, dy) => {
			const { x, y, zoom } = flow.getViewport();
			void flow.setViewport({ x: x + dx, y: y + dy, zoom }, { duration: 100 });
		}
	};
</script>
