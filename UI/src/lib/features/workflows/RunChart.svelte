<script lang="ts">
	import { LocateFixed, Maximize2, Scan, ZoomIn, ZoomOut } from '@lucide/svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Kbd } from '$lib/components/ui/kbd/index.js';
	import * as Tooltip from '$lib/components/ui/tooltip/index.js';
	import type { AgentProfile, PipelineDefinition, WorkflowGraph } from '$lib/api/workflows';
	import { m } from '$lib/features/i18n/messages';
	import { cn } from '$lib/utils.js';
	import WorkflowCanvas from './WorkflowCanvas.svelte';
	import type { FlowApi } from './FlowBridge.svelte';
	import { phaseCaption } from './pipeline-labels';
	import { centreOn, isCentred, type runOverlay } from './workflow-graph';

	// A task's workflow on a canvas its viewer pans and zooms, with
	// tools to zoom, fit the whole graph, centre the current step and (inline)
	// expand it. It follows the run: each new step, including its end, is centred once;
	// moving around in between is the viewer's, and nothing snaps it back.
	// Its keys work while focus is in the chart, never page-wide (the chat
	// input and the editor have their own).
	let {
		graph,
		pipeline = null,
		profiles,
		run,
		label,
		zoom,
		minimap = true,
		testid,
		onexpand,
		class: className
	}: {
		graph: WorkflowGraph;
		pipeline?: PipelineDefinition | null;
		profiles: AgentProfile[];
		run: ReturnType<typeof runOverlay>;
		label: string;
		/** The zoom it opens at. */
		zoom: number;
		minimap?: boolean;
		testid?: string;
		/** Shows an Expand tool that calls this. */
		onexpand?: () => void;
		/** Its height. */
		class?: string;
	} = $props();

	let api = $state<FlowApi>();
	let view = $state<{ x: number; y: number; zoom: number; width: number; height: number }>();

	// Centring needs a step the graph has (a run may name none yet).
	const focus = $derived(graph.nodes.some((n) => n.id === run.focus) ? run.focus : null);
	const centred = $derived(!!focus && !!view && isCentred(graph, focus, view, view));

	function centre() {
		if (!api || !view || !focus) return;
		const at = centreOn(graph, focus, view, view.zoom);
		if (at) api.go(at);
	}

	// Once per step: the canvas opens centred on the step the run is at, and
	// each later step is centred when it comes (a hidden chart, with no size,
	// waits until it shows). Resizes and the viewer's own moves never re-centre.
	let followed: string | null | undefined;
	$effect(() => {
		if (!api || !view) return;
		if (followed === undefined) followed = focus;
		else if (focus && focus !== followed) {
			centre();
			followed = focus;
		}
	});

	const PAN = 80;
	const keys: Record<string, (() => void) | undefined> = {
		c: () => centre(),
		f: () => api?.fit(),
		'0': () => api?.fit(),
		'+': () => api?.zoomIn(),
		'=': () => api?.zoomIn(),
		'-': () => api?.zoomOut(),
		ArrowLeft: () => api?.panBy(PAN, 0),
		ArrowRight: () => api?.panBy(-PAN, 0),
		ArrowUp: () => api?.panBy(0, PAN),
		ArrowDown: () => api?.panBy(0, -PAN)
	};
	function onkeydown(event: KeyboardEvent) {
		if (event.ctrlKey || event.metaKey || event.altKey || event.defaultPrevented) return;
		if (event.target instanceof HTMLElement && event.target.closest('input, textarea, [contenteditable="true"]'))
			return;
		const action = keys[event.key.length === 1 ? event.key.toLowerCase() : event.key];
		if (!action) return;
		event.preventDefault();
		action();
	}
</script>

<!-- The chart is reachable from the keyboard so that its keys can move it. -->
<!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions -->
<div
	class={cn('workflow-tools-layout relative rounded-lg outline-none focus-visible:ring-2 focus-visible:ring-ring', className)}
	role="group"
	aria-label={label}
	aria-keyshortcuts="C F 0 + - ArrowLeft ArrowRight ArrowUp ArrowDown"
	tabindex="0"
	data-testid={testid}
	{onkeydown}
>
	<WorkflowCanvas
		{graph}
		{profiles}
		{run}
		{label}
		{minimap}
		describe={pipeline ? (node) => phaseCaption(pipeline, node.id) : undefined}
		edgeLabels={!pipeline}
		navigable={zoom}
		class="h-full"
		bind:api
		bind:view
	/>

	<Tooltip.Provider delayDuration={300}>
		<div
			class="mobile-touch-targets absolute top-2 right-2 z-10 flex items-center gap-0.5 rounded-lg border bg-background/95 p-0.5 shadow-sm backdrop-blur"
			role="toolbar"
			aria-label={m['workflows.task.chart_tools']()}
		>
			{#snippet tool(text: string, Icon: typeof ZoomIn, action: () => void, shortcut: string, disabled = false)}
				<Tooltip.Root>
					<Tooltip.Trigger>
						{#snippet child({ props })}<Button
								{...props}
								size="icon-sm"
								variant="ghost"
								aria-label={text}
								aria-keyshortcuts={shortcut || undefined}
								{disabled}
								onclick={action}><Icon /></Button
							>{/snippet}
					</Tooltip.Trigger>
					<Tooltip.Content side="bottom"
						>{text}{#if shortcut}<Kbd>{shortcut}</Kbd>{/if}</Tooltip.Content
					>
				</Tooltip.Root>
			{/snippet}
			{@render tool(m['workflows.editor.zoom_out'](), ZoomOut, () => api?.zoomOut(), '-')}
			{@render tool(m['workflows.editor.zoom_in'](), ZoomIn, () => api?.zoomIn(), '+')}
			{@render tool(m['workflows.editor.fit_view'](), Scan, () => api?.fit(), 'F')}
			{#if run.current && focus}{@render tool(m['workflows.task.centre_current'](), LocateFixed, centre, 'C', centred)}{/if}
			{#if onexpand}
				<Tooltip.Root>
					<Tooltip.Trigger>
						{#snippet child({ props })}<Button
								{...props}
								size="icon-sm"
								variant="ghost"
								aria-label={m['workflows.task.expand']()}
								aria-haspopup="dialog"
								onclick={onexpand}><Maximize2 /></Button
							>{/snippet}
					</Tooltip.Trigger>
					<Tooltip.Content side="bottom">{m['workflows.task.expand']()}</Tooltip.Content>
				</Tooltip.Root>
			{/if}
		</div>
	</Tooltip.Provider>
</div>
