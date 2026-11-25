<script lang="ts">
	import { Label } from '$lib/components/ui/label/index.js';
	import SelectField from '$lib/components/shared/SelectField.svelte';
	import type { WorkflowList } from '$lib/api/workflows';
	import { m } from '$lib/features/i18n/messages';
	import {
		DEFAULT_WORKFLOW,
		defaultMayBeCustom,
		isCustomChoice,
		isPipelineChoice,
		workflowOptions
	} from './workflow-choice';

	// The workflow a task follows: the issue's default, Standard or a custom
	// workflow of the workspace. The task composer and the approval card of an
	// assistant delegation offer it the same way; the caller keeps the choice and
	// hides the harness and model pickers while a custom workflow is chosen.
	let {
		list,
		value = $bindable(DEFAULT_WORKFLOW),
		id,
		disabled = false,
		extra
	}: {
		list: WorkflowList;
		value?: string;
		id: string;
		disabled?: boolean;
		/** A workflow already chosen that the list no longer offers, such as one archived since. */
		extra?: { id: string; name: string };
	} = $props();
	const options = $derived(
		workflowOptions(list, { default: m['workflows.choose_default'](), standard: m['workflows.standard']() }, extra)
	);
	// A default may also name a custom workflow, whose first agent step decides;
	// on a pipeline, its writer role does.
	const noted = $derived(isCustomChoice(value) || (value === DEFAULT_WORKFLOW && defaultMayBeCustom(list)));
	const note = $derived(
		isPipelineChoice(list, value) ? m['workflows.composer_note_pipeline']() : m['workflows.composer_note']()
	);
</script>

<div class="grid min-w-0 gap-1.5">
	<Label for={id} class="text-xs leading-4 font-medium text-muted-foreground">{m['workflows.choose_label']()}</Label>
	<SelectField
		{id}
		bind:value
		{options}
		{disabled}
		aria-describedby={noted ? `${id}-note` : undefined}
		data-testid="task-workflow"
	/>
</div>
{#if noted}<p id="{id}-note" class="order-last basis-full text-xs text-muted-foreground">{note}</p>{/if}
