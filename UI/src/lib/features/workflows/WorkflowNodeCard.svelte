<script lang="ts" module>
	import type { Node } from '@xyflow/svelte';
	import type { WorkflowNode } from '$lib/api/workflows';

	export interface WorkflowNodeData extends Record<string, unknown> {
		node: WorkflowNode;
		/** The profile, check or outcome shown under the name. */
		subtitle: string;
		/** Translated validation problems located on this node. */
		problems: string[];
		/** For a task's run: the node it is on, whether it entered it, and how often. */
		current?: boolean;
		visited?: boolean;
		visits?: number;
	}
	export type WorkflowFlowNode = Node<WorkflowNodeData, 'workflow'>;
</script>

<script lang="ts">
	import { Handle, Position, type NodeProps } from '@xyflow/svelte';
	import { CircleAlert } from '@lucide/svelte';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { cn } from '$lib/utils.js';
	import { m } from '$lib/paraglide/messages.js';
	import { nodeTypeLabel } from './labels';
	import { nodeIcon, nodeTone } from './node-style';

	// One step on the workflow canvas: a typed icon, its name and what it
	// uses. Svelte Flow makes it focusable and selectable from the keyboard;
	// the editor's outline offers the same actions.
	let { data, selected }: NodeProps<WorkflowFlowNode> = $props();
	const node = $derived(data.node);
	const limit = $derived(node.max_visits ?? 0);
	const Icon = $derived(nodeIcon(node));
	const terminal = $derived(node.type === 'start' || node.type === 'end');
	// Under the name: what the step uses, else its type unless the name says it.
	const detail = $derived(
		data.subtitle && data.subtitle !== node.label
			? data.subtitle
			: nodeTypeLabel(node.type) !== node.label
				? nodeTypeLabel(node.type)
				: ''
	);
	const visitsText = $derived(
		data.visits !== undefined && data.visits > 0
			? limit > 0
				? m['workflows.task.visits_of']({ count: data.visits, max: limit })
				: m['workflows.task.visits']({ count: data.visits })
			: limit > 0
				? `≤ ${limit}×`
				: ''
	);
</script>

{#if node.type !== 'start'}<Handle type="target" position={Position.Top} />
	<Handle id="loop-in" type="target" position={Position.Left} class="workflow-loop-handle" isConnectable={false} />{/if}
<div
	class={cn(
		'flex items-center gap-2.5 rounded-xl border bg-card p-2 pr-3 text-card-foreground shadow-sm transition-shadow',
		terminal ? 'w-44' : 'w-60',
		data.visited && 'border-primary/50',
		data.current && 'ring-2 ring-primary',
		selected && 'border-ring ring-2 ring-ring/40',
		data.problems.length > 0 && 'border-destructive'
	)}
	data-testid="workflow-node-{node.id}"
	data-current={data.current ? 'true' : undefined}
	title={data.problems.join('\n') || undefined}
>
	<span class={cn('flex size-8 shrink-0 items-center justify-center rounded-lg', nodeTone(node))}>
		<Icon class="size-4" aria-hidden="true" />
	</span>
	<div class="min-w-0 flex-1">
		<p class="truncate text-sm leading-5 font-medium">{node.label}</p>
		{#if detail || visitsText}<p class="truncate text-xs leading-4 text-muted-foreground">
				{[detail, visitsText].filter(Boolean).join(' · ')}
			</p>{/if}
	</div>
	{#if data.current}<Badge class="h-4 shrink-0 px-1.5 text-[10px]">{m['workflows.task.current']()}</Badge>{/if}
	{#if data.problems.length > 0}<CircleAlert
			class="size-4 shrink-0 text-destructive"
			aria-label={data.problems.join('. ')}
		/>{/if}
</div>
{#if node.type !== 'end'}<Handle type="source" position={Position.Bottom} />
	<Handle id="loop-out" type="source" position={Position.Left} class="workflow-loop-handle" isConnectable={false} />
	<Handle
		id="side-out"
		type="source"
		position={Position.Right}
		class="workflow-loop-handle"
		isConnectable={false}
	/>{/if}
