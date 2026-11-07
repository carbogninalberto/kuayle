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
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { cn } from '$lib/utils.js';
	import { m } from '$lib/paraglide/messages.js';
	import { nodeTypeLabel } from './labels';

	// One step on the workflow canvas. Svelte Flow makes it focusable and
	// selectable from the keyboard; the editor's lists offer the same actions.
	let { data, selected }: NodeProps<WorkflowFlowNode> = $props();
	const node = $derived(data.node);
	const limit = $derived(node.max_visits ?? 0);
</script>

{#if node.type !== 'start'}<Handle type="target" position={Position.Top} />{/if}
<div
	class={cn(
		'w-52 rounded-lg border bg-card px-3 py-2 text-card-foreground shadow-xs',
		node.type === 'end' && node.outcome === 'failed' && 'border-destructive/40',
		data.visited && 'border-primary/60',
		data.current && 'ring-2 ring-primary',
		selected && 'ring-2 ring-ring',
		data.problems.length > 0 && 'border-destructive ring-1 ring-destructive'
	)}
	data-testid="workflow-node-{node.id}"
	data-current={data.current ? 'true' : undefined}
>
	<div class="flex items-center justify-between gap-2">
		<span class="text-[10px] font-medium tracking-wide text-muted-foreground uppercase">{nodeTypeLabel(node.type)}</span>
		{#if data.current}<Badge class="h-4 px-1.5 text-[10px]">{m['workflows.task.current']()}</Badge>{/if}
	</div>
	<p class="truncate text-sm font-medium" title={node.label}>{node.label}</p>
	{#if data.subtitle}<p class="truncate text-xs text-muted-foreground" title={data.subtitle}>{data.subtitle}</p>{/if}
	{#if data.visits !== undefined && data.visits > 0}<p class="mt-1 text-[11px] text-muted-foreground">
			{limit > 0
				? m['workflows.task.visits_of']({ count: data.visits, max: limit })
				: m['workflows.task.visits']({ count: data.visits })}
		</p>
	{:else if limit > 0}<p class="mt-1 text-[11px] text-muted-foreground">
			{m['workflows.editor.node_max_visits']()}: {limit}
		</p>{/if}
	{#if data.problems.length > 0}<ul class="mt-1 space-y-0.5 text-[11px] text-destructive" role="list">
			{#each data.problems as problem, i (i)}<li>{problem}</li>{/each}
		</ul>{/if}
</div>
{#if node.type !== 'end'}<Handle type="source" position={Position.Bottom} />{/if}
