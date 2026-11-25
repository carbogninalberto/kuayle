<script lang="ts">
	import { CircleAlert } from '@lucide/svelte';
	import * as Alert from '$lib/components/ui/alert/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import { Textarea } from '$lib/components/ui/textarea/index.js';
	import SelectField from '$lib/components/shared/SelectField.svelte';
	import type { PipelineDefinition, WorkflowIssue } from '$lib/api/workflows';
	import { m } from '$lib/features/i18n/messages';
	import { HUMAN, LEAD, PIPELINE_LIMITS } from './pipeline-editor';
	import { ownerLabel, pipelineIssueLabel } from './pipeline-labels';

	// The pipeline's description, who approves merging (a person, or the lead
	// when the owner grants it) and its budget per task. Who may waive each
	// check is set on the check.
	let {
		pipeline,
		description = $bindable(),
		issues,
		editable,
		onchange,
		ondescription
	}: {
		pipeline: PipelineDefinition;
		description: string;
		issues: WorkflowIssue[];
		editable: boolean;
		onchange: (next: PipelineDefinition) => void;
		ondescription: () => void;
	} = $props();

	const whole = (value: string) => {
		const parsed = Math.floor(Number(value));
		return Number.isFinite(parsed) && parsed > 0 ? parsed : 0;
	};
	const budget = $derived(pipeline.budget ?? {});
	const setBudget = (patch: NonNullable<PipelineDefinition['budget']>) =>
		onchange({ ...pipeline, budget: { ...budget, ...patch } });
	const approver = $derived(pipeline.autonomy?.approve === LEAD ? LEAD : HUMAN);
</script>

<div class="space-y-6" data-testid="pipeline-settings">
	{#if issues.length}<Alert.Root variant="destructive" data-testid="pipeline-settings-issues">
			<CircleAlert />
			<Alert.Description>
				{#each issues as issue, i (i)}<span class="block">{pipelineIssueLabel(issue)}</span>{/each}
			</Alert.Description>
		</Alert.Root>{/if}
	<div class="space-y-1.5">
		<Label for="pipeline-description">{m['workflows.description']()}</Label>
		<Textarea
			id="pipeline-description"
			bind:value={description}
			maxlength={500}
			rows={3}
			disabled={!editable}
			oninput={ondescription}
		/>
	</div>
	<section class="space-y-3" aria-labelledby="pipeline-autonomy">
		<h3 id="pipeline-autonomy" class="text-sm font-medium">{m['workflows.pipeline_editor.autonomy']()}</h3>
		<div class="space-y-1.5">
			<Label for="pipeline-approver">{m['workflows.pipeline_editor.approver']()}</Label>
			<SelectField
				id="pipeline-approver"
				bind:value={
					() => approver, (who: string) => onchange({ ...pipeline, autonomy: { ...pipeline.autonomy, approve: who } })
				}
				disabled={!editable}
				options={[
					{ value: HUMAN, label: ownerLabel(HUMAN) },
					...(pipeline.lead || approver === LEAD ? [{ value: LEAD, label: ownerLabel(LEAD) }] : [])
				]}
			/>
		</div>
	</section>
	<section class="space-y-3" aria-labelledby="pipeline-budget">
		<h3 id="pipeline-budget" class="text-sm font-medium">{m['workflows.editor.budget']()}</h3>
		<div class="grid grid-cols-2 gap-3">
			<div class="space-y-1.5">
				<Label for="pipeline-max-turns">{m['workflows.pipeline_editor.max_turns']()}</Label>
				<Input
					id="pipeline-max-turns"
					type="number"
					min={0}
					max={PIPELINE_LIMITS.turns}
					value={budget.max_turns ?? 0}
					disabled={!editable}
					oninput={(event) => setBudget({ max_turns: whole(event.currentTarget.value) })}
				/>
			</div>
			<div class="space-y-1.5">
				<Label for="pipeline-max-minutes">{m['workflows.editor.budget_minutes']()}</Label>
				<Input
					id="pipeline-max-minutes"
					type="number"
					min={0}
					max={PIPELINE_LIMITS.minutes}
					value={budget.max_minutes ?? 0}
					disabled={!editable}
					oninput={(event) => setBudget({ max_minutes: whole(event.currentTarget.value) })}
				/>
			</div>
		</div>
		<div class="space-y-1.5">
			<Label for="pipeline-on-exhausted">{m['workflows.editor.budget_on_exhausted']()}</Label>
			<SelectField
				id="pipeline-on-exhausted"
				bind:value={() => budget.on_exhausted ?? 'fail', (value: 'fail' | 'ask') => setBudget({ on_exhausted: value })}
				disabled={!editable}
				options={[
					{ value: 'fail' as const, label: m['workflows.editor.on_fail']() },
					{ value: 'ask' as const, label: m['workflows.editor.on_ask']() }
				]}
			/>
		</div>
		<p class="text-xs leading-relaxed text-muted-foreground">{m['workflows.pipeline_editor.budget_help']()}</p>
	</section>
</div>
