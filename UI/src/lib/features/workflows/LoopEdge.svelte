<script lang="ts">
	import { BaseEdge, EdgeLabel, getSmoothStepPath, type EdgeProps } from '@xyflow/svelte';

	// A transition back to an earlier step. It leaves and enters at the top and
	// arcs over the path at its own height (longer loops higher), and its label
	// sits on the arc beside the step it leaves, so several loops never share a
	// line or a label spot.
	let {
		id,
		sourceX,
		sourceY,
		targetX,
		targetY,
		sourcePosition,
		targetPosition,
		label,
		markerEnd,
		style,
		data
	}: EdgeProps = $props();
	const offset = $derived(typeof data?.offset === 'number' ? data.offset : 24);
	const path = $derived(
		getSmoothStepPath({
			sourceX,
			sourceY,
			targetX,
			targetY,
			sourcePosition,
			targetPosition,
			offset,
			borderRadius: 8
		})[0]
	);
	// The label's centre, on the arc just past the line rising from the source.
	const labelWidth = $derived(typeof label === 'string' ? label.length * 6 + 14 : 60);
	const labelX = $derived(sourceX + (targetX > sourceX ? 1 : -1) * (labelWidth / 2 + 10));
	const labelY = $derived(Math.min(sourceY, targetY) - offset);
</script>

<BaseEdge {id} {path} {markerEnd} {style} />
{#if label}<EdgeLabel x={labelX} y={labelY} selectEdgeOnClick>{label}</EdgeLabel>{/if}
