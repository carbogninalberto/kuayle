<script lang="ts">
	import { BaseEdge, EdgeLabel, getSmoothStepPath, type EdgeProps } from '@xyflow/svelte';

	// A transition back to an earlier step. It leaves and enters on the left at
	// its own distance (longer loops further out), and its label sits next to
	// the step it leaves, so several loops never share a line or a label spot.
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
	// The label's centre, just left of the loop's vertical line.
	const labelWidth = $derived(typeof label === 'string' ? label.length * 6 + 14 : 60);
	const labelX = $derived(Math.min(sourceX, targetX) - offset - labelWidth / 2 - 4);
</script>

<BaseEdge {id} {path} {markerEnd} {style} />
{#if label}<EdgeLabel x={labelX} y={sourceY} selectEdgeOnClick>{label}</EdgeLabel>{/if}
