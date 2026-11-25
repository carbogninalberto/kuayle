<script lang="ts">
	import { Trash2 } from '@lucide/svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import { Textarea } from '$lib/components/ui/textarea/index.js';
	import * as ToggleGroup from '$lib/components/ui/toggle-group/index.js';
	import SelectField from '$lib/components/shared/SelectField.svelte';
	import type { PipelineCheck, PipelineDefinition } from '$lib/api/workflows';
	import { m } from '$lib/features/i18n/messages';
	import { PIPELINE_FACTS, PIPELINE_LIMITS, removeCheck, setWaiver, updateCheck, waiver } from './pipeline-editor';
	import { factLabel, ownerLabel } from './pipeline-labels';

	// One check of a phase: the fact it requires (with the role whose verdict
	// it is, or the command a script runs), whether it is required or only
	// advisory, and for a required one who may waive it.
	let {
		pipeline,
		phaseId,
		check,
		editable,
		onchange
	}: {
		pipeline: PipelineDefinition;
		phaseId: string;
		check: PipelineCheck;
		editable: boolean;
		onchange: (next: PipelineDefinition) => void;
	} = $props();

	const key = $derived(`check-${check.id}`);
	const change = (patch: Partial<PipelineCheck>) => onchange(updateCheck(pipeline, phaseId, check.id, patch));
	// A verdict comes from a role that does not write the code; the one it
	// names stays offered while it is being fixed.
	const reviewers = $derived([
		...new Set([
			...pipeline.roster.filter((member) => !member.writer && member.role).map((member) => member.role),
			...(check.role ? [check.role] : [])
		])
	]);
</script>

<li class="space-y-3 rounded-lg border bg-card p-3" data-testid="pipeline-check-{check.id}">
	<div class="flex items-end gap-2">
		<div class="min-w-0 flex-1 space-y-1.5">
			<div class="flex items-baseline justify-between gap-2">
				<Label for="{key}-fact">{m['workflows.pipeline_editor.fact']()}</Label>
				<code class="truncate text-[11px] text-muted-foreground">{check.id}</code>
			</div>
			<SelectField
				id="{key}-fact"
				bind:value={() => check.fact, (fact: string) => change({ fact })}
				disabled={!editable}
				options={PIPELINE_FACTS.map((fact) => ({ value: fact, label: factLabel(fact) }))}
			/>
		</div>
		{#if editable}<Button
				size="icon"
				variant="ghost"
				class="shrink-0 text-muted-foreground hover:text-destructive"
				aria-label={m['workflows.pipeline_editor.remove_check']({ id: check.id })}
				title={m['workflows.pipeline_editor.remove_check']({ id: check.id })}
				onclick={() => onchange(removeCheck(pipeline, phaseId, check.id))}><Trash2 /></Button
			>{/if}
	</div>
	{#if check.fact === 'verdict'}
		<div class="space-y-1.5">
			<Label for="{key}-role">{m['workflows.pipeline_editor.verdict_role']()}</Label>
			<SelectField
				id="{key}-role"
				bind:value={() => check.role ?? '', (role: string) => change({ role })}
				disabled={!editable}
				options={[
					{ value: '', label: m['workflows.pipeline_editor.choose_role']() },
					...reviewers.map((role) => ({ value: role, label: role }))
				]}
			/>
		</div>
	{:else if check.fact === 'script'}
		<div class="space-y-1.5">
			<Label for="{key}-script">{m['workflows.pipeline_editor.script']()}</Label>
			<Textarea
				id="{key}-script"
				value={check.script ?? ''}
				rows={2}
				maxlength={PIPELINE_LIMITS.promptBytes}
				spellcheck={false}
				class="font-mono text-xs"
				disabled={!editable}
				oninput={(event) => change({ script: event.currentTarget.value })}
			/>
		</div>
	{/if}
	<div class="flex flex-wrap items-end gap-3">
		<ToggleGroup.Root
			type="single"
			variant="outline"
			disabled={!editable}
			aria-label={m['workflows.pipeline_editor.check_kind']()}
			data-testid="{key}-required"
			bind:value={
				() => (check.required ? 'required' : 'advisory'),
				(value: string) => {
					// One is always chosen: clicking the chosen one keeps it.
					if (value) change({ required: value === 'required' });
				}
			}
		>
			<ToggleGroup.Item value="required">{m['workflows.pipeline_editor.required']()}</ToggleGroup.Item>
			<ToggleGroup.Item value="advisory">{m['workflows.pipeline_editor.advisory']()}</ToggleGroup.Item>
		</ToggleGroup.Root>
		{#if check.required}
			<div class="min-w-40 flex-1 space-y-1.5">
				<Label for="{key}-waiver">{m['workflows.pipeline_editor.waiver']()}</Label>
				<SelectField
					id="{key}-waiver"
					bind:value={
						() => waiver(pipeline, check.id), (who: 'human' | 'lead') => onchange(setWaiver(pipeline, check.id, who))
					}
					disabled={!editable}
					options={[
						{ value: 'human', label: ownerLabel('human') },
						...(pipeline.lead || waiver(pipeline, check.id) === 'lead'
							? [{ value: 'lead' as const, label: ownerLabel('lead') }]
							: [])
					]}
				/>
			</div>
		{/if}
	</div>
</li>
