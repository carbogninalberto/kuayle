<script lang="ts">
	import { tick, untrack } from 'svelte';
	import { beforeNavigate, goto } from '$app/navigation';
	import * as AlertDialog from '$lib/components/ui/alert-dialog/index.js';
	import { Switch } from '$lib/components/ui/switch/index.js';
	import {
		ArrowLeft,
		ArrowRight,
		Check,
		CircleAlert,
		Copy,
		StretchHorizontal,
		Link2,
		Maximize,
		MousePointerClick,
		Pencil,
		Plus,
		Redo2,
		Trash2,
		Undo2,
		ZoomIn,
		ZoomOut
	} from '@lucide/svelte';
	import * as ContextMenu from '$lib/components/ui/context-menu/index.js';
	import * as Tooltip from '$lib/components/ui/tooltip/index.js';
	import type { FlowApi } from './FlowBridge.svelte';
	import AiMarkdownEditor from './AiMarkdownEditor.svelte';
	import InfoHint from '$lib/components/shared/InfoHint.svelte';
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
	import { m } from '$lib/features/i18n/messages';
	import WorkflowCanvas, { type Selection } from './WorkflowCanvas.svelte';
	import { conditionLabel, issueLabel, nodeTypeLabel, profileTitle } from './labels';
	import { blockHint, blockIcon, blockLabel, blockTone, nodeIcon, nodeTone } from './node-style';
	import {
		BLOCKS,
		retries,
		retryCondition,
		reviewBlock,
		setRetry,
		type Block,
		addNext,
		conditionsFor,
		connect,
		duplicateNode,
		graphForSave,
		leftToRight,
		samePositions,
		newNode,
		removeEdge,
		removeNode,
		tidy,
		updateEdge,
		updateNode,
		waits
	} from './workflow-graph';

	// Svelte Flow editor for one workflow, filling the page: a top bar with the
	// name, status and save; the canvas, edited directly (right-click menus, a
	// "+" beside each step, drag to connect) with a floating toolbar (add, undo,
	// redo, arrange left to right, zoom); and a side panel
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

	const VARIABLES = ['{{plan}}', '{{previous_summary}}', '{{review_comments}}', '{{failing_checks}}', '{{diff}}'];
	const clone = (graph: WorkflowGraph): WorkflowGraph => JSON.parse(JSON.stringify(graph));

	// The chart runs left to right, so a workflow saved top to bottom opens
	// arranged. That alone is not a change: nothing is unsaved and no version
	// is made until the person edits something, and that save then stores the
	// arranged positions too.
	let graph = $state<WorkflowGraph>(untrack(() => leftToRight(clone(workflow.graph))));
	let name = $state(untrack(() => workflow.name));
	let description = $state(untrack(() => workflow.description));
	let version = $state(untrack(() => workflow.latest_version));
	let selection = $state<Selection>(null);
	let tab = $state<'inspect' | 'outline' | 'settings'>('inspect');
	let issues = $state<WorkflowIssue[]>([]);
	let checking = $state(false);
	let saving = $state(false);
	let dirty = $state(false);
	let flow = $state<FlowApi>();
	// What the last right click was on, and where on the canvas.
	let menuTarget = $state<{ kind: 'pane' | 'node' | 'edge'; id: string; at: { x: number; y: number } }>({
		kind: 'pane',
		id: '',
		at: { x: 0, y: 0 }
	});
	// Undo and redo: graphs before and after each change. Changes within half
	// a second (typing in a field) are one step.
	let past = $state<WorkflowGraph[]>([]),
		future = $state<WorkflowGraph[]>([]),
		lastChange = 0;

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

	// Leaving with unsaved changes asks first, in a dialog. Closing the tab
	// can only use the browser's own prompt.
	let leaving = $state<URL | null>(null),
		leaveConfirmed = false;
	beforeNavigate((navigation) => {
		if (!dirty || !editable || leaveConfirmed) return;
		navigation.cancel();
		if (navigation.type !== 'leave' && navigation.to) leaving = navigation.to.url;
	});
	function leave() {
		const target = leaving;
		leaving = null;
		if (!target) return;
		leaveConfirmed = true;
		void goto(target);
	}

	function shortcut(event: KeyboardEvent) {
		if (!(event.metaKey || event.ctrlKey)) return;
		const key = event.key.toLowerCase();
		if (key === 's') {
			event.preventDefault();
			if (dirty && name.trim()) void save();
			return;
		}
		// Text fields keep their own undo.
		const target = event.target as HTMLElement | null;
		if (!editable || target?.closest('input, textarea, select, [contenteditable="true"]')) return;
		if (key === 'd' && selection?.kind === 'node') {
			event.preventDefault();
			duplicate(selection.id);
		} else if (key === 'z' && !event.shiftKey) {
			event.preventDefault();
			undo();
		} else if ((key === 'z' && event.shiftKey) || key === 'y') {
			event.preventDefault();
			redo();
		}
	}

	function change(next: WorkflowGraph) {
		if (next === graph) return;
		const now = Date.now();
		if (now - lastChange > 500) past = [...past.slice(-99), graph];
		lastChange = now;
		future = [];
		graph = next;
		dirty = true;
	}

	function undo() {
		const previous = past.at(-1);
		if (!previous) return;
		future = [graph, ...future];
		past = past.slice(0, -1);
		graph = previous;
		lastChange = 0;
		dirty = true;
		selection = null;
	}

	function redo() {
		const next = future[0];
		if (!next) return;
		past = [...past, graph];
		future = future.slice(1);
		graph = next;
		lastChange = 0;
		dirty = true;
		selection = null;
	}

	// Arranges the steps left to right. Only positions change, so it is saved
	// and undone like any other edit.
	async function arrange() {
		const arranged = tidy(graph);
		if (!samePositions(arranged, graph)) {
			lastChange = 0;
			change(arranged);
		}
		await tick();
		flow?.fit();
	}

	function addAt(type: Block, at?: { x: number; y: number }) {
		if (type === 'review') {
			const added = reviewBlock(graph, blockLabel(type), profiles, at);
			lastChange = 0;
			change(added.graph);
			select({ kind: 'node', id: added.id });
			return;
		}
		const node = newNode(graph, type, blockLabel(type), at);
		lastChange = 0;
		change({ ...graph, nodes: [...graph.nodes, node] });
		select({ kind: 'node', id: node.id });
	}

	function addAfter(source: string, type: Block) {
		let added: { graph: WorkflowGraph; id: string };
		if (type === 'review') {
			const from = graph.nodes.find((n) => n.id === source);
			const placed = from ? addNext(graph, source, 'agent', '', profiles) : { graph, id: '' };
			const at = placed.graph.nodes.find((n) => n.id === placed.id)?.position;
			const block = reviewBlock(graph, blockLabel(type), profiles, at);
			added = { graph: connect(block.graph, source, block.id, profiles), id: block.id };
		} else added = addNext(graph, source, type, blockLabel(type), profiles);
		if (!added.id) return;
		lastChange = 0;
		change(added.graph);
		select({ kind: 'node', id: added.id });
	}

	function connectTo(source: string, target: string) {
		const next = connect(graph, source, target, profiles);
		if (next === graph) return;
		lastChange = 0;
		change(next);
		select({ kind: 'edge', id: next.edges.at(-1)!.id });
	}

	function duplicate(id: string) {
		const copied = duplicateNode(graph, id);
		if (!copied.id) return;
		lastChange = 0;
		change(copied.graph);
		select({ kind: 'node', id: copied.id });
	}

	// Menu actions close the menu before changing the graph: a change re-renders
	// the canvas under the menu, which otherwise leaves it open and the page
	// unclickable.
	let menuOpen = $state(false);
	function act(action: () => void) {
		menuOpen = false;
		queueMicrotask(action);
	}

	// The context menu opens on the canvas; the canvas reports whether a step
	// or a transition was under the pointer before the menu reads the target.
	function contextAt(event: MouseEvent) {
		menuTarget = { kind: 'pane', id: '', at: flow?.toFlow({ x: event.clientX, y: event.clientY }) ?? { x: 0, y: 0 } };
	}
	const menuNode = $derived(menuTarget.kind === 'node' ? graph.nodes.find((n) => n.id === menuTarget.id) : undefined);
	const menuEdge = $derived(menuTarget.kind === 'edge' ? graph.edges.find((e) => e.id === menuTarget.id) : undefined);
	const connectable = (source: string) =>
		graph.nodes.filter((n) => n.id !== source && n.type !== 'start' && sourceConditions(source).length > 0);

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

	function remove(nodes: string[], edges: string[]) {
		let next = graph;
		for (const id of edges) next = removeEdge(next, id);
		for (const id of nodes) if (next.nodes.find((n) => n.id === id)?.type !== 'start') next = removeNode(next, id);
		lastChange = 0;
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
			{#snippet canvas()}
				<WorkflowCanvas
					{graph}
					{profiles}
					{issues}
					{editable}
					{selection}
					controls={!editable}
					bind:api={flow}
					class="h-full rounded-none border-0 bg-transparent"
					label={m['workflows.editor.canvas']()}
					onselect={select}
					onmove={(id, position) => {
						lastChange = 0;
						change(updateNode(graph, id, { position }));
					}}
					onconnect={connectTo}
					onaddnext={addAfter}
					oncontext={(target) => {
						if (target) menuTarget = { ...menuTarget, kind: target.kind, id: target.id };
					}}
					ondelete={remove}
				/>
			{/snippet}
			{#if editable}
				<ContextMenu.Root bind:open={menuOpen}>
					<ContextMenu.Trigger class="block h-full" oncontextmenucapture={contextAt} data-testid="workflow-canvas-menu">
						{@render canvas()}
					</ContextMenu.Trigger>
					<ContextMenu.Content class="w-60">
						{#if menuNode}
							{@const node = menuNode}
							<ContextMenu.Label class="truncate text-xs text-muted-foreground">{node.label}</ContextMenu.Label>
							<ContextMenu.Item onclick={() => act(() => select({ kind: 'node', id: node.id }))}
								><Pencil />{m['workflows.editor.edit']()}</ContextMenu.Item
							>
							{#if node.type !== 'end'}
								<ContextMenu.Sub>
									<ContextMenu.SubTrigger><Plus />{m['workflows.editor.add_next']()}</ContextMenu.SubTrigger>
									<ContextMenu.SubContent class="w-56">
										{#each BLOCKS as type (type)}
											{@const Icon = blockIcon(type)}
											<ContextMenu.Item onclick={() => act(() => addAfter(node.id, type))}
												><Icon />{blockLabel(type)}</ContextMenu.Item
											>
										{/each}
									</ContextMenu.SubContent>
								</ContextMenu.Sub>
								<ContextMenu.Sub>
									<ContextMenu.SubTrigger disabled={connectable(node.id).length === 0}
										><Link2 />{m['workflows.editor.connect_to']()}</ContextMenu.SubTrigger
									>
									<ContextMenu.SubContent class="max-h-72 w-56 overflow-y-auto">
										{#each connectable(node.id) as target (target.id)}
											{@const Icon = nodeIcon(target)}
											<ContextMenu.Item onclick={() => act(() => connectTo(node.id, target.id))}
												><Icon /><span class="truncate">{target.label}</span></ContextMenu.Item
											>
										{/each}
									</ContextMenu.SubContent>
								</ContextMenu.Sub>
							{/if}
							{#if node.type !== 'start'}
								{#if retryCondition(node, profiles)}
									<ContextMenu.CheckboxItem
										checked={retries(graph, node, profiles)}
										onCheckedChange={(on) =>
											act(() => {
												lastChange = 0;
												change(setRetry(graph, node.id, on, profiles));
											})}>{m['workflows.editor.retry']()}</ContextMenu.CheckboxItem
									>
								{/if}
								<ContextMenu.Item onclick={() => act(() => duplicate(node.id))}
									><Copy />{m['workflows.editor.duplicate']()}<ContextMenu.Shortcut>⌘D</ContextMenu.Shortcut
									></ContextMenu.Item
								>
								<ContextMenu.Separator />
								<ContextMenu.Item variant="destructive" onclick={() => act(() => remove([node.id], []))}
									><Trash2 />{m['workflows.editor.delete_node']()}<ContextMenu.Shortcut>⌫</ContextMenu.Shortcut
									></ContextMenu.Item
								>
							{/if}
						{:else if menuEdge}
							{@const edge = menuEdge}
							<ContextMenu.Label class="truncate text-xs text-muted-foreground"
								>{m['workflows.editor.transition_named']({
									from: nodeName(edge.source),
									to: nodeName(edge.target)
								})}</ContextMenu.Label
							>
							<ContextMenu.Sub>
								<ContextMenu.SubTrigger
									><ArrowRight />{m['workflows.editor.edge_condition']()}: {conditionLabel(
										edge.condition
									)}</ContextMenu.SubTrigger
								>
								<ContextMenu.SubContent class="w-56">
									<ContextMenu.RadioGroup
										value={edge.condition}
										onValueChange={(value) =>
											act(() => {
												lastChange = 0;
												change(updateEdge(graph, edge.id, { condition: value as WorkflowCondition }));
											})}
									>
										{#each [edge.condition, ...sourceConditions(edge.source, edge.id).filter((c) => c !== edge.condition)] as condition (condition)}
											<ContextMenu.RadioItem value={condition}>{conditionLabel(condition)}</ContextMenu.RadioItem>
										{/each}
									</ContextMenu.RadioGroup>
								</ContextMenu.SubContent>
							</ContextMenu.Sub>
							<ContextMenu.Separator />
							<ContextMenu.Item variant="destructive" onclick={() => act(() => remove([], [edge.id]))}
								><Trash2 />{m['workflows.editor.delete_edge']()}</ContextMenu.Item
							>
						{:else}
							{@const at = menuTarget.at}
							<ContextMenu.Sub>
								<ContextMenu.SubTrigger><Plus />{m['workflows.editor.add_step_here']()}</ContextMenu.SubTrigger>
								<ContextMenu.SubContent class="w-56">
									{#each BLOCKS as type (type)}
										{@const Icon = blockIcon(type)}
										<ContextMenu.Item onclick={() => act(() => addAt(type, at))}
											><Icon />{blockLabel(type)}</ContextMenu.Item
										>
									{/each}
								</ContextMenu.SubContent>
							</ContextMenu.Sub>
							<ContextMenu.Item onclick={() => act(() => void arrange())}
								><StretchHorizontal />{m['workflows.editor.tidy']()}</ContextMenu.Item
							>
							<ContextMenu.Item onclick={() => act(() => flow?.fit())}
								><Maximize />{m['workflows.editor.fit_view']()}</ContextMenu.Item
							>
							<ContextMenu.Separator />
							<ContextMenu.Item disabled={past.length === 0} onclick={() => act(() => undo())}
								><Undo2 />{m['workflows.editor.undo']()}<ContextMenu.Shortcut>⌘Z</ContextMenu.Shortcut
								></ContextMenu.Item
							>
							<ContextMenu.Item disabled={future.length === 0} onclick={() => act(() => redo())}
								><Redo2 />{m['workflows.editor.redo']()}<ContextMenu.Shortcut>⇧⌘Z</ContextMenu.Shortcut
								></ContextMenu.Item
							>
						{/if}
					</ContextMenu.Content>
				</ContextMenu.Root>

				<!-- The editing toolbar floats over the canvas. -->
				<Tooltip.Provider delayDuration={300}>
					<div
						class="absolute bottom-4 left-1/2 z-10 flex -translate-x-1/2 items-center gap-0.5 rounded-xl border bg-background/95 p-1 shadow-lg backdrop-blur"
						role="toolbar"
						aria-label={m['workflows.editor.toolbar']()}
						data-testid="workflow-toolbar"
					>
						<DropdownMenu.Root>
							<DropdownMenu.Trigger>
								{#snippet child({ props })}<Button {...props} size="sm" class="gap-1.5" data-testid="workflow-add-step"
										><Plus />{m['workflows.editor.palette']()}</Button
									>{/snippet}
							</DropdownMenu.Trigger>
							<DropdownMenu.Content side="top" align="start" class="w-72">
								{#each BLOCKS as type (type)}
									{@const Icon = blockIcon(type)}
									<DropdownMenu.Item onclick={() => addAt(type)} class="gap-3 py-2" data-testid="workflow-add-{type}">
										<span class={cn('flex size-7 shrink-0 items-center justify-center rounded-md', blockTone(type))}
											><Icon class="size-4" /></span
										>
										<span class="min-w-0">
											<span class="block text-sm font-medium">{blockLabel(type)}</span>
											<span class="block text-xs text-muted-foreground">{blockHint(type)}</span>
										</span>
									</DropdownMenu.Item>
								{/each}
							</DropdownMenu.Content>
						</DropdownMenu.Root>
						<span class="mx-1 h-5 w-px bg-border"></span>
						{#snippet tool(label: string, Icon: typeof Plus, action: () => void, disabled = false, shortcut = '', testid = '')}
							<Tooltip.Root>
								<Tooltip.Trigger>
									{#snippet child({ props })}<Button
											{...props}
											size="icon-sm"
											variant="ghost"
											aria-label={label}
											{disabled}
											data-testid={testid || undefined}
											onclick={action}><Icon /></Button
										>{/snippet}
								</Tooltip.Trigger>
								<Tooltip.Content side="top"
									>{label}{#if shortcut}<span class="ml-2 text-muted-foreground">{shortcut}</span>{/if}</Tooltip.Content
								>
							</Tooltip.Root>
						{/snippet}
						{@render tool(m['workflows.editor.undo'](), Undo2, undo, past.length === 0, '⌘Z')}
						{@render tool(m['workflows.editor.redo'](), Redo2, redo, future.length === 0, '⇧⌘Z')}
						<span class="mx-1 h-5 w-px bg-border"></span>
						{@render tool(m['workflows.editor.tidy'](), StretchHorizontal, () => void arrange(), false, '', 'workflow-arrange')}
						{@render tool(m['workflows.editor.zoom_out'](), ZoomOut, () => flow?.zoomOut())}
						{@render tool(m['workflows.editor.zoom_in'](), ZoomIn, () => flow?.zoomIn())}
						{@render tool(m['workflows.editor.fit_view'](), Maximize, () => flow?.fit())}
					</div>
				</Tooltip.Provider>
			{:else}
				{@render canvas()}
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
											...activeProfiles.map((p) => ({
												value: p.id,
												label: profileTitle(p)
											}))
										]}
									/>
								</div>
								<div class="space-y-1.5">
									<div class="flex items-center gap-1">
										<Label>{m['workflows.editor.node_prompt']()}</Label>
										<InfoHint>
											{m['workflows.editor.node_prompt_help']()}
											{#each VARIABLES as variable (variable)}<code
													class="mx-0.5 rounded bg-muted px-1 py-px font-mono text-[11px] text-foreground"
													>{variable}</code
												>{/each}
										</InfoHint>
									</div>
									{#key node.id}
										<AiMarkdownEditor
											bind:value={
												() => node.prompt ?? '',
												(value: string) => change(updateNode(graph, node.id, { prompt: value.slice(0, 4000) }))
											}
											{slug}
											kind="step_prompt"
											{editable}
											label={m['workflows.editor.node_prompt']()}
											class="min-h-32"
											testid="workflow-node-prompt"
										/>
									{/key}
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
							{#if retryCondition(node, profiles)}
								<div class="flex items-center justify-between gap-3 rounded-lg border px-3 py-2">
									<div class="flex items-center gap-1">
										<Label for="node-retry" class="text-sm">{m['workflows.editor.retry']()}</Label>
										<InfoHint text={m['workflows.editor.retry_help']()} />
									</div>
									<Switch
										id="node-retry"
										checked={retries(graph, node, profiles)}
										disabled={!editable}
										onCheckedChange={(on) => {
											lastChange = 0;
											change(setRetry(graph, node.id, on, profiles));
										}}
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
											'flex h-8 w-full min-w-0 items-center gap-2 rounded-md px-2 text-left text-sm font-normal outline-none hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring/50',
											selection?.id === node.id && 'bg-muted font-medium'
										)}
										aria-label={m['workflows.editor.select_named']({ name: node.label })}
										aria-pressed={selection?.kind === 'node' && selection.id === node.id}
										onclick={() => select({ kind: 'node', id: node.id })}
									>
										<span class={cn('flex size-5 shrink-0 items-center justify-center rounded', nodeTone(node))}
											><Icon class="size-3" /></span
										>
										<span class="min-w-0 flex-1 truncate">{node.label}</span>
										<span class="shrink-0 text-xs text-muted-foreground">{nodeTypeLabel(node.type)}</span>
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
											'flex w-full min-w-0 flex-col items-start gap-0.5 rounded-md px-2 py-1.5 text-left font-normal outline-none hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring/50',
											selection?.id === edge.id && 'bg-muted font-medium'
										)}
										aria-pressed={selection?.kind === 'edge' && selection.id === edge.id}
										onclick={() => select({ kind: 'edge', id: edge.id })}
									>
										<span class="block w-full truncate text-sm"
											>{m['workflows.editor.transition_named']({
												from: nodeName(edge.source),
												to: nodeName(edge.target)
											})}</span
										>
										<span class="block w-full truncate text-xs font-normal text-muted-foreground"
											>{conditionLabel(edge.condition)}</span
										>
									</Button>
								</li>
							{/each}
						</ul>
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

<AlertDialog.Root open={leaving !== null} onOpenChange={(open) => !open && (leaving = null)}>
	<AlertDialog.Content>
		<AlertDialog.Header>
			<AlertDialog.Title>{m['workflows.editor.leave_title']()}</AlertDialog.Title>
			<AlertDialog.Description>{m['workflows.editor.leave_unsaved']()}</AlertDialog.Description>
		</AlertDialog.Header>
		<AlertDialog.Footer>
			<AlertDialog.Cancel>{m['workflows.editor.stay']()}</AlertDialog.Cancel>
			<AlertDialog.Action variant="destructive" onclick={leave} data-testid="workflow-leave"
				>{m['workflows.editor.leave']()}</AlertDialog.Action
			>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>
