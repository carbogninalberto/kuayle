import { m } from '$lib/paraglide/messages.js';
import type { TaskState } from '$lib/api/agent-tasks';
export function taskLabel(state: TaskState): string {
	const labels: Record<TaskState, () => string> = {
		queued: m['tasks.state.queued'],
		preparing: m['tasks.state.preparing'],
		running: m['tasks.state.running'],
		waiting_input: m['tasks.state.waiting_input'],
		validating: m['tasks.state.validating'],
		publishing: m['tasks.state.publishing'],
		awaiting_review: m['tasks.state.awaiting_review'],
		blocked: m['tasks.state.blocked'],
		completed: m['tasks.state.completed'],
		failed: m['tasks.state.failed'],
		cancelling: m['tasks.state.cancelling'],
		cancelled: m['tasks.state.cancelled']
	};
	return labels[state]?.() ?? state;
}
