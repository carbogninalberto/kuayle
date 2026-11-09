import type { AgentTask } from '$lib/api/agent-tasks';

// One visual tone per task state, drawn from the active theme's tokens so
// icons and badges match every light and dark theme.
export type TaskTone = 'success' | 'running' | 'review' | 'warning' | 'error' | 'neutral';

export function taskTone(state: AgentTask['state'] | string): TaskTone {
	switch (state) {
		case 'completed':
			return 'success';
		case 'preparing':
		case 'running':
		case 'validating':
		case 'publishing':
		case 'cancelling':
			return 'running';
		case 'awaiting_review':
			return 'review';
		case 'blocked':
			return 'warning';
		case 'failed':
			return 'error';
		default:
			return 'neutral';
	}
}

// Review waits on a person, so it shares the yellow warning tone.
export const toneText: Record<TaskTone, string> = {
	success: 'text-[var(--status-success)]',
	running: 'text-[var(--status-running)]',
	review: 'text-[var(--status-warning)]',
	warning: 'text-[var(--status-warning)]',
	error: 'text-[var(--status-error)]',
	neutral: 'text-muted-foreground'
};

export const toneBadge: Record<TaskTone, string> = {
	success: 'border-transparent bg-[var(--status-success)]/15 text-[var(--status-success)]',
	running: 'border-transparent bg-[var(--status-running)]/15 text-[var(--status-running)]',
	review: 'border-transparent bg-[var(--status-warning)]/15 text-[var(--status-warning)]',
	warning: 'border-transparent bg-[var(--status-warning)]/15 text-[var(--status-warning)]',
	error: 'border-transparent bg-[var(--status-error)]/15 text-[var(--status-error)]',
	neutral: 'text-muted-foreground'
};

// The status dot beside a task's state label, in the state's tone.
export const toneDot: Record<TaskTone, string> = {
	success: 'bg-[var(--status-success)]',
	running: 'bg-[var(--status-running)] animate-pulse motion-reduce:animate-none',
	review: 'bg-[var(--status-warning)]',
	warning: 'bg-[var(--status-warning)]',
	error: 'bg-[var(--status-error)]',
	neutral: 'bg-muted-foreground/50'
};

// The status dot of a task row: running, waiting for you, failed, or done.
export const indicatorDot: Record<'running' | 'waiting' | 'failed' | 'done', string> = {
	running: 'bg-[var(--status-running)] animate-pulse motion-reduce:animate-none',
	waiting: 'bg-[var(--status-warning)]',
	failed: 'bg-[var(--status-error)]',
	done: 'bg-muted-foreground/40'
};
