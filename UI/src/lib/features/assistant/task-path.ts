import type { AgentTask, TaskTransition } from '$lib/api/agent-tasks';
import type { WorkflowPauseEvent } from '$lib/api/workflows';
import type { TimelineStep } from './agent-timeline';

// The path a task took, for the sidebar: the steps of its timeline (see
// agent-timeline.ts) with what a reader wants at a glance. Where it went
// next and why, what a waiting step waits for, where and why it stopped,
// and the pauses, resumes and moves to a newer workflow version where they
// happened. A task without a workflow is its attempts, each with its state
// changes. Compact is each step once, as it last went.

/** What the step the task is on waits for. */
export type PathWait =
	| { kind: 'ci' | 'merge' | 'paused' }
	| { kind: 'approval'; question: string }
	// A limit the workflow stopped at, or whatever the task itself says.
	| { kind: 'limit' | 'other'; text: string };

export interface PathStep {
	kind: 'step';
	/** The step's anchor in the agent view. */
	key: string;
	step: TimelineStep;
	/** The step the task went to from here; its condition is why. */
	next?: TimelineStep;
	wait?: PathWait;
	/** Why the task stopped here. */
	stopped?: string;
	/** A task without a workflow: the state changes of this attempt. */
	changes: { state: string; reason: string; at: string }[];
}

export interface PathEvent {
	kind: 'event';
	key: string;
	action: 'paused' | 'resumed' | 'moved';
	at: string;
	/** A move: the workflow versions it went from and to. */
	from?: number;
	to?: number;
}

export type PathEntry = PathStep | PathEvent;

type Task = Pick<AgentTask, 'state' | 'reason' | 'generation'> & { workflow_paused_at?: string | null };

function waitsFor(step: TimelineStep, task: Task, status: string | undefined): PathWait | undefined {
	// An attempt that ended leaves its task waiting: for a review, or blocked.
	const idle = !step.nodeId && ['awaiting_review', 'blocked', 'waiting_input'].includes(task.state);
	if (!step.current || (step.status !== 'waiting' && !idle)) return undefined;
	if (task.workflow_paused_at) return { kind: 'paused' };
	if (status === 'paused') return { kind: 'limit', text: task.reason };
	if (task.state === 'awaiting_review') {
		if (step.nodeType === 'approval') return { kind: 'approval', question: step.question ?? '' };
		if (step.nodeType === 'merge') return { kind: 'merge' };
		if (step.nodeType === 'check' && step.check === 'ci') return { kind: 'ci' };
	}
	return { kind: 'other', text: task.reason };
}

type Changes = PathStep['changes'];

// Each attempt starts with the task being queued, so the nth "queued" opens
// the task's nth attempt (its generation). An attempt is paired with its
// changes by that number, never by position: one that failed before a run
// was recorded for it (no machine) has changes and no step.
function attemptChanges(transitions: TaskTransition[]): { from: number; changes: Changes }[] {
	const groups: { from: number; changes: Changes }[] = [];
	for (const change of [...transitions].sort((a, b) => a.sequence - b.sequence)) {
		if (change.to_state === 'queued' || !groups.length) groups.push({ from: Date.parse(change.created_at), changes: [] });
		groups.at(-1)!.changes.push({ state: change.to_state, reason: change.reason, at: change.created_at });
	}
	return groups;
}

// The generation of each attempt, oldest first. The one the task is on is
// the task's own; an earlier one is the attempt that was queued last before
// its run was recorded. Without the state changes they are counted back
// from the task's.
function attemptGenerations(steps: TimelineStep[], generation: number, groups: { from: number }[]): number[] {
	return steps.map((step, index) => {
		if (index === steps.length - 1) return Math.max(1, generation);
		const recorded = Date.parse(step.runs[0]?.created_at ?? step.startedAt);
		const queued = groups.findLastIndex((group) => group.from <= recorded);
		return queued >= 0 ? queued + 1 : Math.max(1, generation - (steps.length - 1 - index));
	});
}

