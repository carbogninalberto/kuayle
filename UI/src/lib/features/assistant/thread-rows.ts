import type { TaskMessage } from '$lib/api/agent-tasks';
import type { RunUsage } from '$lib/types/dev-machine';
import type { TimelineStep } from './agent-timeline';

// A task's thread as one line per event (KUA-286): a role's turn (or a
// graph workflow's agent step) is one line that says who, what it ended
// with, how long it took, what it worked through and how it went; what
// Kuayle did in a row folds into one line. The reader filters the lines by
// role, or to the decisions only. This shapes what the server returned; it
// decides nothing.

/** Which lines the reader keeps: one role's (empty: everyone's), and only decisions. */
export interface ThreadFilter {
	role: string;
	decisions: boolean;
}

/**
 * The first line of a text that has words, without its Markdown marks, and
 * capped: a line of a thread, never a paragraph.
 */
export function firstLine(text: string | null | undefined, max = 200): string {
	const line =
		(text ?? '')
			.split('\n')
			.map((value) =>
				value
					.replace(/^\s*(?:#{1,6}\s+|>\s*|[-*+]\s+|\d+[.)]\s+)/, '')
					.replace(/(\*\*|__|`)/g, '')
					.trim()
			)
			.find(Boolean) ?? '';
	const chars = [...line];
	return chars.length > max ? `${chars.slice(0, max - 1).join('')}…` : line;
}

/**
 * What a turn ended with: the first line of its run's final message, or,
 * before the run ends, of the newest post it made that the room took.
 */
export function turnSummary(
	run: { summary?: string | null } | undefined,
	posts: readonly Pick<TaskMessage, 'body' | 'reject_reason'>[]
): string {
	const final = firstLine(run?.summary);
	if (final) return final;
	for (let i = posts.length - 1; i >= 0; i--) {
		if (posts[i].reject_reason) continue;
		const said = firstLine(posts[i].body);
		if (said) return said;
	}
	return '';
}

type Reported = { [K in keyof RunUsage]?: RunUsage[K] | null };

/**
 * The tokens runs worked through: input and output as their harnesses
 * reported them (cache reads are re-read context, told apart inside the
 * opened line). Undefined when no run reported either.
 */
export function workTokens(usages: readonly Reported[]): number | undefined {
	let total: number | undefined;
	for (const usage of usages)
		for (const value of [usage.input_tokens, usage.output_tokens])
			if (typeof value === 'number') total = (total ?? 0) + value;
	return total;
}

/**
 * The rows a filter keeps: with a role, those that concern it (written by
 * it or addressed to it); with decisions, the decisions.
 */
export function shownRows<T>(
	rows: readonly T[],
	filter: ThreadFilter,
	roles: (row: T) => readonly string[],
	decision: (row: T) => boolean
): T[] {
	return rows.filter((row) => (!filter.role || roles(row).includes(filter.role)) && (!filter.decisions || decision(row)));
}

/** The roles rows concern, in the order they first appear. */
export function threadRoles<T>(rows: readonly T[], roles: (row: T) => readonly string[]): string[] {
	const seen = new Set<string>();
	for (const row of rows) for (const role of roles(row)) if (role) seen.add(role);
	return [...seen];
}

/** A graph workflow's step on the thread's rows, or Kuayle's steps in a row folded. */
export type StepRow =
	| { type: 'step'; key: string; step: TimelineStep; role: string; decision: boolean }
	| { type: 'fold'; key: string; steps: TimelineStep[]; role: ''; decision: false };

/**
 * A graph workflow's steps (or a task's attempts) as rows. An agent step is
 * a turn of its role; a review's verdict and a person's approval are
 * decisions. Steps Kuayle ran one after another (publish, a check, a merge)
 * fold into one row; a decision and the step the task is on stay rows of
 * their own.
 */
export function stepThread(steps: readonly TimelineStep[], roleOf: (step: TimelineStep) => string | undefined): StepRow[] {
	const rows: StepRow[] = [];
	let run: TimelineStep[] = [];
	const flush = () => {
		if (run.length > 1) rows.push({ type: 'fold', key: `fold-${run[0].anchor}`, steps: run, role: '', decision: false });
		else if (run.length) rows.push({ type: 'step', key: run[0].anchor, step: run[0], role: '', decision: false });
		run = [];
	};
	for (const step of steps) {
		const decision = step.nodeType === 'approval' || !!step.reviewTaskId || step.status === 'changes';
		if (step.kind === 'row' && !decision && !step.current) {
			run.push(step);
			continue;
		}
		flush();
		rows.push({
			type: 'step',
			key: step.anchor,
			step,
			role: step.kind === 'agent' ? (roleOf(step) ?? '') : '',
			decision
		});
	}
	flush();
	return rows;
}
