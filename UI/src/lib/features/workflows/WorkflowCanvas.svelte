<script lang="ts">
	import { untrack } from 'svelte';
	import '@xyflow/svelte/dist/style.css';
	import {
		SvelteFlow,
		Background,
		BackgroundVariant,
		Controls,
		MarkerType,
		type Connection,
		type Edge,
		type EdgeTypes,
		type NodeTypes
	} from '@xyflow/svelte';
	import type { AgentProfile, WorkflowGraph, WorkflowIssue } from '$lib/api/workflows';
	import { m } from '$lib/features/i18n/messages';
	import WorkflowNodeCard, { type WorkflowFlowNode } from './WorkflowNodeCard.svelte';
	import FlowBridge, { type FlowApi } from './FlowBridge.svelte';
	import LoopEdge from './LoopEdge.svelte';
	import DropEdge from './DropEdge.svelte';
	import type { WorkflowNodeType } from '$lib/api/workflows';
	import { conditionLabel, issueLabel, profileTitle } from './labels';
	import {
		dropsDown,
		extent,
		layering,
		loopOffsets,
		opening,
		placeIssues,
		type Block,
		type FiredTransition
	} from './workflow-graph';

	// The workflow graph on a Svelte Flow canvas, editable or read-only. The
	// graph prop stays the source of truth: the canvas reports moves,
	// connections and deletions, and redraws from the graph it is given.
	export type Selection = { kind: 'node' | 'edge'; id: string } | null;

	let {
		graph,
		profiles,
		issues = [],
		editable = false,
		selection = null,
		run,
		label,
		class: className = 'h-[520px]',
		controls = true,
		zoom,
		api = $bindable(),
		onselect,
		onaddnext,
		oncontext,
		onmove,
		onconnect,
		ondelete
	}: {
		graph: WorkflowGraph;
		profiles: AgentProfile[];
		issues?: WorkflowIssue[];
		editable?: boolean;
		selection?: Selection;
		/** A task's run: its current node, the nodes it entered, visit counts and fired transitions. */
		run?: {
			current: string;
			visited: Set<string>;
			visits: Record<string, number>;
			fired: Map<string, FiredTransition>;
		};
		label: string;
		class?: string;
		/** Svelte Flow's own zoom controls; the editor has its own toolbar. */
		controls?: boolean;
		/**
		 * Draws the whole graph at this zoom in a canvas of exactly its size, with
		 * no panning or zooming of its own: the page around it scrolls instead.
		 */
		zoom?: number;
		/** Svelte Flow's API, for the editor's menus and toolbar. */
		api?: FlowApi;
		onselect?: (selection: Selection) => void;
		/** Adds a step after the given one (its hover "+" button). */
		onaddnext?: (source: string, type: Block) => void;
		/** A right click on a step or a transition, before the menu opens. */
		oncontext?: (target: Selection) => void;
		onmove?: (id: string, position: { x: number; y: number }) => void;
		onconnect?: (source: string, target: string) => void;
		ondelete?: (nodes: string[], edges: string[]) => void;
	} = $props();

	const nodeTypes: NodeTypes = { workflow: WorkflowNodeCard };
	const edgeTypes: EdgeTypes = { loop: LoopEdge, drop: DropEdge };

	function subtitle(node: WorkflowGraph['nodes'][number]): string {
		switch (node.type) {
			case 'agent': {
				const profile = profiles.find((p) => p.id === node.profile_id);
				return profile ? profileTitle(profile) : '';
			}
			case 'check':
				return node.check === 'ci' ? m['workflows.editor.check_ci']() : m['workflows.editor.check_tests']();
			case 'end':
				return node.outcome === 'failed'
					? m['workflows.editor.outcome_failed']()
					: m['workflows.editor.outcome_done']();
			case 'approval':
				return node.prompt ?? '';
		}
		return '';
	}

	let nodes = $state.raw<WorkflowFlowNode[]>([]);
	let edges = $state.raw<Edge[]>([]);
	// Bumped to redraw from the graph when the canvas changed itself (a
	// connection the graph did not accept).
	let redraw = $state(0);

	$effect(() => {
		void redraw;
		const placed = placeIssues(issues);
		nodes = graph.nodes.map((node) => ({
			id: node.id,
			type: 'workflow',
			position: node.position,
			selected: selection?.kind === 'node' && selection.id === node.id,
			deletable: editable && node.type !== 'start',
			connectable: editable,
			ariaLabel: `${node.label}`,
			data: {
				node,
				subtitle: subtitle(node),
				problems: (placed.nodes.get(node.id) ?? []).map((issue) => issueLabel(issue.code, issue.message)),
				current: run?.current === node.id,
				visited: run?.visited.has(node.id),
				visits: run ? (run.visits[node.id] ?? 0) : undefined,
				// A step's transitions back to itself (retries) show on its card.
				retries: graph.edges
					.filter((e) => e.source === node.id && e.target === node.id)
					.map((e) => conditionLabel(e.condition)),
				onaddnext: editable && onaddnext && node.type !== 'end' ? (type: Block) => onaddnext(node.id, type) : undefined
			}
		}));
		// Loops back to an earlier step arc over the top, each at its own height,
		// and transitions to a lane below (failures) leave downwards, so neither
		// crosses the main path; transitions between the same two steps share
		// one label.
		const { back } = layering(graph);
		const loopOffset = loopOffsets(graph);
		const pairs = new Map<string, string[]>();
		for (const edge of graph.edges) {
			const key = `${edge.source}>${edge.target}`;
			pairs.set(key, [...(pairs.get(key) ?? []), edge.id]);
		}
		const pairLabel = (edge: WorkflowGraph['edges'][number]) => {
			const ids = pairs.get(`${edge.source}>${edge.target}`) ?? [];
			if (ids[0] !== edge.id) return undefined;
			return ids.map((id) => conditionLabel(graph.edges.find((e) => e.id === id)?.condition ?? '')).join(' / ');
		};
		edges = graph.edges.map((edge) => {
			const fired = run?.fired.get(edge.id);
			const loop = back.has(edge.id);
			const self = edge.source === edge.target;
			const source = graph.nodes.find((n) => n.id === edge.source);
			const target = graph.nodes.find((n) => n.id === edge.target);
			const down = !loop && !!source && !!target && dropsDown(source, target);
			const problem = placed.edges.has(edge.id);
			return {
				id: edge.id,
				type: 'smoothstep',
				source: edge.source,
				target: edge.target,
				...(loop
					? {
							type: 'loop',
							sourceHandle: 'loop-out',
							targetHandle: 'loop-in',
							data: { offset: loopOffset.get(edge.id) }
						}
					: {}),
				...(down ? { type: 'drop', sourceHandle: 'side-out' } : {}),
				label: fired && fired.count > 1 ? `${conditionLabel(edge.condition)} ×${fired.count}` : pairLabel(edge),
				selected: selection?.kind === 'edge' && selection.id === edge.id,
				hidden: self,
				deletable: editable,
				animated: !!fired && run?.current === edge.target,
				markerEnd: { type: MarkerType.ArrowClosed },
				class: problem
					? 'workflow-edge-problem'
					: fired
						? 'workflow-edge-fired'
						: run
							? 'workflow-edge-idle'
							: undefined,
				ariaLabel: fired ? `${conditionLabel(edge.condition)}: ${fired.reason}` : conditionLabel(edge.condition)
			};
		});
	});

	// Where the canvas opens, once its size is known: Svelte Flow reads the
	// viewport only when it starts.
	// (TaskWorkflowView scrolls to a step knowing this padding.)
	const PADDING = 24;
	const box = $derived(zoom ? extent(graph) : undefined);
	let width = $state(0),
		height = $state(0);
	const viewport = $derived(
		zoom && box
			? { x: PADDING - box.x * zoom, y: PADDING - box.y * zoom, zoom }
			: width > 0 && height > 0
				? opening(graph, { width, height })
				: undefined
	);

	function connected(connection: Connection) {
		onconnect?.(connection.source, connection.target);
		redraw++;
	}