const eventAction: Partial<Record<WorkflowPauseEvent['action'], PathEvent['action']>> = {
	paused: 'paused',
	resumed: 'resumed',
	version_switched: 'moved'
};

/** The whole path, oldest first: every step, with what happened between them. */
export function taskPath(
	steps: TimelineStep[],
	context: {
		task: Task;
		/** The workflow's own status ("paused" is a limit it stopped at). */
		status?: string;
		pauses?: WorkflowPauseEvent[];
		/** A task without a workflow: its state changes. */
		transitions?: TaskTransition[];
	}
): PathEntry[] {
	const { task } = context;
	const workflow = steps.some((step) => step.nodeId);
	const groups = workflow ? [] : attemptChanges(context.transitions ?? []);
	const generations = workflow ? [] : attemptGenerations(steps, task.generation, groups);
	const events = (context.pauses ?? [])
		.flatMap((event, index): PathEvent[] => {
			const action = eventAction[event.action];
			return action
				? [{ kind: 'event', key: `${action}-${index}`, action, at: event.at, from: event.from_version, to: event.to_version }]
				: [];
		})
		.sort((a, b) => Date.parse(a.at) - Date.parse(b.at));
	const entries: PathEntry[] = [];
	let placed = 0;
	steps.forEach((step, index) => {
		// What happened before this step began belongs to the one before it.
		while (index > 0 && placed < events.length && Date.parse(events[placed].at) < Date.parse(step.startedAt))
			entries.push(events[placed++]);
		const last = index === steps.length - 1;
		entries.push({
			kind: 'step',
			key: step.anchor,
			// An attempt is numbered like a visit: the task's own is the newest.
			step: workflow ? step : { ...step, visit: generations[index] },
			next: steps[index + 1]?.nodeId ? steps[index + 1] : undefined,
			wait: waitsFor(step, task, context.status),
			stopped: last && task.state === 'failed' ? task.reason || step.detail || step.reason || undefined : undefined,
			changes: groups[generations[index] - 1]?.changes ?? []
		});
	});
	entries.push(...events.slice(placed));
	return entries;
}

/**
 * What is shown before the reader asks for more: each step once, as it last
 * went, in the order the task last passed them, without the start. Attempts
 * are the last outcome and the one the task is on.
 */
export function compactPath(entries: PathEntry[]): PathStep[] {
	const steps = entries.filter((entry): entry is PathStep => entry.kind === 'step');
	if (!steps.some((entry) => entry.step.nodeId)) return steps.slice(-2);
	const latest = new Map<string, PathStep>();
	for (const entry of steps) latest.set(entry.step.nodeId ?? entry.key, entry);
	const kept = steps.filter((entry) => latest.get(entry.step.nodeId ?? entry.key) === entry);
	const beyondStart = kept.filter((entry) => entry.step.nodeType !== 'start');
	return beyondStart.length ? beyondStart : kept;
}

/** How long a step took, rounded for a glance: seconds, minutes, then hours. */
export function pathDuration(ms: number): string {
	const seconds = Math.max(0, Math.round(ms / 1000));
	if (seconds < 60) return `${seconds}s`;
	const minutes = Math.floor(seconds / 60);
	if (minutes < 60) return `${minutes}m`;
	const hours = Math.floor(minutes / 60);
	if (hours < 24) return minutes % 60 ? `${hours}h ${minutes % 60}m` : `${hours}h`;
	return hours % 24 ? `${Math.floor(hours / 24)}d ${hours % 24}h` : `${Math.floor(hours / 24)}d`;
}

/** When an entry happened, short: the time today, the day as well before. */
export function pathTime(at: string, now: number, locale?: string): string {
	const date = new Date(at);
	const today = new Date(now);
	const sameDay =
		date.getFullYear() === today.getFullYear() && date.getMonth() === today.getMonth() && date.getDate() === today.getDate();
	return sameDay
		? date.toLocaleTimeString(locale, { hour: '2-digit', minute: '2-digit' })
		: date.toLocaleString(locale, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
}
