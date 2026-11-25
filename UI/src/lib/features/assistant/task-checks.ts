import type { TaskMessage } from '$lib/api/agent-tasks';
import type { PipelineCheck, PipelineDefinition } from '$lib/api/workflows';

// The checks of a task on a pipeline (KUA-286), each with how it stands for
// the head, Kuayle's reason and since when, for the Checks tab. Everything
// comes from the room: Kuayle posts each fact when its value changes (with
// its reason when it blocks), the roles post their verdicts and the owner
// their waivers. It mirrors how the tick judges (task_facts.go, judge) only
// as far as the room says; what the room has not said is not known yet.
// The server decides; this only reads what it posted.

/** How a check stands for the head. */
export type CheckState = 'met' | 'unmet' | 'waiting' | 'unknown' | 'waived';

/** What the view says about a check when Kuayle's own words do not cover it. */
export type CheckNote =
	| { kind: 'no_verdict'; role: string }
	| { kind: 'old_verdict'; role: string; commit: string }
	| { kind: 'not_approved'; role: string; verdict: string }
	| { kind: 'merges_first'; issues: string }
	| { kind: 'waived'; commit: string };

export interface CheckRow {
	check: PipelineCheck;
	phase: { id: string; label: string; index: number };
	state: CheckState;
	/** What Kuayle said about it when it blocks: its own words, possibly long. */
	reason: string;
	note?: CheckNote;
	/** When it came to stand so, as far as the room says. */
	since?: string;
}

export interface TaskChecks {
	/** The pull request's head, as Kuayle last said it; empty before it did. */
	head: string;
	rows: CheckRow[];
}

type Said = { value: unknown; body: string; at: string };

const want: Record<string, boolean> = {
	pr_draft: false,
	head_contains_base: true,
	conflict: false,
	migration_collision: false,
	approved: true,
	merged: true,
	closed: false
};

// A commit names the head when one is a prefix of the other (verdicts may
// name a short SHA).
const sameCommit = (head: string, commit: string | undefined) =>
	!!head && !!commit && (head.startsWith(commit) || commit.startsWith(head));

/**
 * The checks of every phase in order. phase is the phase the task is on;
 * without a lead every phase before it passed, so their required checks
 * held (or were waived) when Kuayle last moved it, and a done task passed
 * them all.
 */
export function pipelineChecks(
	pipeline: PipelineDefinition,
	room: readonly TaskMessage[],
	phase: string | null | undefined,
	done: boolean
): TaskChecks {
	const facts = new Map<string, Said>();
	const verdicts = new Map<string, { verdict: string; commit: string; at: string }>();
	const scripts = new Map<string, { commit: string; exit: number; body: string; at: string }>();
	const waivers = new Map<string, { head: string; at: string }>();
	let head = '';
	for (const message of [...room].sort((a, b) => a.seq - b.seq)) {
		if (message.reject_reason) continue;
		const data = message.data ?? {};
		if (message.author_kind === 'kuayle' && message.kind === 'fact' && data.fact) {
			if (data.fact === 'script') {
				const run = (data.value ?? {}) as { check?: string; commit?: string; exit?: number };
				if (run.check && typeof run.exit === 'number')
					scripts.set(run.check, { commit: run.commit ?? '', exit: run.exit, body: message.body, at: message.created_at });
				continue;
			}
			facts.set(data.fact, { value: data.value, body: message.body, at: message.created_at });
			if (data.fact === 'head_sha' && typeof data.value === 'string') head = data.value;
		} else if (message.author_kind === 'member' && message.kind === 'verdict' && data.verdict && message.author_role) {
			verdicts.set(message.author_role, { verdict: data.verdict, commit: data.commit ?? '', at: message.created_at });
		} else if (message.author_kind === 'human' && message.kind === 'waiver' && data.check) {
			// It holds for the head the room last named (the tick's readRoom).
			waivers.set(data.check, { head, at: message.created_at });
		}
	}

	const short = head.slice(0, 7);
	function judge(check: PipelineCheck): Omit<CheckRow, 'check' | 'phase'> {
		// Every check needs the pull request first, as the tick judges.
		const pr = facts.get('pr_exists');
		if (!pr) return { state: 'unknown', reason: '' };
		if (pr.value !== true) return { state: 'unmet', reason: pr.body, since: pr.at };
		if (check.fact === 'pr_exists') return { state: 'met', reason: '', since: pr.at };
		if (check.fact === 'verdict') {
			const role = check.role ?? '';
			const given = verdicts.get(role);
			if (!given) return { state: 'unmet', reason: '', note: { kind: 'no_verdict', role } };
			if (!sameCommit(head, given.commit))
				return { state: 'unmet', reason: '', note: { kind: 'old_verdict', role, commit: given.commit.slice(0, 7) }, since: given.at };
			if (given.verdict !== 'approve')
				return { state: 'unmet', reason: '', note: { kind: 'not_approved', role, verdict: given.verdict }, since: given.at };
			return { state: 'met', reason: '', since: given.at };
		}
		if (check.fact === 'script') {
			const run = scripts.get(check.id);
			if (!run || !sameCommit(head, run.commit)) return { state: 'waiting', reason: '' };
			return run.exit === 0 ? { state: 'met', reason: '', since: run.at } : { state: 'unmet', reason: run.body, since: run.at };
		}
		const said = facts.get(check.fact);
		if (!said) return { state: 'unknown', reason: '' };
		if (check.fact === 'ci_green') {
			const ci = (said.value ?? {}) as { state?: string; head?: string };
			if (!sameCommit(head, ci.head)) return { state: 'unknown', reason: said.body, since: said.at };
			if (ci.state === 'passing') return { state: 'met', reason: '', since: said.at };
			if (ci.state === 'failing') return { state: 'unmet', reason: said.body, since: said.at };
			if (ci.state === 'pending' || ci.state === 'none') return { state: 'waiting', reason: said.body, since: said.at };
			return { state: 'unknown', reason: said.body, since: said.at };
		}
		if (check.fact === 'overlap') {
			const ahead = Array.isArray(said.value) ? (said.value as string[]) : [];
			return ahead.length
				? { state: 'waiting', reason: said.body, note: { kind: 'merges_first', issues: ahead.join(', ') }, since: said.at }
				: { state: 'met', reason: '', since: said.at };
		}
		if (check.fact in want && typeof said.value === 'boolean')
			return said.value === want[check.fact]
				? { state: 'met', reason: '', since: said.at }
				: { state: 'unmet', reason: said.body, since: said.at };
		return { state: 'unknown', reason: said.body, since: said.at };
	}

	const current = pipeline.phases.findIndex((p) => p.id === phase);
	const rows: CheckRow[] = [];
	pipeline.phases.forEach((p, index) => {
		const passed = done || (!pipeline.lead && current >= 0 && index < current);
		for (const check of p.checks ?? []) {
			const row: CheckRow = { check, phase: { id: p.id, label: p.label || p.id, index }, ...judge(check) };
			const waiver = check.required ? waivers.get(check.id) : undefined;
			if (row.state !== 'met' && waiver && waiver.head === head)
				Object.assign(row, { state: 'waived', note: { kind: 'waived', commit: short }, since: waiver.at });
			else if (passed && check.required && row.state !== 'met') Object.assign(row, { state: 'met', reason: '', note: undefined });
			rows.push(row);
		}
	});
	return { head, rows };
}
