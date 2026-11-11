import type { AgentTask, WorkflowSwitch } from '$lib/api/agent-tasks';

// A task is pinned to the version of its workflow it started on. When the
// workflow has a newer one, its creator can move the task to it: at once
// when the task is paused or waiting and the newer version still has the
// step it is on, otherwise after pausing, or not at all.
export type WorkflowUpdate =
	| { kind: 'none' }
	| { kind: 'ready'; version: number; step: string }
	| { kind: 'pause_first'; version: number }
	| { kind: 'step_removed' | 'step_changed'; version: number; current: number };

type SwitchFields = Pick<AgentTask, 'state' | 'kind' | 'workflow_version_id'>;

/** Whether it is worth asking the server about a newer version at all. */
export function mayHaveWorkflowUpdate(task: SwitchFields): boolean {
	return (
		!!task.workflow_version_id && task.kind !== 'review' && !['completed', 'failed', 'cancelled'].includes(task.state)
	);
}

export function workflowUpdate(plan: WorkflowSwitch | null | undefined): WorkflowUpdate {
	if (!plan || plan.to_version <= plan.from_version) return { kind: 'none' };
	const version = plan.to_version;
	switch (plan.refused) {
		case undefined:
		case null:
			return { kind: 'ready', version, step: plan.node_label || plan.node || '' };
		case 'not_at_rest':
			return { kind: 'pause_first', version };
		case 'step_removed':
		case 'step_changed':
			return { kind: plan.refused, version, current: plan.from_version };
		default:
			return { kind: 'none' };
	}
}
