<script lang="ts">
	import { ArrowLeft, ArrowRight, CircleAlert, Plus, Trash2 } from '@lucide/svelte';
	import * as Alert from '$lib/components/ui/alert/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import { Switch } from '$lib/components/ui/switch/index.js';
	import SelectField from '$lib/components/shared/SelectField.svelte';
	import type { PipelineDefinition, PipelinePhase, WorkflowIssue } from '$lib/api/workflows';
	import { cn } from '$lib/utils.js';
	import { m } from '$lib/features/i18n/messages';
	import AiMarkdownEditor from './AiMarkdownEditor.svelte';
	import PipelineCheckRow from './PipelineCheckRow.svelte';
	import { nodeIcon, nodeTone } from './node-style';
	import {
		HUMAN,
		KUAYLE,
		PIPELINE_LIMITS,
		addCheck,
		canMove,
		movePhase,
		phaseNodeType,
		removePhase,
		updatePhase
	} from './pipeline-editor';
	import { ownerLabel, pipelineIssueLabel } from './pipeline-labels';

	// One phase of a pipeline: its name, who owns it, the prompt the writer's
	// turns get in it and the checks that must hold to move on. Only the last
	// phase can be the guarded merge. The validator's issues about the phase
	// are shown at its top. The editor mounts one per phase it shows.
	let {
		slug,
		pipeline,
		phase,
		issues,
		editable,
		onchange
	}: {
		slug: string;
		pipeline: PipelineDefinition;
		phase: PipelinePhase;
		issues: WorkflowIssue[];
		editable: boolean;
		onchange: (next: PipelineDefinition) => void;
	} = $props();

	const index = $derived(pipeline.phases.findIndex((candidate) => candidate.id === phase.id));
	const last = $derived(index === pipeline.phases.length - 1);
	const Icon = $derived(nodeIcon({ type: phaseNodeType(phase) }));
	const checks = $derived(phase.checks ?? []);
	const owners = $derived([
		...new Set([...pipeline.roster.map((member) => member.role).filter(Boolean), KUAYLE, HUMAN, phase.owner])
	]);
	const change = (patch: Partial<PipelinePhase>) => onchange(updatePhase(pipeline, phase.id, patch));
</script>

<div class="space-y-4" data-testid="pipeline-phase-form">
	<div class="flex items-center gap-2.5">
		<span
			class={cn(
				'flex size-8 shrink-0 items-center justify-center rounded-lg',
				nodeTone({ type: phaseNodeType(phase) })
			)}><Icon class="size-4" aria-hidden="true" /></span
		>
		<div class="min-w-0 flex-1">
			<p class="truncate text-sm font-medium">{phase.label.trim() || phase.id}</p>
			<p class="truncate text-xs text-muted-foreground">
				{index + 1}/{pipeline.phases.length} · <code>{phase.id}</code>
			</p>
		</div>
		{#if editable}
			<div class="flex shrink-0 gap-0.5">
				<Button
					size="icon-sm"
					variant="ghost"
					aria-label={m['workflows.pipeline_editor.move_earlier']()}
					title={m['workflows.pipeline_editor.move_earlier']()}
					disabled={!canMove(pipeline, phase.id, -1)}
					onclick={() => onchange(movePhase(pipeline, phase.id, -1))}><ArrowLeft /></Button
				>
				<Button
					size="icon-sm"
					variant="ghost"
					aria-label={m['workflows.pipeline_editor.move_later']()}
					title={m['workflows.pipeline_editor.move_later']()}
					disabled={!canMove(pipeline, phase.id, 1)}
					onclick={() => onchange(movePhase(pipeline, phase.id, 1))}><ArrowRight /></Button
				>
				<Button
					size="icon-sm"
					variant="ghost"
					class="text-muted-foreground hover:text-destructive"
					aria-label={m['workflows.pipeline_editor.delete_phase']()}
					title={m['workflows.pipeline_editor.delete_phase']()}
					disabled={pipeline.phases.length <= 1}
					onclick={() => onchange(removePhase(pipeline, phase.id))}><Trash2 /></Button
				>
			</div>
		{/if}
	</div>

	{#if issues.length}<Alert.Root variant="destructive" data-testid="pipeline-phase-issues">
			<CircleAlert />
			<Alert.Description>
				{#each issues as issue, i (i)}<span class="block">{pipelineIssueLabel(issue)}</span>{/each}
			</Alert.Description>
		</Alert.Root>{/if}

	<div class="space-y-1.5">
		<Label for="phase-label">{m['workflows.editor.node_label']()}</Label>
		<Input
			id="phase-label"
			value={phase.label}
			maxlength={80}
			disabled={!editable}
			oninput={(event) => change({ label: event.currentTarget.value })}
		/>
	</div>
	<div class="space-y-1.5">
		<Label for="phase-owner">{m['workflows.pipeline_editor.owner']()}</Label>
		<SelectField
			id="phase-owner"
			bind:value={() => phase.owner, (owner: string) => change({ owner })}
			disabled={!editable}
			options={owners.map((owner) => ({ value: owner, label: ownerLabel(owner) }))}
		/>
	</div>
	<div class="space-y-1.5">
		<Label>{m['workflows.pipeline_editor.prompt']()}</Label>
		<AiMarkdownEditor
			bind:value={
				() => phase.prompt ?? '', (prompt: string) => change({ prompt: prompt.slice(0, PIPELINE_LIMITS.promptBytes) })
			}
			{slug}
			kind="step_prompt"
			{editable}
			label={m['workflows.pipeline_editor.prompt']()}
			class="min-h-28"
			testid="pipeline-phase-prompt"
		/>
		<p class="text-xs text-muted-foreground">{m['workflows.pipeline_editor.prompt_help']()}</p>
	</div>
	{#if last}
		<div class="space-y-1 rounded-lg border px-3 py-2">
			<div class="flex items-center justify-between gap-3">
				<Label for="phase-guarded" class="text-sm">{m['workflows.pipeline_editor.guarded']()}</Label>
				<Switch
					id="phase-guarded"
					checked={!!phase.guarded}
					disabled={!editable}
					aria-describedby="phase-guarded-help"
					onCheckedChange={(on) => change(on ? { guarded: true, owner: KUAYLE } : { guarded: false })}
				/>
			</div>
			<p id="phase-guarded-help" class="text-xs text-muted-foreground">
				{m['workflows.pipeline_editor.guarded_help']()}
			</p>
		</div>
	{/if}

	<section class="space-y-2" aria-labelledby="phase-checks">
		<div class="flex items-center justify-between gap-2">
			<h3 id="phase-checks" class="text-sm font-medium">{m['workflows.pipeline_editor.checks']()}</h3>
			{#if editable}<Button
					size="sm"
					variant="outline"
					disabled={checks.length >= PIPELINE_LIMITS.checks}
					data-testid="pipeline-add-check"
					onclick={() => onchange(addCheck(pipeline, phase.id).pipeline)}
					><Plus />{m['workflows.pipeline_editor.add_check']()}</Button
				>{/if}
		</div>
		<p class="text-xs text-muted-foreground">{m['workflows.pipeline_editor.checks_help']()}</p>
		{#if checks.length}
			<ul class="space-y-2" role="list">
				{#each checks as check (check.id)}
					<PipelineCheckRow {pipeline} phaseId={phase.id} {check} {editable} {onchange} />
				{/each}
			</ul>
		{/if}
	</section>
</div>
