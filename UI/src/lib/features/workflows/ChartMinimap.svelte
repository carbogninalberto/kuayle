<script lang="ts">
	import type { WorkflowGraph } from '$lib/api/workflows';
	import { cn } from '$lib/utils.js';
	import { nodeAccent } from './node-style';
	import { extent, NODE_HEIGHT, nodeWidth } from './workflow-graph';

	// The minimap of a chart drawn at a fixed zoom (a task's running chart):
	// the whole graph and the part of it its scroll area shows. Svelte Flow's
	// own minimap cannot do this: there the flow's viewport is the whole graph
	// and the page's scroll is the real view. A picture only: nothing in it
	// takes focus or pointer input, so the scroll area stays the one control.
	// Not a shadcn-covered control, hence plain SVG.
	let {
		graph,
		run,
		view,
		label,
		class: className
	}: {
		graph: WorkflowGraph;
		/** The run's current step and the steps it entered. */
		run: { current: string; visited: Set<string> };
		/** The part shown, in graph coordinates (minimapWindow). */
		view: { x: number; y: number; width: number; height: number };
		label: string;
		class?: string;
	} = $props();

	const WIDTH = 160;
	const box = $derived(extent(graph));
	// A little room around the graph, as in Svelte Flow's minimap.
	const margin = $derived(Math.max(box.width, box.height) * 0.04);
	const frame = $derived({
		x: box.x - margin,
		y: box.y - margin,
		width: box.width + 2 * margin,
		height: box.height + 2 * margin
	});
	// As tall as the graph's shape asks, within readable bounds.
	const height = $derived(Math.round(Math.min(100, Math.max(28, (WIDTH * frame.height) / Math.max(frame.width, 1)))));
	// Graph units per pixel, for corners that stay round at any size.
	const unit = $derived(Math.max(frame.width / WIDTH, frame.height / height));
</script>

<div
	class={cn('pointer-events-none overflow-hidden rounded-lg border bg-background', className)}
	data-testid="task-workflow-minimap"
>
	<svg
		class="block"
		width={WIDTH}
		{height}
		viewBox="{frame.x} {frame.y} {frame.width} {frame.height}"
		role="img"
		aria-label={label}
	>
		{#each graph.nodes as node (node.id)}
			{@const current = run.current === node.id}
			{@const visited = run.visited.has(node.id)}
			<rect
				x={node.position.x}
				y={node.position.y}
				width={nodeWidth(node.type)}
				height={NODE_HEIGHT}
				rx={5 * unit}
				style:fill={nodeAccent(node)}
				opacity={current || visited ? 1 : 0.35}
				style:stroke={current ? 'var(--primary)' : 'none'}
				stroke-width={current ? 3 : 0}
				vector-effect="non-scaling-stroke"
				data-testid="minimap-node-{node.id}"
				data-current={current || undefined}
				data-visited={visited || undefined}
			/>
		{/each}
		<!-- Everything outside the shown part is dimmed, as in Svelte Flow's minimap. -->
		<path
			d="M{frame.x},{frame.y}h{frame.width}v{frame.height}h{-frame.width}z M{view.x},{view.y}h{view.width}v{view.height}h{-view.width}z"
			fill-rule="evenodd"
			style:fill="color-mix(in oklab, var(--muted-foreground) 18%, transparent)"
		/>
		<rect
			x={view.x}
			y={view.y}
			width={view.width}
			height={view.height}
			fill="none"
			style:stroke="var(--ring)"
			stroke-width="1"
			vector-effect="non-scaling-stroke"
			data-testid="task-workflow-minimap-view"
		/>
	</svg>
</div>
