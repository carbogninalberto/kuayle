import type { TaskSignal } from '$lib/api/agent-tasks';

// The reviewer who approved the current attempt, if its latest deciding review
// is an approval. Comments do not change a decision, as on GitHub; reviews by
// people without write access are ignored. Completion still waits for the merge.
export function approvedBy(
	signals: Pick<TaskSignal, 'kind' | 'run_id' | 'outcome' | 'action' | 'name' | 'created_at'>[],
	runId: string
): string | undefined {
	const decisions = signals
		.filter((s) => s.kind === 'review' && s.run_id === runId && s.action !== 'untrusted' && s.outcome !== 'commented')
		.sort((a, b) => Date.parse(a.created_at) - Date.parse(b.created_at));
	const latest = decisions.at(-1);
	return latest?.outcome === 'approved' ? latest.name : undefined;
}
