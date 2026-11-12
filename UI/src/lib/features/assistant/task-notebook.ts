import type {
	NotebookChange,
	NotebookFinding,
	NotebookInstruction,
	NotebookPlanStep,
	TaskNotebook
} from '$lib/api/agent-tasks';

// The task notebook as the agent view reads it: a short working document.
// The plan comes first, as a checklist; what people asked stays apart from
// what agents wrote; who changed what, and what Kuayle noted, is one history.

/** How Kuayle marks a "done" entry it wrote from an agent's final message. */
const RECORDED = /^\[Recorded by Kuayle[^\]]*\]\s*/;

export type NotebookHistoryEntry =
	| {
			kind: 'change';
			key: string;
			author: NotebookChange['author'];
			userId?: string;
			/** The workflow step's name; empty for a task without a workflow. */
			step: string;
			sections: string[];
			at: string;
	  }
	| { kind: 'event'; key: string; text: string; at: string };

export interface NotebookDocument {
	/** Nothing to read yet beyond the request the view already shows. */
	empty: boolean;
	steps: NotebookPlanStep[];
	/** The planning step's text, when it listed no steps. */
	planText: string;
	progress?: { done: number; total: number };
	/** recorded: Kuayle wrote it from the agent's final message. */
	done: { text: string; recorded: boolean }[];
	remaining: string[];
	verify: string[];
	decisions: { decision: string; why?: string }[];
	/** Open findings first. */
	findings: NotebookFinding[];
	/** What people asked, oldest first, without the request shown above it. */
	people: NotebookInstruction[];
	/** Changes and events, newest first. */
	history: NotebookHistoryEntry[];
}

const same = (a: string, b: string) => a.replace(/\s+/g, ' ').trim() === b.replace(/\s+/g, ' ').trim();

export function notebookDocument(view: TaskNotebook | null | undefined, request: string): NotebookDocument {
	const notebook = view?.notebook;
	const steps = notebook?.plan?.steps ?? [];
	const done = (notebook?.done ?? []).map((text) => ({
		text: text.replace(RECORDED, ''),
		recorded: RECORDED.test(text)
	}));
	const findings = notebook?.findings ?? [];
	const people = (notebook?.instructions ?? []).filter(
		(instruction) => !(instruction.source === 'request' && same(instruction.text, request))
	);
	const history: NotebookHistoryEntry[] = [
		...(view?.history ?? []).map(
			(change): NotebookHistoryEntry => ({
				kind: 'change',
				key: `change-${change.revision}`,
				author: change.author,
				userId: change.user_id,
				step: change.step_label,
				sections: change.sections ?? [],
				at: change.created_at
			})
		),
		...(notebook?.events ?? []).map(
			(event, index): NotebookHistoryEntry => ({ kind: 'event', key: `event-${index}`, text: event.text, at: event.at })
		)
	].sort((a, b) => Date.parse(b.at) - Date.parse(a.at));
	const document: NotebookDocument = {
		empty: true,
		steps,
		planText: steps.length ? '' : (notebook?.plan?.text ?? '').trim(),
		progress: steps.length ? { done: steps.filter((step) => step.done).length, total: steps.length } : undefined,
		done,
		remaining: notebook?.remaining ?? [],
		verify: notebook?.verify ?? [],
		decisions: notebook?.decisions ?? [],
		findings: [...findings.filter((finding) => !finding.resolved), ...findings.filter((finding) => finding.resolved)],
		people,
		history
	};
	document.empty = !(
		steps.length ||
		document.planText ||
		done.length ||
		document.remaining.length ||
		document.verify.length ||
		document.decisions.length ||
		findings.length ||
		people.length
	);
	return document;
}

/**
 * The runs that ended without updating the notebook: Kuayle then records the
 * agent's final message as done itself, which is the only time it writes
 * that section.
 */
export function missedNotebook(view: TaskNotebook | null | undefined): Set<string> {
	const runs = new Set<string>();
	for (const change of view?.history ?? [])
		if (change.author === 'kuayle' && change.run_id && change.sections?.includes('done')) runs.add(change.run_id);
	return runs;
}
