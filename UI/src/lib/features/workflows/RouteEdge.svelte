<script lang="ts">
	import { BaseEdge, EdgeLabel, type EdgeProps } from '@xyflow/svelte';
	import { polyline, roundedPath, type Route } from './workflow-graph';

	// A transition that runs in the free space between the cards (see routes in
	// workflow-graph): a loop back over the top of the chart, or a drop under
	// its step's row to a lane below or past the steps it skips. A loop's label
	// sits on its arc beside the line rising from its step and a drop's under
	// its step, so each reads with the step it leaves and none share a spot.
	let { id, sourceX, sourceY, targetX, targetY, label, markerEnd, style, data }: EdgeProps = $props();
	const route = $derived(data?.route as Route);
	const width = (key: string) => (typeof data?.[key] === 'number' ? (data[key] as number) : 0);
	// The cards' handles sit at fixed spots (WorkflowNodeCard); the route says
	// where along the edge this transition leaves and enters.
	const points = $derived(
		polyline(
			{
				x:
					route.exit === 'right'
						? sourceX
						: sourceX + (route.from - (route.exit === 'top' ? 0.65 : 0.5)) * width('sourceWidth'),
				y: sourceY
			},
			{ x: route.enter === 'left' ? targetX : targetX + (route.to - 0.35) * width('targetWidth'), y: targetY },
			route
		)
	);
	const labelWidth = $derived(typeof label === 'string' ? label.length * 6 + 14 : 60);
	const labelAt = $derived.by(() => {
		if (route.kind === 'drop') return { x: points[0].x, y: points[0].y + 16 };
		const corner = route.exit === 'right' ? 2 : 1;
		const along = points[corner + 1].x >= points[corner].x ? 1 : -1;
		return { x: points[corner].x + along * (labelWidth / 2 + 10), y: points[corner].y };
	});
</script>

<BaseEdge {id} path={roundedPath(points)} {markerEnd} {style} />
{#if label}<EdgeLabel x={labelAt.x} y={labelAt.y} selectEdgeOnClick>{label}</EdgeLabel>{/if}
