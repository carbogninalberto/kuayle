<script lang="ts">
	import '@xyflow/svelte/dist/style.css';
	import {
		SvelteFlow,
		Background,
		BackgroundVariant,
		Controls,
		MarkerType,
		type Connection,
		type Edge,
		type NodeTypes
	} from '@xyflow/svelte';
	import type { AgentProfile, WorkflowGraph, WorkflowIssue } from '$lib/api/workflows';
	import { m } from '$lib/paraglide/messages.js';
	import WorkflowNodeCard, { type WorkflowFlowNode } from './WorkflowNodeCard.svelte';
	import { conditionLabel, issueLabel, roleLabel } from './labels';
	import { layering, placeIssues, type FiredTransition } from './workflow-graph';

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
		onselect,
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
		onselect?: (selection: Selection) => void;
		onmove?: (id: string, position: { x: number; y: number }) => void;
		onconnect?: (source: string, target: string) => void;
		ondelete?: (nodes: string[], edges: string[]) => void;
	} = $props();

	const nodeTypes: NodeTypes = { workflow: WorkflowNodeCard };

	function subtitle(node: WorkflowGraph['nodes'][number]): string {
		switch (node.type) {
			case 'agent': {
				const profile = profiles.find((p) => p.id === node.profile_id);
				if (!profile) return '';
				const role = roleLabel(profile.role);
				return profile.name === role ? profile.name : `${profile.name} · ${role}`;
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
				visits: run ? (run.visits[node.id] ?? 0) : undefined
			}
		}));
		// Loops back to an earlier step leave and enter on the left and failures
		// leave on the right, so neither crosses the main path; transitions
		// between the same two steps share one label.
		const { back } = layering(graph);
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
			const target = graph.nodes.find((n) => n.id === edge.target);
			const toFailure = !loop && target?.type === 'end' && target.outcome === 'failed';
			const problem = placed.edges.has(edge.id);
			return {
				id: edge.id,
				type: 'smoothstep',
				source: edge.source,
				target: edge.target,
				...(loop ? { sourceHandle: 'loop-out', targetHandle: 'loop-in' } : {}),
				...(toFailure ? { sourceHandle: 'side-out', targetHandle: 'loop-in' } : {}),
				label: fired && fired.count > 1 ? `${conditionLabel(edge.condition)} ×${fired.count}` : pairLabel(edge),
				selected: selection?.kind === 'edge' && selection.id === edge.id,
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

	function connected(connection: Connection) {
		onconnect?.(connection.source, connection.target);
		redraw++;
	}
</script>

<div
	class="workflow-canvas min-h-0 w-full overflow-hidden rounded-lg border bg-muted/20 {className}"
	role="region"
	aria-label={label}
>
	<SvelteFlow
		bind:nodes
		bind:edges
		{nodeTypes}
		fitView
		fitViewOptions={{ padding: 0.25, maxZoom: 1 }}
		minZoom={0.2}
		maxZoom={1.5}
		nodesDraggable={editable}
		nodesConnectable={editable}
		elementsSelectable
		deleteKey={editable ? ['Backspace', 'Delete'] : null}
		proOptions={{ hideAttribution: false }}
		onnodeclick={({ node }) => onselect?.({ kind: 'node', id: node.id })}
		onedgeclick={({ edge }) => onselect?.({ kind: 'edge', id: edge.id })}
		onpaneclick={() => onselect?.(null)}
		onnodedragstop={({ targetNode }) => targetNode && onmove?.(targetNode.id, targetNode.position)}
		onconnect={connected}
		onbeforedelete={async ({ nodes: deleting }) => editable && !deleting.some((n) => n.data.node.type === 'start')}
		ondelete={({ nodes: removed, edges: removedEdges }) =>
			ondelete?.(
				removed.map((n) => n.id),
				removedEdges.map((e) => e.id)
			)}
	>
		<Background variant={BackgroundVariant.Dots} gap={18} size={1.2} />
		<Controls showLock={false} />
	</SvelteFlow>
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
		--xy-attribution-background-color: transparent;
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
