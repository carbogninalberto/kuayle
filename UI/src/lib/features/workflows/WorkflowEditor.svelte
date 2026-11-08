<script lang="ts">
	import { untrack } from 'svelte';
	import { beforeNavigate } from '$app/navigation';
	import {
		ArrowLeft,
		ArrowRight,
		Check,
		CircleAlert,
		LayoutGrid,
		MousePointerClick,
		Plus,
		Trash2
	} from '@lucide/svelte';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import { Textarea } from '$lib/components/ui/textarea/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import * as Popover from '$lib/components/ui/popover/index.js';
	import * as Tabs from '$lib/components/ui/tabs/index.js';
	import { cn } from '$lib/utils.js';
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
	import { nodeHint, nodeIcon, nodeTone } from './node-style';
	import {
		PALETTE,
		conditionsFor,
		connect,
		graphForSave,
		newNode,
		removeEdge,
		removeNode,
		tidy,
		updateEdge,
		updateNode,
		waits
	} from './workflow-graph';

	// Svelte Flow editor for one workflow, filling the page: a top bar with the
	// name, status and save; the canvas with an add-step menu; and a side panel
	// with the selection (Inspect), every step and transition as keyboard-
	// reachable lists (Outline), and the description and budget (Settings).
	// The server validates as you edit and on save; each save is a new
	// immutable version.
	let {
		slug,
		workflow,
		profiles,
		editable,
		backHref,
		onsaved
	}: {
		slug: string;
		workflow: WorkflowSummary;
		profiles: AgentProfile[];
		editable: boolean;
		backHref: string;
		onsaved?: (saved: WorkflowSummary) => void;
	} = $props();

	const VARIABLES = ['{{previous_summary}}', '{{review_comments}}', '{{failing_checks}}', '{{diff}}'];
	const clone = (graph: WorkflowGraph): WorkflowGraph => JSON.parse(JSON.stringify(graph));

	let graph = $state<WorkflowGraph>(untrack(() => clone(workflow.graph)));
	let name = $state(untrack(() => workflow.name));
	let description = $state(untrack(() => workflow.description));
	let version = $state(untrack(() => workflow.latest_version));
	let selection = $state<Selection>(null);
	let tab = $state<'inspect' | 'outline' | 'settings'>('inspect');
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

	// Selecting on the canvas or in a list opens it in the Inspect tab.
	function select(next: Selection) {
		selection = next;
		if (next) tab = 'inspect';
	}

	beforeNavigate((navigation) => {
		if (dirty && editable && !confirm(m['workflows.editor.leave_unsaved']())) navigation.cancel();
	});

	function shortcut(event: KeyboardEvent) {
		if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 's') {
			event.preventDefault();
			if (dirty && name.trim()) void save();
		}
	}

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
		select({ kind: 'node', id: node.id });
	}

	function addTransition(event: SubmitEvent) {
		event.preventDefault();
		if (!newFrom || !newTo) return;
		const next = connect(graph, newFrom, newTo, profiles, newCondition || undefined);
		if (next === graph) return;
		change(next);
		select({ kind: 'edge', id: next.edges.at(-1)!.id });
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
		if (!editable || saving || !dirty) return;
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

<svelte:window onkeydown={shortcut} />

<div class="flex h-full min-h-0 flex-col" data-testid="workflow-editor">
	<header class="flex h-14 shrink-0 items-center gap-2 border-b px-3 sm:px-4">
		<Button
			variant="ghost"
			size="icon-sm"
			href={backHref}
			aria-label={m['workflows.back']()}
			title={m['workflows.back']()}><ArrowLeft /></Button
		>
		<div class="flex min-w-0 flex-1 items-center gap-2">
			{#if editable && !workflow.builtin}
				<Input
					id="workflow-name"
					aria-label={m['workflows.name']()}
					placeholder={m['workflows.editor.untitled']()}
					bind:value={name}
					maxlength={80}
					oninput={() => (dirty = true)}
					class="h-8 max-w-sm min-w-0 border-transparent bg-transparent px-2 text-base font-semibold shadow-none hover:border-input focus-visible:border-input dark:bg-transparent"
				/>
			{:else}
				<h1 class="truncate px-2 text-base font-semibold">{name}</h1>
			{/if}
			<div class="hidden items-center gap-1.5 sm:flex">
				{#if workflow.builtin}<Badge variant="secondary">{m['workflows.builtin']()}</Badge>{/if}
				{#if version > 0}<Badge variant="outline">{m['workflows.version']({ version })}</Badge>{/if}
				{#if !editable}<Badge variant="outline">{m['workflows.read_only']()}</Badge>{/if}
			</div>
		</div>
		<div class="flex shrink-0 items-center gap-2 text-xs text-muted-foreground" aria-live="polite">
			{#if issues.length > 0}
				<Popover.Root>
					<Popover.Trigger>
						{#snippet child({ props })}<Button
								{...props}
								variant="ghost"
								size="sm"
								class="text-destructive hover:text-destructive"
								data-testid="workflow-problems-trigger"
								><CircleAlert />{m['workflows.editor.problems']({ count: issues.length })}</Button
							>{/snippet}
					</Popover.Trigger>
					<Popover.Content align="end" class="w-80 p-0" data-testid="workflow-problems">
						<p class="border-b px-3 py-2 text-sm font-medium">{m['workflows.editor.problems_title']()}</p>
						<ul class="max-h-72 overflow-y-auto p-1" role="list">
							{#each issues as issue, i (i)}
								<li>
									<Button
										variant="plain"
										class="h-auto w-full flex-col items-start gap-0.5 rounded-md px-2 py-1.5 text-left whitespace-normal hover:bg-muted"
										disabled={!issue.edge_id && !issue.node_id}
										onclick={() =>
											select(
												issue.edge_id
													? { kind: 'edge', id: issue.edge_id }
													: issue.node_id
														? { kind: 'node', id: issue.node_id }
														: null
											)}
									>
										{#if issue.edge_id || issue.node_id}<span class="text-xs font-medium"
												>{issue.edge_id
													? conditionLabel(graph.edges.find((e) => e.id === issue.edge_id)?.condition ?? '')
													: nodeName(issue.node_id!)}</span
											>{/if}
										<span class="text-xs text-muted-foreground">{issueLabel(issue.code, issue.message)}</span>
									</Button>
								</li>
							{/each}
						</ul>
					</Popover.Content>
				</Popover.Root>
			{:else if checking}<span>{m['workflows.editor.checking']()}</span>
			{:else if dirty}<span class="flex items-center gap-1.5"
					><span class="size-1.5 rounded-full bg-amber-500"></span>{m['workflows.editor.unsaved']()}</span
				>
			{:else if editable}<span class="hidden items-center gap-1 sm:flex"
					><Check class="size-3.5" />{m['workflows.editor.saved_state']()}</span
				>{/if}
			{#if editable}<Button
					size="sm"
					onclick={save}
					disabled={saving || !dirty || !name.trim()}
					title={m['workflows.editor.shortcut_save']()}
					data-testid="workflow-save">{m['workflows.editor.save']()}</Button
				>{/if}
		</div>
	</header>

	<div class="grid min-h-0 flex-1 grid-rows-[minmax(22rem,1fr)_auto] lg:grid-cols-[minmax(0,1fr)_22rem] lg:grid-rows-1">
		<div class="relative min-h-0 bg-muted/30">
			<WorkflowCanvas
				{graph}
				{profiles}
				{issues}
				{editable}
				{selection}
				class="h-full rounded-none border-0 bg-transparent"
				label={m['workflows.editor.canvas']()}
				onselect={select}
				onmove={(id, position) => change(updateNode(graph, id, { position }))}
				onconnect={(source, target) => {
					const next = connect(graph, source, target, profiles);
					if (next !== graph) {
						change(next);
						select({ kind: 'edge', id: next.edges.at(-1)!.id });
					}
				}}
				ondelete={remove}
			/>
			{#if editable}
				<div class="absolute top-3 left-3 z-10 flex gap-2">
					<DropdownMenu.Root>
						<DropdownMenu.Trigger>
							{#snippet child({ props })}<Button {...props} size="sm" variant="outline" class="bg-background shadow-sm"
									><Plus />{m['workflows.editor.palette']()}</Button
								>{/snippet}
						</DropdownMenu.Trigger>
						<DropdownMenu.Content align="start" class="w-72">
							{#each PALETTE as type (type)}
								{@const Icon = nodeIcon({ type })}
								<DropdownMenu.Item onclick={() => addNode(type)} class="gap-3 py-2" data-testid="workflow-add-{type}">
									<span class={cn('flex size-7 shrink-0 items-center justify-center rounded-md', nodeTone({ type }))}
										><Icon class="size-4" /></span
									>
									<span class="min-w-0">
										<span class="block text-sm font-medium">{nodeTypeLabel(type)}</span>
										<span class="block text-xs text-muted-foreground">{nodeHint(type)}</span>
									</span>
								</DropdownMenu.Item>
							{/each}
						</DropdownMenu.Content>
					</DropdownMenu.Root>
					<Button
						size="sm"
						variant="outline"
						class="bg-background shadow-sm"
						onclick={() => change(tidy(graph))}
						data-testid="workflow-tidy"><LayoutGrid />{m['workflows.editor.tidy']()}</Button
					>
				</div>
			{/if}
		</div>

		<aside
			class="flex min-h-0 flex-col border-t bg-background lg:border-t-0 lg:border-l"
			aria-label={m['workflows.editor.inspector']()}
		>
			<Tabs.Root bind:value={tab} class="flex min-h-0 flex-1 flex-col gap-0">
				<div class="border-b px-3 py-2">
					<Tabs.List class="w-full">
						<Tabs.Trigger value="inspect">{m['workflows.editor.tab_inspect']()}</Tabs.Trigger>
						<Tabs.Trigger value="outline">{m['workflows.editor.tab_outline']()}</Tabs.Trigger>
						<Tabs.Trigger value="settings">{m['workflows.editor.tab_settings']()}</Tabs.Trigger>
					</Tabs.List>
				</div>

				<Tabs.Content value="inspect" class="min-h-0 flex-1 overflow-y-auto p-4">
					{#if selectedNode}
						{@const node = selectedNode}
						{@const Icon = nodeIcon(node)}
						<div class="space-y-4" data-testid="workflow-node-inspector">
							<div class="flex items-center gap-2.5">
								<span class={cn('flex size-8 shrink-0 items-center justify-center rounded-lg', nodeTone(node))}
									><Icon class="size-4" /></span
								>
								<div class="min-w-0">
									<p class="truncate text-sm font-medium">{node.label}</p>
									<p class="text-xs text-muted-foreground">{nodeTypeLabel(node.type)}</p>
								</div>
							</div>
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
										rows={6}
										maxlength={4000}
										disabled={!editable}
										class="font-mono text-xs"
										aria-describedby="node-prompt-help"
										oninput={(event) => change(updateNode(graph, node.id, { prompt: event.currentTarget.value }))}
									/>
									<p id="node-prompt-help" class="text-xs leading-relaxed text-muted-foreground">
										{m['workflows.editor.node_prompt_help']()}
										{#each VARIABLES as variable (variable)}<code
												class="mx-0.5 rounded bg-muted px-1 py-px font-mono text-[11px] text-foreground"
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
										rows={3}
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
							{#if (node.type !== 'start' && node.type !== 'end') || waits(node, profiles)}
								<div class="grid grid-cols-2 gap-3">
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
												aria-describedby="node-limits-help"
												oninput={(event) =>
													change(updateNode(graph, node.id, { max_visits: numberValue(event.currentTarget.value) }))}
											/>
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
												aria-describedby="node-limits-help"
												oninput={(event) =>
													change(
														updateNode(graph, node.id, { timeout_minutes: numberValue(event.currentTarget.value) })
													)}
											/>
										</div>
									{/if}
								</div>
								<div id="node-limits-help" class="space-y-1 text-xs text-muted-foreground">
									{#if node.type !== 'start' && node.type !== 'end'}<p>
											{m['workflows.editor.node_max_visits_help']()}
										</p>{/if}
									{#if waits(node, profiles)}<p>{m['workflows.editor.node_timeout_help']()}</p>{/if}
								</div>
							{/if}
							{#if editable && node.type !== 'start'}<div class="border-t pt-3">
									<Button
										variant="ghost"
										size="sm"
										class="-ml-2 text-destructive hover:text-destructive"
										onclick={() => remove([node.id], [])}><Trash2 />{m['workflows.editor.delete_node']()}</Button
									>
								</div>{/if}
						</div>
					{:else if selectedEdge}
						{@const edge = selectedEdge}
						<div class="space-y-4" data-testid="workflow-edge-inspector">
							<div class="flex items-center gap-2 text-sm font-medium">
								<span class="truncate">{nodeName(edge.source)}</span>
								<ArrowRight class="size-3.5 shrink-0 text-muted-foreground" />
								<span class="truncate">{nodeName(edge.target)}</span>
							</div>
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
							{#if editable}<div class="border-t pt-3">
									<Button
										variant="ghost"
										size="sm"
										class="-ml-2 text-destructive hover:text-destructive"
										onclick={() => remove([], [edge.id])}><Trash2 />{m['workflows.editor.delete_edge']()}</Button
									>
								</div>{/if}
						</div>
					{:else}
						<div class="flex flex-col items-center px-4 py-10 text-center">
							<span class="flex size-10 items-center justify-center rounded-full bg-muted text-muted-foreground"
								><MousePointerClick class="size-5" /></span
							>
							<p class="mt-3 text-sm font-medium">{m['workflows.editor.empty_title']()}</p>
							<p class="mt-1 text-xs leading-relaxed text-muted-foreground">{m['workflows.editor.empty_hint']()}</p>
						</div>
					{/if}
				</Tabs.Content>

				<Tabs.Content value="outline" class="min-h-0 flex-1 space-y-5 overflow-y-auto p-4">
					<section class="space-y-1.5" aria-labelledby="workflow-steps">
						<h3 id="workflow-steps" class="text-xs font-medium text-muted-foreground">
							{m['workflows.editor.steps']()} · {graph.nodes.length}
						</h3>
						<ul class="space-y-px" role="list">
							{#each graph.nodes as node (node.id)}
								{@const Icon = nodeIcon(node)}
								<li>
									<Button
										variant="plain"
										class={cn(
											'h-8 w-full justify-start gap-2 rounded-md px-2 text-left text-sm font-normal hover:bg-muted',
											selection?.id === node.id && 'bg-muted font-medium'
										)}
										aria-label={m['workflows.editor.select_named']({ name: node.label })}
										aria-pressed={selection?.kind === 'node' && selection.id === node.id}
										onclick={() => select({ kind: 'node', id: node.id })}
									>
										<span class={cn('flex size-5 shrink-0 items-center justify-center rounded', nodeTone(node))}
											><Icon class="size-3" /></span
										>
										<span class="truncate">{node.label}</span>
										<span class="ml-auto shrink-0 text-xs text-muted-foreground">{nodeTypeLabel(node.type)}</span>
									</Button>
								</li>
							{/each}
						</ul>
					</section>
					<section class="space-y-1.5" aria-labelledby="workflow-transitions">
						<h3 id="workflow-transitions" class="text-xs font-medium text-muted-foreground">
							{m['workflows.editor.transitions']()} · {graph.edges.length}
						</h3>
						<ul class="space-y-px" role="list">
							{#each graph.edges as edge (edge.id)}
								<li>
									<Button
										variant="plain"
										class={cn(
											'h-auto w-full flex-col items-start gap-0 rounded-md px-2 py-1 text-left font-normal hover:bg-muted',
											selection?.id === edge.id && 'bg-muted'
										)}
										aria-pressed={selection?.kind === 'edge' && selection.id === edge.id}
										onclick={() => select({ kind: 'edge', id: edge.id })}
									>
										<span class="w-full truncate text-sm"
											>{m['workflows.editor.transition_named']({
												from: nodeName(edge.source),
												to: nodeName(edge.target)
											})}</span
										>
										<span class="text-xs text-muted-foreground">{conditionLabel(edge.condition)}</span>
									</Button>
								</li>
							{/each}
						</ul>
						{#if editable}<form
								class="mt-3 space-y-2 rounded-lg border bg-muted/30 p-3"
								onsubmit={addTransition}
								data-testid="workflow-add-transition"
							>
								<p class="text-xs font-medium">{m['workflows.editor.add_transition']()}</p>
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
								<Button type="submit" size="sm" variant="outline" class="w-full" disabled={!newFrom || !newTo}
									><Plus />{m['workflows.editor.add_transition']()}</Button
								>
							</form>{/if}
					</section>
				</Tabs.Content>

				<Tabs.Content value="settings" class="min-h-0 flex-1 space-y-6 overflow-y-auto p-4">
					<div class="space-y-1.5">
						<Label for="workflow-description">{m['workflows.description']()}</Label>
						<Textarea
							id="workflow-description"
							bind:value={description}
							maxlength={500}
							rows={4}
							disabled={!editable}
							oninput={() => (dirty = true)}
						/>
					</div>
					<section class="space-y-3" aria-labelledby="workflow-budget">
						<h3 id="workflow-budget" class="text-sm font-medium">{m['workflows.editor.budget']()}</h3>
						<div class="grid grid-cols-2 gap-3">
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
						<p class="text-xs leading-relaxed text-muted-foreground">{m['workflows.editor.budget_help']()}</p>
					</section>
				</Tabs.Content>
			</Tabs.Root>
		</aside>
	</div>
</div>
