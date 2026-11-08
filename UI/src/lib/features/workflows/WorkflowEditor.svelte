<script lang="ts">
	import { untrack } from 'svelte';
	import { Plus, Save, Trash2 } from '@lucide/svelte';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import { Textarea } from '$lib/components/ui/textarea/index.js';
	import SelectField from '$lib/components/shared/SelectField.svelte';
	import {
		workflows,
		type AgentProfile,
		type WorkflowCondition,
		type WorkflowGraph,
		type WorkflowIssue,
		type WorkflowNodeType,
		type WorkflowSummary
	} from '$lib/api/workflows';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/paraglide/messages.js';
	import WorkflowCanvas, { type Selection } from './WorkflowCanvas.svelte';
	import { conditionLabel, issueLabel, nodeTypeLabel, roleLabel } from './labels';
	import {
		PALETTE,
		conditionsFor,
		connect,
		graphForSave,
		newNode,
		removeEdge,
		removeNode,
		updateEdge,
		updateNode,
		waits
	} from './workflow-graph';

	// Svelte Flow editor for one workflow: a palette, the canvas, a node and a
	// transition inspector, the budget, and lists of steps and transitions that
	// offer every canvas action from the keyboard. The server validates as you
	// edit and on save; each save is a new immutable version.
	let {
		slug,
		workflow,
		profiles,
		editable,
		onsaved
	}: {
		slug: string;
		workflow: WorkflowSummary;
		profiles: AgentProfile[];
		editable: boolean;
		onsaved?: (saved: WorkflowSummary) => void;
	} = $props();

	const VARIABLES = ['{{previous_summary}}', '{{review_comments}}', '{{failing_checks}}', '{{diff}}'];
	const clone = (graph: WorkflowGraph): WorkflowGraph => JSON.parse(JSON.stringify(graph));

	let graph = $state<WorkflowGraph>(untrack(() => clone(workflow.graph)));
	let name = $state(untrack(() => workflow.name));
	let description = $state(untrack(() => workflow.description));
	let version = $state(untrack(() => workflow.latest_version));
	let selection = $state<Selection>(null);
	let issues = $state<WorkflowIssue[]>([]);
	let checking = $state(false);
	let saving = $state(false);
	let dirty = $state(false);
	let newFrom = $state(''),
		newTo = $state(''),
		newCondition = $state<WorkflowCondition | ''>('');

	const activeProfiles = $derived(profiles.filter((p) => !p.archived_at));
	const selectedNode = $derived(
		selection?.kind === 'node' ? graph.nodes.find((n) => n.id === selection?.id) : undefined
	);
	const selectedEdge = $derived(
		selection?.kind === 'edge' ? graph.edges.find((e) => e.id === selection?.id) : undefined
	);
	const nodeName = (id: string) => graph.nodes.find((n) => n.id === id)?.label ?? id;

	function change(next: WorkflowGraph) {
		graph = next;
		dirty = true;
	}

	// Server-side validation, debounced while editing.
	let validateTimer: ReturnType<typeof setTimeout> | undefined;
	let validationRun = 0;
	$effect(() => {
		const snapshot = graphForSave(graph);
		clearTimeout(validateTimer);
		validateTimer = setTimeout(async () => {
			const run = ++validationRun;
			checking = true;
			try {
				const result = await workflows.validate(slug, snapshot);
				if (run === validationRun) issues = result.issues;
			} catch {
				/* Saving validates again and reports any problem. */
			} finally {
				if (run === validationRun) checking = false;
			}
		}, 400);
		return () => clearTimeout(validateTimer);
	});

	function addNode(type: WorkflowNodeType) {
		const node = newNode(graph, type, nodeTypeLabel(type));
		change({ ...graph, nodes: [...graph.nodes, node] });
		selection = { kind: 'node', id: node.id };
	}

	function addTransition(event: SubmitEvent) {
		event.preventDefault();
		if (!newFrom || !newTo) return;
		const next = connect(graph, newFrom, newTo, profiles, newCondition || undefined);
		if (next === graph) return;
		change(next);
		selection = { kind: 'edge', id: next.edges.at(-1)!.id };
		newCondition = '';
	}

	function remove(nodes: string[], edges: string[]) {
		let next = graph;
		for (const id of edges) next = removeEdge(next, id);
		for (const id of nodes) if (next.nodes.find((n) => n.id === id)?.type !== 'start') next = removeNode(next, id);
		change(next);
		selection = null;
	}

	const numberValue = (value: string | number) => {
		const parsed = Math.floor(Number(value));
		return Number.isFinite(parsed) && parsed > 0 ? parsed : 0;
	};

	async function save() {
		if (!editable || saving) return;
		saving = true;
		try {
			const saved = await workflows.save(slug, workflow.id, {
				name,
				description,
				graph: graphForSave(graph),
				expected_version: version
			});
			version = saved.latest_version;
			dirty = false;
			issues = [];
			appToast.success(m['workflows.editor.saved']({ version: saved.latest_version }));
			onsaved?.(saved);
		} catch (error) {
			const body = error as { status?: number; error?: { code?: string; issues?: WorkflowIssue[] } };
			if (body.error?.issues) issues = body.error.issues;
			if (body.error?.code === 'WORKFLOW_STALE') appToast.error(m['workflows.editor.stale']());
			else appToast.apiError(error, m['workflows.failed_save']());
		} finally {
			saving = false;
		}
	}

	const sourceConditions = (source: string, own?: string) => {
		const node = graph.nodes.find((n) => n.id === source);
		if (!node) return [];
		const used = new Set(graph.edges.filter((e) => e.source === source && e.id !== own).map((e) => e.condition));
		return conditionsFor(node, profiles).filter((c) => !used.has(c));
	};
