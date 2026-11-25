<script lang="ts">
	import { tick, untrack } from 'svelte';
	import { beforeNavigate, goto } from '$app/navigation';
	import { ArrowLeft, CircleAlert, FlaskConical, Plus, Save } from '@lucide/svelte';
	import * as Alert from '$lib/components/ui/alert/index.js';
	import * as AlertDialog from '$lib/components/ui/alert-dialog/index.js';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import * as Breadcrumb from '$lib/components/ui/breadcrumb/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import * as Tabs from '$lib/components/ui/tabs/index.js';
	import {
		workflows,
		type AgentProfile,
		type PipelineDefinition,
		type WorkflowIssue,
		type WorkflowSummary
	} from '$lib/api/workflows';
	import { appToast } from '$lib/features/toast/toast';
	import { cn } from '$lib/utils.js';
	import { m } from '$lib/features/i18n/messages';
	import type { FlowApi } from './FlowBridge.svelte';
	import WorkflowCanvas from './WorkflowCanvas.svelte';
	import WorkflowVersionBadge from './WorkflowVersionBadge.svelte';
	import PipelinePhaseForm from './PipelinePhaseForm.svelte';
	import PipelineProblems from './PipelineProblems.svelte';
	import PipelineRoster from './PipelineRoster.svelte';
	import PipelineSettings from './PipelineSettings.svelte';
	import { nodeIcon, nodeTone } from './node-style';
	import {
		PIPELINE_LIMITS,
		addPhase,
		editablePipeline,
		issueSection,
		phaseNodeType,
		pipelineForSave,
		pipelineGraph
	} from './pipeline-editor';
	import { ownerLabel, pipelineIssueLabel } from './pipeline-labels';

	// The editor of a pipeline workflow (KUA-284): the phases drawn on the
	// workflow canvas, left to right with no loop edges (an unmet check is
	// what sends work back), and a side panel with a form per phase, the
	// roster and the settings. The server validates as you edit and on save,
	// and its issues are shown where they are fixed. A stored pipeline saves
	// a new version; a draft (no id yet) is created as a new workflow on its
	// first save and never before.
	let {
		slug,
		workflow,
		profiles,
		editable,
		backHref,
		onsaved,
		oncreated
	}: {
		slug: string;
		workflow: {
			/** Absent for a draft nobody saved yet. */
			id?: string;
			name: string;
			description: string;
			latest_version: number;
			versions?: { version: number; created_at?: string }[];
			graph: PipelineDefinition;
		};
		profiles: AgentProfile[];
		editable: boolean;
		backHref: string;
		onsaved?: (saved: WorkflowSummary) => void;
		/** A draft was saved as a new workflow. */
		oncreated?: (created: WorkflowSummary) => void;
	} = $props();

	const isDraft = untrack(() => !workflow.id);
	let pipeline = $state<PipelineDefinition>(untrack(() => editablePipeline(workflow.graph)));
	let name = $state(untrack(() => workflow.name));
	let description = $state(untrack(() => workflow.description));
	let version = $state(untrack(() => workflow.latest_version));
	let versions = $state(untrack(() => workflow.versions ?? []));
	let selected = $state<string | null>(null);
	let tab = $state<'phases' | 'roles' | 'settings'>('phases');
	let issues = $state<WorkflowIssue[]>([]);
	let checking = $state(false);
	let saving = $state(false);
	// A draft is unsaved from the start.
	let dirty = $state(isDraft);
	let flow = $state<FlowApi>();
	let editingName = $state(false),
		draftName = $state(''),
		nameInput = $state<HTMLInputElement | null>(null),
		renameButton = $state<HTMLElement | null>(null);

	const graph = $derived(pipelineGraph(pipeline));
	const current = $derived(pipeline.phases.find((phase) => phase.id === selected) ?? pipeline.phases[0]);
	const saveDisabled = $derived(!editable || saving || !dirty || !name.trim());
	const issuesIn = (section: 'phases' | 'roles' | 'settings') =>
		issues.filter((issue) => issueSection(issue) === section);
	const phaseIssues = (id: string) => issues.filter((issue) => issue.node_id === id);
	const generalIssues = $derived(issuesIn('phases').filter((issue) => !issue.node_id));

	function change(next: PipelineDefinition) {
		if (next === pipeline) return;
		pipeline = next;
		dirty = true;
	}

	function select(id: string) {
		selected = id;
		tab = 'phases';
	}

	async function newPhase() {
		const added = addPhase(pipeline, m['workflows.pipeline_editor.new_phase'](), current?.id);
		change(added.pipeline);
		select(added.id);
		await tick();
		flow?.fit();
	}

	function pick(issue: WorkflowIssue) {
		tab = issueSection(issue);
		if (issue.node_id) selected = issue.node_id;
	}

	// The phase a card stands for: who owns it and how many checks it has.
	function describe(node: { id: string }) {
		const phase = pipeline.phases.find((candidate) => candidate.id === node.id);
		if (!phase) return '';
		const count = phase.checks?.length ?? 0;
		return [ownerLabel(phase.owner), count ? m['workflows.pipeline_editor.check_count']({ count }) : '']
			.filter(Boolean)
			.join(' · ');
	}

	// Server-side validation, debounced while editing.
	let validateTimer: ReturnType<typeof setTimeout> | undefined;
	let validationRun = 0;
	$effect(() => {
		const snapshot = pipelineForSave(pipeline);
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

	async function save() {
		if (saveDisabled) return;
		const saved = { pipeline, name, description };
		const input = { name: name.trim(), description: description.trim(), graph: pipelineForSave(pipeline) };
		saving = true;
		try {
			if (!workflow.id) {
				const created = await workflows.create(slug, input);
				dirty = false;
				appToast.success(m['workflows.created']());
				oncreated?.(created);
				return;
			}
			const result = await workflows.save(slug, workflow.id, { ...input, expected_version: version });
			version = result.latest_version;
			versions = [{ version, created_at: result.updated_at }, ...versions];
			dirty = pipeline !== saved.pipeline || name !== saved.name || description !== saved.description;
			issues = [];
			appToast.success(m['workflows.editor.saved']({ version: result.latest_version }));
			onsaved?.(result);
		} catch (error) {
			const body = error as { error?: { code?: string; issues?: WorkflowIssue[] } };
			if (body.error?.issues) issues = body.error.issues;
			if (body.error?.code === 'WORKFLOW_STALE') appToast.error(m['workflows.editor.stale']());
			else appToast.apiError(error, m['workflows.failed_save']());
		} finally {
			saving = false;
		}
	}

	function shortcut(event: KeyboardEvent) {
		if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 's') {
			event.preventDefault();
			void save();
		}
	}

	async function startRename() {
		draftName = name;
		editingName = true;
		await tick();
		nameInput?.focus();
		nameInput?.select();
	}

	async function renameKeydown(event: KeyboardEvent) {
		if (event.isComposing || (event.key !== 'Enter' && event.key !== 'Escape')) return;
		event.preventDefault();
		const next = draftName.trim();
		if (event.key === 'Enter' && next && next !== name) {
			name = next;
			dirty = true;
		}
		editingName = false;
		await tick();
		renameButton?.focus();
	}

	// Leaving with unsaved changes asks first, in a dialog.
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
</script>

<svelte:window onkeydown={shortcut} />

<div class="flex h-full min-h-0 flex-col" data-testid="pipeline-editor">
	<header class="flex h-14 shrink-0 items-center justify-between gap-4 border-b px-3 sm:gap-6 sm:px-4">
		<Breadcrumb.Root class="min-w-0">
			<Breadcrumb.List class="flex-nowrap gap-2 sm:gap-3">
				<Breadcrumb.Item class="min-w-0">
					<Breadcrumb.Link href={backHref} title={m['workflows.title']()} class="flex min-w-0 items-center gap-1.5">
						<ArrowLeft class="size-4 shrink-0" aria-hidden="true" />
						<span class="truncate">{m['workflows.title']()}</span>
					</Breadcrumb.Link>
				</Breadcrumb.Item>
				<Breadcrumb.Separator class="shrink-0">/</Breadcrumb.Separator>
				<Breadcrumb.Item aria-current="page" class="max-w-[38ch] min-w-[6ch] text-foreground">
					{#if editingName}
						<Input
							bind:ref={nameInput}
							bind:value={draftName}
							aria-label={m['workflows.name']()}
							maxlength={80}
							onkeydown={renameKeydown}
							onblur={() => (editingName = false)}
							class="field-sizing-content w-auto max-w-full min-w-[6ch] font-medium"
						/>
					{:else}
						<h1 aria-label={name} class="min-w-0 truncate text-sm font-medium">
							{#if editable}
								<Button
									variant="plain"
									bind:ref={renameButton}
									aria-label={m['workflows.editor.rename']({ name })}
									title={name}
									onclick={startRename}
									class="max-w-full truncate rounded-sm text-left underline-offset-4 hover:underline focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-ring"
									>{name}</Button
								>
							{:else}{name}{/if}
						</h1>
					{/if}
				</Breadcrumb.Item>
				<Breadcrumb.Item class="hidden sm:inline-flex">
					<Badge variant="secondary"><FlaskConical aria-hidden="true" />{m['workflows.pipeline']()}</Badge>
				</Breadcrumb.Item>
				{#if !editable}<Breadcrumb.Item class="hidden lg:inline-flex">
						<Badge variant="outline">{m['workflows.read_only']()}</Badge>
					</Breadcrumb.Item>{/if}
			</Breadcrumb.List>
		</Breadcrumb.Root>
		<div class="flex shrink-0 items-center gap-3 text-xs text-muted-foreground">
			{#if issues.length > 0}<PipelineProblems {issues} {pipeline} onpick={pick} />
			{:else if checking}<span class="sr-only sm:not-sr-only" role="status">{m['workflows.editor.checking']()}</span
				>{/if}
			{#if isDraft}<Badge variant="outline" data-testid="pipeline-unsaved"
					>{m['workflows.pipeline_editor.unsaved']()}</Badge
				>
			{:else}<WorkflowVersionBadge {version} {versions} {dirty} />{/if}
			{#if editable}<Button
					size="icon"
					aria-label={m['workflows.editor.save']()}
					title={m['workflows.editor.save']()}
					aria-keyshortcuts="Control+S Meta+S"
					disabled={saveDisabled}
					onclick={save}
					data-testid="pipeline-save"><Save aria-hidden="true" /></Button
				>{/if}
		</div>
	</header>

	<!-- As the graph editor: side by side, the canvas takes the height that is left and the
	     panel scrolls; stacked, the canvas keeps 22rem and this body scrolls to the full panel. -->
	<div
		class="grid min-h-0 flex-1 grid-rows-[minmax(22rem,1fr)_auto] overflow-y-auto lg:grid-cols-[minmax(0,1fr)_24rem] lg:grid-rows-1"
	>
		<div class="relative min-h-0 bg-muted/30" data-testid="pipeline-canvas-area">
			<WorkflowCanvas
				{graph}
				{profiles}
				issues={issues.filter((issue) => issue.node_id)}
				selection={current ? { kind: 'node', id: current.id } : null}
				bind:api={flow}
				class="h-full rounded-none border-0 bg-transparent"
				label={m['workflows.pipeline_editor.canvas']()}
				{describe}
				edgeLabels={false}
				onselect={(next) => next?.kind === 'node' && select(next.id)}
			/>
		</div>

		<aside
			class="flex flex-col border-t bg-background lg:min-h-0 lg:border-t-0 lg:border-l"
			aria-label={m['workflows.editor.inspector']()}
			data-testid="pipeline-panel"
		>
			<Tabs.Root bind:value={tab} class="flex min-h-0 flex-1 flex-col gap-0">
				<div class="border-b px-3 py-2">
					<Tabs.List class="w-full">
						<Tabs.Trigger value="phases" data-testid="pipeline-tab-phases"
							>{m['workflows.pipeline_phases']()}{#if issuesIn('phases').length}<CircleAlert
									class="text-destructive"
									aria-hidden="true"
								/>{/if}</Tabs.Trigger
						>
						<Tabs.Trigger value="roles" data-testid="pipeline-tab-roles"
							>{m['workflows.pipeline_roles']()}{#if issuesIn('roles').length}<CircleAlert
									class="text-destructive"
									aria-hidden="true"
								/>{/if}</Tabs.Trigger
						>
						<Tabs.Trigger value="settings" data-testid="pipeline-tab-settings"
							>{m['workflows.editor.tab_settings']()}{#if issuesIn('settings').length}<CircleAlert
									class="text-destructive"
									aria-hidden="true"
								/>{/if}</Tabs.Trigger
						>
					</Tabs.List>
				</div>

				<Tabs.Content value="phases" class="min-h-0 flex-1 space-y-4 overflow-y-auto p-4">
					{#if generalIssues.length}<Alert.Root variant="destructive" data-testid="pipeline-general-issues">
							<CircleAlert />
							<Alert.Description>
								{#each generalIssues as issue, i (i)}<span class="block">{pipelineIssueLabel(issue)}</span>{/each}
							</Alert.Description>
						</Alert.Root>{/if}
					<section class="space-y-1.5" aria-labelledby="pipeline-phase-list">
						<div class="flex items-center justify-between gap-2">
							<h3 id="pipeline-phase-list" class="text-xs font-medium text-muted-foreground">
								{m['workflows.pipeline_phases']()} · {pipeline.phases.length}
							</h3>
							{#if editable}<Button
									size="sm"
									variant="outline"
									disabled={pipeline.phases.length >= PIPELINE_LIMITS.phases}
									data-testid="pipeline-add-phase"
									onclick={newPhase}><Plus />{m['workflows.pipeline_editor.add_phase']()}</Button
								>{/if}
						</div>
						<ol class="space-y-px">
							{#each pipeline.phases as phase, i (phase.id)}
								{@const Icon = nodeIcon({ type: phaseNodeType(phase) })}
								<li>
									<Button
										variant="plain"
										class={cn(
											'flex h-8 w-full min-w-0 items-center gap-2 rounded-md px-2 text-left text-sm font-normal outline-none hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring/50',
											current?.id === phase.id && 'bg-muted font-medium'
										)}
										aria-pressed={current?.id === phase.id}
										data-testid="pipeline-phase-item-{phase.id}"
										onclick={() => select(phase.id)}
									>
										<span
											class={cn(
												'flex size-5 shrink-0 items-center justify-center rounded',
												nodeTone({ type: phaseNodeType(phase) })
											)}><Icon class="size-3" aria-hidden="true" /></span
										>
										<span class="min-w-0 flex-1 truncate">{i + 1}. {phase.label.trim() || phase.id}</span>
										{#if phaseIssues(phase.id).length}<CircleAlert
												class="size-3.5 shrink-0 text-destructive"
												aria-label={m['workflows.editor.problems']({ count: phaseIssues(phase.id).length })}
											/>{/if}
										<span class="shrink-0 text-xs text-muted-foreground">{ownerLabel(phase.owner)}</span>
									</Button>
								</li>
							{/each}
						</ol>
					</section>
					{#if current}
						<div class="border-t pt-4">
							{#key current.id}
								<PipelinePhaseForm
									{slug}
									{pipeline}
									phase={current}
									issues={phaseIssues(current.id)}
									{editable}
									onchange={change}
								/>
							{/key}
						</div>
					{/if}
				</Tabs.Content>

				<Tabs.Content value="roles" class="min-h-0 flex-1 overflow-y-auto p-4">
					<PipelineRoster {pipeline} {profiles} issues={issuesIn('roles')} {editable} onchange={change} />
				</Tabs.Content>

				<Tabs.Content value="settings" class="min-h-0 flex-1 overflow-y-auto p-4">
					<PipelineSettings
						{pipeline}
						bind:description
						issues={issuesIn('settings')}
						{editable}
						onchange={change}
						ondescription={() => (dirty = true)}
					/>
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
			<AlertDialog.Action variant="destructive" onclick={leave} data-testid="pipeline-leave"
				>{m['workflows.editor.leave']()}</AlertDialog.Action
			>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>
