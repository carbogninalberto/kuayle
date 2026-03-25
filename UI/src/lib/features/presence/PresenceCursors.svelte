<script lang="ts">
	import { presenceState } from './presence.state.svelte';

	let { containerRef }: { containerRef?: HTMLElement } = $props();
</script>

{#if containerRef}
	{#each presenceState.activeViewers.filter((v) => v.cursor) as viewer (viewer.user_id)}
		{@const x = (viewer.cursor?.x ?? 0) * containerRef.clientWidth}
		{@const y = (viewer.cursor?.y ?? 0) * containerRef.clientHeight}
		<div
			class="pointer-events-none absolute z-50 transition-all duration-75 ease-out"
			style="left: {x}px; top: {y}px;"
		>
			<svg
				width="16"
				height="20"
				viewBox="0 0 16 20"
				fill="none"
				class="drop-shadow-sm"
			>
				<path
					d="M0 0L16 12L8 12L4 20L0 0Z"
					fill={viewer.color}
				/>
			</svg>
			<div
				class="ml-3 -mt-1 whitespace-nowrap rounded px-1.5 py-0.5 text-[10px] font-medium text-white shadow-sm"
				style="background-color: {viewer.color};"
			>
				{viewer.name}
			</div>
		</div>
	{/each}
{/if}