</script>

<div class="space-y-4" data-testid="workflow-editor">
	<div class="flex flex-wrap items-end gap-3">
		<div class="min-w-48 flex-1 space-y-1.5">
			<Label for="workflow-name">{m['workflows.name']()}</Label>
			<Input
				id="workflow-name"
				bind:value={name}
				maxlength={80}
				disabled={!editable || workflow.builtin}
				oninput={() => (dirty = true)}
			/>
		</div>
		<div class="flex flex-wrap items-center gap-2 pb-1 text-xs text-muted-foreground" aria-live="polite">
			{#if version > 0}<Badge variant="outline">{m['workflows.version']({ version })}</Badge>{/if}
			{#if !editable}<Badge variant="secondary">{m['workflows.read_only']()}</Badge>{/if}
			{#if dirty}<span>{m['workflows.editor.unsaved']()}</span>{/if}
			{#if checking}<span>{m['workflows.editor.checking']()}</span>
			{:else if issues.length > 0}<span class="text-destructive"
					>{m['workflows.editor.problems']({ count: issues.length })}</span
				>
			{:else}<span>{m['workflows.editor.valid']()}</span>{/if}
		</div>
		{#if editable}<Button onclick={save} disabled={saving || !dirty || !name.trim()} data-testid="workflow-save"
				><Save />{m['workflows.editor.save']()}</Button
			>{/if}
	</div>
	<div class="space-y-1.5">
		<Label for="workflow-description">{m['workflows.description']()}</Label>
		<Textarea
			id="workflow-description"
			bind:value={description}
			maxlength={500}
			rows={2}
			disabled={!editable}
			oninput={() => (dirty = true)}
		/>
	</div>

	<div class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_20rem]">
		<div class="min-w-0 space-y-3">
			{#if editable}<div
					class="flex flex-wrap items-center gap-1.5"
					role="toolbar"
					aria-label={m['workflows.editor.palette']()}
				>
					<span class="mr-1 text-xs font-medium text-muted-foreground">{m['workflows.editor.palette']()}</span>
					{#each PALETTE as type (type)}<Button
							size="sm"
							variant="outline"
							onclick={() => addNode(type)}
							data-testid="workflow-add-{type}"><Plus />{nodeTypeLabel(type)}</Button
						>{/each}
				</div>{/if}
			<WorkflowCanvas
				{graph}
				{profiles}
				{issues}
				{editable}
				{selection}
				label={m['workflows.editor.canvas']()}
				onselect={(next) => (selection = next)}
				onmove={(id, position) => change(updateNode(graph, id, { position }))}
				onconnect={(source, target) => {
					const next = connect(graph, source, target, profiles);
					if (next !== graph) {
						change(next);
						selection = { kind: 'edge', id: next.edges.at(-1)!.id };
					}
				}}
				ondelete={remove}
			/>
			{#if issues.length > 0}<section
					aria-labelledby="workflow-problems"
					class="rounded-lg border border-destructive/40 bg-destructive/5 p-3"
					data-testid="workflow-problems"
				>
					<h3 id="workflow-problems" class="text-sm font-medium text-destructive">
						{m['workflows.editor.problems_title']()}
					</h3>
					<ul class="mt-1 space-y-1 text-sm" role="list">
						{#each issues as issue, i (i)}
							<li class="flex flex-wrap items-baseline gap-1">
								{#if issue.edge_id || issue.node_id}<Button
										variant="link"
										size="sm"
										class="h-auto p-0"
										onclick={() =>
											(selection = issue.edge_id
												? { kind: 'edge', id: issue.edge_id }
												: { kind: 'node', id: issue.node_id! })}
										>{issue.edge_id
											? conditionLabel(graph.edges.find((e) => e.id === issue.edge_id)?.condition ?? '')
											: nodeName(issue.node_id!)}</Button
									>:{/if}
								<span>{issueLabel(issue.code, issue.message)}</span>
							</li>
						{/each}
					</ul>
				</section>{/if}
		</div>

		<aside class="space-y-4 rounded-lg border bg-card p-4" aria-label={m['workflows.editor.inspector']()}>
			{#if selectedNode}
				{@const node = selectedNode}
				<div class="space-y-3" data-testid="workflow-node-inspector">
					<p class="text-xs font-medium text-muted-foreground uppercase">{nodeTypeLabel(node.type)}</p>
					<div class="space-y-1.5">
						<Label for="node-label">{m['workflows.editor.node_label']()}</Label>
						<Input
							id="node-label"
							value={node.label}
							maxlength={80}
							disabled={!editable}
							oninput={(event) => change(updateNode(graph, node.id, { label: event.currentTarget.value }))}
						/>
					</div>
					{#if node.type === 'agent'}
						<div class="space-y-1.5">
							<Label for="node-profile">{m['workflows.editor.node_profile']()}</Label>
							<SelectField
								id="node-profile"
								bind:value={
									() => node.profile_id ?? '',
									(value: string) => change(updateNode(graph, node.id, { profile_id: value || null }))
								}
								disabled={!editable}
								options={[
									{ value: '', label: m['workflows.editor.node_profile_placeholder']() },
									...activeProfiles.map((p) => ({ value: p.id, label: `${p.name} · ${roleLabel(p.role)}` }))
								]}
							/>
						</div>
						<div class="space-y-1.5">
							<Label for="node-prompt">{m['workflows.editor.node_prompt']()}</Label>
							<Textarea
								id="node-prompt"
								value={node.prompt ?? ''}
								rows={5}
								maxlength={4000}
								disabled={!editable}
								aria-describedby="node-prompt-help"
								oninput={(event) => change(updateNode(graph, node.id, { prompt: event.currentTarget.value }))}
							/>
							<p id="node-prompt-help" class="text-xs text-muted-foreground">
								{m['workflows.editor.node_prompt_help']()}
								{#each VARIABLES as variable (variable)}<code class="mx-0.5 rounded bg-muted px-1 font-mono text-[11px]"
										>{variable}</code
									>{/each}
							</p>
						</div>
					{:else if node.type === 'approval'}
						<div class="space-y-1.5">
							<Label for="node-question">{m['workflows.editor.node_question']()}</Label>
							<Textarea
								id="node-question"
								value={node.prompt ?? ''}
								rows={2}
								maxlength={4000}
								disabled={!editable}
								oninput={(event) => change(updateNode(graph, node.id, { prompt: event.currentTarget.value }))}
							/>
						</div>
					{:else if node.type === 'check'}
						<div class="space-y-1.5">
							<Label for="node-check">{m['workflows.editor.node_check']()}</Label>
							<SelectField
								id="node-check"
								bind:value={
									() => node.check ?? 'tests',
									(value: string) => change(updateNode(graph, node.id, { check: value as 'tests' | 'ci' }))
								}
								disabled={!editable}
								options={[
									{ value: 'tests', label: m['workflows.editor.check_tests']() },
									{ value: 'ci', label: m['workflows.editor.check_ci']() }
								]}
							/>
						</div>
					{:else if node.type === 'end'}
						<div class="space-y-1.5">
							<Label for="node-outcome">{m['workflows.editor.node_outcome']()}</Label>
							<SelectField
								id="node-outcome"
								bind:value={
									() => node.outcome ?? 'done',
									(value: string) => change(updateNode(graph, node.id, { outcome: value as 'done' | 'failed' }))
								}
								disabled={!editable}
								options={[
									{ value: 'done', label: m['workflows.editor.outcome_done']() },
									{ value: 'failed', label: m['workflows.editor.outcome_failed']() }
								]}
							/>
						</div>
					{/if}
					{#if node.type !== 'start' && node.type !== 'end'}
						<div class="space-y-1.5">
							<Label for="node-max-visits">{m['workflows.editor.node_max_visits']()}</Label>
							<Input
								id="node-max-visits"
								type="number"
								min={0}
								max={100}
								value={node.max_visits ?? 0}
								disabled={!editable}
								aria-describedby="node-max-visits-help"
								oninput={(event) =>
									change(updateNode(graph, node.id, { max_visits: numberValue(event.currentTarget.value) }))}
							/>
							<p id="node-max-visits-help" class="text-xs text-muted-foreground">
								{m['workflows.editor.node_max_visits_help']()}
							</p>
						</div>
					{/if}
					{#if waits(node, profiles)}
						<div class="space-y-1.5">
							<Label for="node-timeout">{m['workflows.editor.node_timeout']()}</Label>
							<Input
								id="node-timeout"
								type="number"
								min={0}
								max={10080}
								value={node.timeout_minutes ?? 0}
								disabled={!editable}
								aria-describedby="node-timeout-help"
								oninput={(event) =>
									change(updateNode(graph, node.id, { timeout_minutes: numberValue(event.currentTarget.value) }))}
							/>
							<p id="node-timeout-help" class="text-xs text-muted-foreground">
								{m['workflows.editor.node_timeout_help']()}
							</p>
						</div>
					{/if}
					{#if editable && node.type !== 'start'}<Button
							variant="outline"
							size="sm"
							onclick={() => remove([node.id], [])}><Trash2 />{m['workflows.editor.delete_node']()}</Button
						>{/if}
				</div>
			{:else if selectedEdge}
				{@const edge = selectedEdge}
				<div class="space-y-3" data-testid="workflow-edge-inspector">
					<p class="text-sm font-medium">{nodeName(edge.source)} → {nodeName(edge.target)}</p>
					<div class="space-y-1.5">
						<Label for="edge-condition">{m['workflows.editor.edge_condition']()}</Label>
						<SelectField
							id="edge-condition"
							bind:value={
								() => edge.condition,
								(value: string) => change(updateEdge(graph, edge.id, { condition: value as WorkflowCondition }))
							}
							disabled={!editable}
							options={[
								edge.condition,
								...sourceConditions(edge.source, edge.id).filter((c) => c !== edge.condition)
							].map((c) => ({ value: c, label: conditionLabel(c) }))}
						/>
					</div>
					{#if editable}<Button variant="outline" size="sm" onclick={() => remove([], [edge.id])}
							><Trash2 />{m['workflows.editor.delete_edge']()}</Button
						>{/if}
				</div>
			{:else}
				<p class="text-xs text-muted-foreground">{m['workflows.editor.nothing_selected']()}</p>
				<section class="space-y-2" aria-labelledby="workflow-budget">
					<h3 id="workflow-budget" class="text-sm font-medium">{m['workflows.editor.budget']()}</h3>
					<div class="grid grid-cols-2 gap-2">
						<div class="space-y-1.5">
							<Label for="budget-iterations">{m['workflows.editor.budget_iterations']()}</Label>
							<Input
								id="budget-iterations"
								type="number"
								min={0}
								max={200}
								value={graph.budget.max_iterations ?? 0}
								disabled={!editable}
								oninput={(event) =>
									change({
										...graph,
										budget: { ...graph.budget, max_iterations: numberValue(event.currentTarget.value) }
									})}
							/>
						</div>
						<div class="space-y-1.5">
							<Label for="budget-minutes">{m['workflows.editor.budget_minutes']()}</Label>
							<Input
								id="budget-minutes"
								type="number"
								min={0}
								max={43200}
								value={graph.budget.max_minutes ?? 0}
								disabled={!editable}
								oninput={(event) =>
									change({
										...graph,
										budget: { ...graph.budget, max_minutes: numberValue(event.currentTarget.value) }
									})}
							/>
						</div>
					</div>
					<div class="space-y-1.5">
						<Label for="budget-policy">{m['workflows.editor.budget_on_exhausted']()}</Label>
						<SelectField
							id="budget-policy"
							bind:value={
								() => graph.budget.on_exhausted ?? 'fail',
								(value: string) =>
									change({ ...graph, budget: { ...graph.budget, on_exhausted: value as 'fail' | 'ask' } })
							}
							disabled={!editable}
							options={[
								{ value: 'fail', label: m['workflows.editor.on_fail']() },
								{ value: 'ask', label: m['workflows.editor.on_ask']() }
							]}
						/>
					</div>
					<p class="text-xs text-muted-foreground">{m['workflows.editor.budget_help']()}</p>
				</section>
			{/if}

			<section class="space-y-1" aria-labelledby="workflow-steps">
				<h3 id="workflow-steps" class="text-sm font-medium">{m['workflows.editor.steps']()}</h3>
				<ul class="space-y-0.5" role="list">
					{#each graph.nodes as node (node.id)}<li>
							<Button
								variant="plain"
								class="w-full truncate rounded px-1.5 py-0.5 text-left text-xs hover:bg-muted {selection?.id === node.id
									? 'bg-muted font-medium'
									: ''}"
								aria-label={m['workflows.editor.select_named']({ name: node.label })}
								aria-pressed={selection?.kind === 'node' && selection.id === node.id}
								onclick={() => (selection = { kind: 'node', id: node.id })}
								>{node.label} <span class="text-muted-foreground">· {nodeTypeLabel(node.type)}</span></Button
							>
						</li>{/each}
				</ul>
			</section>
			<section class="space-y-1" aria-labelledby="workflow-transitions">
				<h3 id="workflow-transitions" class="text-sm font-medium">{m['workflows.editor.transitions']()}</h3>
				<ul class="space-y-0.5" role="list">
					{#each graph.edges as edge (edge.id)}<li>
							<Button
								variant="plain"
								class="w-full truncate rounded px-1.5 py-0.5 text-left text-xs hover:bg-muted {selection?.id === edge.id
									? 'bg-muted font-medium'
									: ''}"
								aria-pressed={selection?.kind === 'edge' && selection.id === edge.id}
								onclick={() => (selection = { kind: 'edge', id: edge.id })}
								>{nodeName(edge.source)} → {nodeName(edge.target)}
								<span class="text-muted-foreground">· {conditionLabel(edge.condition)}</span></Button
							>
						</li>{/each}
				</ul>
				{#if editable}<form
						class="mt-2 space-y-2 rounded-md border p-2"
						onsubmit={addTransition}
						data-testid="workflow-add-transition"
					>
						<div class="grid grid-cols-2 gap-2">
							<div class="space-y-1">
								<Label for="transition-from" class="text-xs">{m['workflows.editor.transition_from']()}</Label>
								<SelectField
									id="transition-from"
									bind:value={newFrom}
									options={[
										{ value: '', label: m['workflows.editor.transition_from']() },
										...graph.nodes.filter((n) => n.type !== 'end').map((n) => ({ value: n.id, label: n.label }))
									]}
								/>
							</div>
							<div class="space-y-1">
								<Label for="transition-to" class="text-xs">{m['workflows.editor.transition_to']()}</Label>
								<SelectField
									id="transition-to"
									bind:value={newTo}
									options={[
										{ value: '', label: m['workflows.editor.transition_to']() },
										...graph.nodes.filter((n) => n.type !== 'start').map((n) => ({ value: n.id, label: n.label }))
									]}
								/>
							</div>
						</div>
						{#if newFrom}<div class="space-y-1">
								<Label for="transition-condition" class="text-xs">{m['workflows.editor.edge_condition']()}</Label>
								<SelectField
									id="transition-condition"
									bind:value={newCondition}
									options={[
										{ value: '', label: m['workflows.editor.edge_condition']() },
										...sourceConditions(newFrom).map((c) => ({ value: c, label: conditionLabel(c) }))
									]}
								/>
							</div>{/if}
						<Button type="submit" size="sm" variant="outline" disabled={!newFrom || !newTo}
							><Plus />{m['workflows.editor.add_transition']()}</Button
						>
					</form>{/if}
			</section>
		</aside>
	</div>
</div>
