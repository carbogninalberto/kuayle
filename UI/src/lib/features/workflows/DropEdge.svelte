<script lang="ts">
	import { BaseEdge, EdgeLabel, getSmoothStepPath, type EdgeProps } from '@xyflow/svelte';

	// A transition to a step in a lane below (a failure). It drops from the
	// step it leaves and then runs along that lane, where transitions from
	// other steps join it; its label sits on the drop, under its own step, so
	// each one reads with the step it belongs to.
	let { id, sourceX, sourceY, targetX, targetY, sourcePosition, targetPosition, label, markerEnd, style }: EdgeProps =
		$props();
	const path = $derived(
		getSmoothStepPath({ sourceX, sourceY, targetX, targetY, sourcePosition, targetPosition, borderRadius: 8 })[0]
	);
</script>

<BaseEdge {id} {path} {markerEnd} {style} />
{#if label}<EdgeLabel x={sourceX} y={sourceY + Math.min(30, (targetY - sourceY) / 2)} selectEdgeOnClick
		>{label}</EdgeLabel
	>{/if}
