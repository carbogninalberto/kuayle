import type { WorkflowList, WorkflowSummary } from '$lib/api/workflows';

// The workflow a task follows is chosen where it starts: in the task composer
// and on an assistant delegation's approval card. Both offer the issue's
// default, Standard and the workspace's pipelines. The scopes' defaults and
// the automations choose among the same workflows (startable).

/** The choice that leaves the workflow to the issue's default. */
export const DEFAULT_WORKFLOW = 'default';

export interface WorkflowOption {
	value: string;
	label: string;
}

/**
 * Whether tasks can be started on a workflow besides Standard: a pipeline
 * that is not archived, only while the server's switch is on (the server
 * refuses one otherwise, and refuses a graph: graph workflows were removed).
 */
export function startable(list: WorkflowList | null | undefined, w: WorkflowSummary): boolean {
	return !w.archived && w.graph?.kind === 'pipeline' && !!list?.pipelines_enabled;
}

/** The pipelines a task can be started on besides Standard. */
export function customWorkflows(list: WorkflowList | null | undefined): WorkflowSummary[] {
	return (list?.workflows ?? []).filter((w) => !w.builtin && startable(list, w));
}

/** A pipeline's writer role decides the harness and model. */
export function isCustomChoice(choice: string): boolean {
	return choice !== DEFAULT_WORKFLOW && choice !== 'standard';
}

/** A default of the workspace, a team or a project may itself be a pipeline. */
export function defaultMayBeCustom(list: WorkflowList | null | undefined): boolean {
	return (list?.defaults ?? []).some((d) => d.workflow_id);
}

/**
 * The options of the picker. extra is a workflow already chosen that the list
 * no longer offers (archived since it was proposed), so it still shows its name.
 */
export function workflowOptions(
	list: WorkflowList | null | undefined,
	labels: { default: string; standard: string },
	extra?: { id: string; name: string }
): WorkflowOption[] {
	const custom = customWorkflows(list);
	const options = [
		{ value: DEFAULT_WORKFLOW, label: labels.default },
		{ value: 'standard', label: labels.standard },
		...custom.map((w) => ({ value: w.id, label: w.name }))
	];
	if (extra && extra.id !== 'standard' && !custom.some((w) => w.id === extra.id))
		options.push({ value: extra.id, label: extra.name || extra.id });
	return options;
}

/**
 * The workflow_id an approval decision carries: nothing while the choice is the
 * one the assistant proposed, "" for the issue's default, else the workflow.
 */
export function workflowEdit(choice: string, proposed: string | undefined): { workflow_id: string } | undefined {
	const wanted = choice === DEFAULT_WORKFLOW ? '' : choice;
	return wanted === (proposed ?? '') ? undefined : { workflow_id: wanted };
}
