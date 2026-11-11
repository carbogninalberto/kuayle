import type { AgentTask } from '$lib/api/agent-tasks';

// How a task's workflow stands towards a person's pause: it can be paused,
// a pause is taking effect (the agent is being stopped, or the current step
// finishes first), or it is paused and waits to be resumed.
export type WorkflowPause = 'none' | 'pausable' | 'pausing' | 'paused';

type PauseFields = Pick<
	AgentTask,
	'state' | 'kind' | 'workflow_version_id' | 'workflow_status' | 'workflow_paused_at' | 'workflow_pause_request'
>;

const running = ['queued', 'preparing', 'running', 'waiting_input', 'validating', 'publishing', 'awaiting_review'];

export function workflowPause(task: PauseFields): WorkflowPause {
	if (!task.workflow_version_id || task.kind === 'review') return 'none';
	if (['completed', 'failed', 'cancelled'].includes(task.state)) return 'none';
	if (task.workflow_paused_at) return task.state === 'cancelling' ? 'pausing' : 'paused';
	if (task.workflow_pause_request) return 'pausing';
	const status = task.workflow_status;
	return running.includes(task.state) && (status === 'running' || status === 'waiting') ? 'pausable' : 'none';
}

// A pause still taking effect can be withdrawn unless the agent is already
// being stopped.
export function canResume(task: PauseFields): boolean {
	const pause = workflowPause(task);
	return pause === 'paused' || (pause === 'pausing' && task.state !== 'cancelling');
}