</script>

<div
	class="workflow-canvas min-h-0 overflow-hidden {zoom ? 'shrink-0' : 'w-full rounded-lg border bg-muted/20'} {className}"
	style:width={zoom && box ? `${Math.ceil(box.width * zoom) + 2 * PADDING}px` : undefined}
	style:height={zoom && box ? `${Math.ceil(box.height * zoom) + 2 * PADDING}px` : undefined}
	role="region"
	aria-label={label}
	bind:clientWidth={width}
	bind:clientHeight={height}
>
	{#if viewport}<SvelteFlow
		bind:nodes
		bind:edges
		{nodeTypes}
		{edgeTypes}
		initialViewport={untrack(() => viewport)}
		panOnDrag={!zoom}
		zoomOnScroll={!zoom}
		zoomOnPinch={!zoom}
		zoomOnDoubleClick={!zoom}
		preventScrolling={!zoom}
		minZoom={0.1}
		maxZoom={1.5}
		nodesDraggable={editable}
		nodesConnectable={editable}
		elementsSelectable
		deleteKey={editable ? ['Backspace', 'Delete'] : null}
		proOptions={{ hideAttribution: true }}
		onnodeclick={({ node }) => onselect?.({ kind: 'node', id: node.id })}
		onedgeclick={({ edge }) => onselect?.({ kind: 'edge', id: edge.id })}
		onpaneclick={() => onselect?.(null)}
		onnodecontextmenu={({ node }) => oncontext?.({ kind: 'node', id: node.id })}
		onedgecontextmenu={({ edge }) => oncontext?.({ kind: 'edge', id: edge.id })}
		onnodedragstop={({ targetNode }) => targetNode && onmove?.(targetNode.id, targetNode.position)}
		onconnect={connected}
		onbeforedelete={async ({ nodes: deleting }) => editable && !deleting.some((n) => n.data.node.type === 'start')}
		ondelete={({ nodes: removed, edges: removedEdges }) =>
			ondelete?.(
				removed.map((n) => n.id),
				removedEdges.map((e) => e.id)
			)}
	>
		{#if !zoom}<Background variant={BackgroundVariant.Dots} gap={18} size={1.2} />{/if}
		{#if controls && !zoom}<Controls showLock={false} />{/if}
		<FlowBridge bind:api />
	</SvelteFlow>{/if}
</div>

<style>
	.workflow-canvas :global(.svelte-flow) {
		--xy-background-color: transparent;
		--xy-edge-stroke: color-mix(in oklab, var(--muted-foreground) 55%, transparent);
		--xy-edge-stroke-width: 1.5;
		--xy-background-pattern-color: color-mix(in oklab, var(--muted-foreground) 25%, transparent);
		--xy-edge-stroke-selected: var(--ring);
		--xy-edge-label-background-color: var(--background);
		--xy-edge-label-color: var(--foreground);
		--xy-controls-button-background-color: var(--background);
		--xy-controls-button-background-color-hover: var(--muted);
		--xy-controls-button-color: var(--foreground);
		--xy-controls-button-border-color: var(--border);
		--xy-node-border-radius: 0.5rem;
		--xy-handle-background-color: var(--muted-foreground);
	}
	.workflow-canvas :global(.svelte-flow__node-workflow) {
		background: transparent;
		border: none;
		padding: 0;
	}
	.workflow-canvas :global(.workflow-edge-problem .svelte-flow__edge-path) {
		stroke: var(--destructive);
		stroke-width: 2;
	}
	.workflow-canvas :global(.workflow-edge-fired .svelte-flow__edge-path) {
		stroke: var(--primary);
		stroke-width: 2;
	}
	.workflow-canvas :global(.workflow-edge-idle .svelte-flow__edge-path) {
		stroke-dasharray: 4 4;
		opacity: 0.6;
	}
	.workflow-canvas :global(.svelte-flow__edge-label) {
		padding: 1px 6px;
		border: 1px solid var(--border);
		border-radius: 9999px;
		background: var(--background);
		color: var(--muted-foreground);
		font-size: 11px;
		line-height: 16px;
		white-space: nowrap;
	}
	.workflow-canvas :global(.svelte-flow__edge.selected .svelte-flow__edge-label) {
		border-color: var(--ring);
		color: var(--foreground);
	}
	.workflow-canvas :global(.svelte-flow__handle) {
		width: 8px;
		height: 8px;
		border: 2px solid var(--background);
	}
	.workflow-canvas :global(.svelte-flow__node:hover .workflow-add-next) {
		opacity: 1;
	}
	.workflow-canvas :global(.workflow-loop-handle) {
		opacity: 0;
		pointer-events: none;
	}
	.workflow-canvas :global(.svelte-flow__controls) {
		overflow: hidden;
		border: 1px solid var(--border);
		border-radius: 0.5rem;
		box-shadow: none;
	}
</style>
