<script lang="ts">
	import '@xyflow/svelte/dist/style.css';
	import {
		SvelteFlow,
		Background,
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
	import { placeIssues, type FiredTransition } from './workflow-graph';

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
		run?: { current: string; visited: Set<string>; visits: Record<string, number>; fired: Map<string, FiredTransition> };
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
				return profile ? `${profile.name} · ${roleLabel(profile.role)}` : '';
			}
			case 'check':
				return node.check === 'ci' ? m['workflows.editor.check_ci']() : m['workflows.editor.check_tests']();
			case 'end':
				return node.outcome === 'failed' ? m['workflows.editor.outcome_failed']() : m['workflows.editor.outcome_done']();
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
		edges = graph.edges.map((edge) => {
			const fired = run?.fired.get(edge.id);
			const problem = placed.edges.has(edge.id);
			return {
				id: edge.id,
				source: edge.source,
				target: edge.target,
				label: fired && fired.count > 1 ? `${conditionLabel(edge.condition)} ×${fired.count}` : conditionLabel(edge.condition),
				selected: selection?.kind === 'edge' && selection.id === edge.id,
				deletable: editable,
				animated: !!fired && run?.current === edge.target,
				markerEnd: { type: MarkerType.ArrowClosed },
				class: problem ? 'workflow-edge-problem' : fired ? 'workflow-edge-fired' : run ? 'workflow-edge-idle' : undefined,
				ariaLabel: fired ? `${conditionLabel(edge.condition)}: ${fired.reason}` : conditionLabel(edge.condition)
			};
		});
	});

	function connected(connection: Connection) {
		onconnect?.(connection.source, connection.target);
		redraw++;
	}
</script>

<div class="workflow-canvas min-h-0 w-full overflow-hidden rounded-lg border bg-muted/20 {className}" role="region" aria-label={label}>
	<SvelteFlow
		bind:nodes
		bind:edges
		{nodeTypes}
		fitView
		minZoom={0.2}
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
		<Background />
		<Controls showLock={false} />
	</SvelteFlow>
</div>

<style>
	.workflow-canvas :global(.svelte-flow) {
		--xy-background-color: transparent;
		--xy-edge-stroke: var(--muted-foreground);
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
	.workflow-canvas :global(.svelte-flow__edge-text) {
		font-size: 11px;
	}
</style>
