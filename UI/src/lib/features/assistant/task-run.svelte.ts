import { untrack } from 'svelte';
import { agentTasks, type AgentTask, type TaskTransition } from '$lib/api/agent-tasks';
import { workflows, type TaskWorkflow } from '$lib/api/workflows';
import type { AgentRun } from '$lib/types/dev-machine';
import type { ConversationState } from './conversation-state.svelte';
import { attemptTimeline, workflowTimeline, type TimelineStep } from './agent-timeline';

// How long after a failed read it is tried once more.
const RETRY_AFTER = 2000;

// What a task did, loaded once for every view that shows it: the agent view's
// timeline, the sidebar's path and the workflow chart read the same TaskRun,
// so the workflow is fetched and polled once however many of them are open.
// A task that follows a workflow reads its workflow view; any other reads its
// attempts, and its state changes or the Standard workflow's graph when a
// view asks for them.
export class TaskRun {
	view = $state.raw<TaskWorkflow | null>(null);
	attempts = $state.raw<AgentRun[]>([]);
	transitions = $state.raw<TaskTransition[]>([]);
	/** What the task did could not be read, also when tried again. */
	failed = $state(false);
	/** A task without a workflow: the Standard chart could not be read. */
	chartFailed = $state(false);
	task = $state.raw<AgentTask>(undefined as unknown as AgentTask);
	/** Fixed for a task: it pins its workflow version when it is created. */
	readonly followsWorkflow: boolean = $derived(!!this.task.workflow_version_id);
	/** The task's steps, oldest first: workflow steps, or one per attempt. */
	readonly steps: TimelineStep[] = $derived(
		this.followsWorkflow
			? this.view
				? workflowTimeline(this.view, this.task)
				: []
			: attemptTimeline(this.task, this.attempts)
	);
	users = 0;
	private request = 0;
	private key = '';
	private history = false;
	private chart = false;
	private timer: ReturnType<typeof setInterval> | undefined;
	private pace = 0;
	private failures = 0;
	private again: ReturnType<typeof setTimeout> | undefined;
	// A finished run's summary is final: the ones read are kept, and only
	// those of runs finished since (knownUntil) are asked for.
	private summaries = new Map<string, string>();
	private knownUntil = '';

	constructor(
		private readonly slug: string,
		task: AgentTask
	) {
		this.task = task;
	}

	/**
	 * Follows the task: key changes whenever it moved (its state, attempt or
	 * workflow step, or one of its events), and the first view to say so
	 * reloads for all of them.
	 */
	sync(task: AgentTask, key: string, wants: { history?: boolean; chart?: boolean } = {}) {
		this.task = task;
		const more = (!!wants.history && !this.history) || (!!wants.chart && !this.chart && !this.followsWorkflow);
		this.history ||= !!wants.history;
		this.chart ||= !!wants.chart;
		if (key !== this.key || more) {
			this.key = key;
			void this.load();
		}
		this.schedule();
	}

	/**
	 * Reads what the task did. A read that fails is tried once more after a
	 * moment, by itself; only then does the run say it failed, and a view
	 * offers to read again.
	 */
	async load() {
		const current = ++this.request;
		clearTimeout(this.again);
		this.again = undefined;
		try {
			if (this.followsWorkflow) {
				const loaded = await workflows.task(this.slug, this.task.id, this.knownUntil || undefined);
				if (current !== this.request) return;
				// Anything but a workflow view is a failed load, not a path.
				if (!Array.isArray(loaded?.steps) || !loaded.graph) throw new Error('not a workflow view');
				const runs = (loaded.runs ?? []).map((run) => {
					if (run.summary) this.summaries.set(run.id, run.summary);
					if (run.completed_at && (!this.knownUntil || Date.parse(run.completed_at) > Date.parse(this.knownUntil)))
						this.knownUntil = run.completed_at;
					return run.summary || !this.summaries.has(run.id) ? run : { ...run, summary: this.summaries.get(run.id) };
				});
				this.view = { ...loaded, runs };
			} else {
				// The attempts, the Standard chart and the state changes are
				// read side by side: one that fails takes none of the others.
				if (this.chart) void this.loadChart(current);
				if (this.history) void this.loadHistory();
				const loaded = await agentTasks.runs(this.slug, this.task.id);
				if (current !== this.request) return;
				this.attempts = loaded.runs;
			}
			this.failed = false;
			this.failures = 0;
		} catch {
			if (current !== this.request) return;
			if (++this.failures === 1 && this.users)
				this.again = setTimeout(() => void this.load(), RETRY_AFTER);
			else this.failed = true;
		}
		this.schedule();
	}

	private async loadChart(current: number) {
		try {
			const chart = await workflows.task(this.slug, this.task.id);
			if (current !== this.request) return;
			if (!chart?.graph) throw new Error('not a workflow view');
			this.view = chart;
			this.chartFailed = false;
		} catch {
			if (current === this.request) this.chartFailed = true;
		}
	}

	// State changes are paged and only ever added to: each load reads on from
	// the last one it has.
	private historyLoad: Promise<void> | undefined;
	private loadHistory() {
		return (this.historyLoad ??= (async () => {
			try {
				let after = this.transitions.at(-1)?.sequence ?? 0;
				for (let page = 0; page < 10; page++) {
					const events = await agentTasks.history(this.slug, this.task.id, after);
					const known = new Set(this.transitions.map((change) => change.sequence));
					this.transitions = [...this.transitions, ...events.transitions.filter((change) => !known.has(change.sequence))];
					if (!events.next_after || events.next_after === after) break;
					after = events.next_after;
				}
			} catch {
				/* The attempts still show; the next change of the task reads again. */
			} finally {
				this.historyLoad = undefined;
			}
		})());
	}

	// Events can be missed (a dropped connection), so a workflow is also
	// polled: modestly while a step runs, rarely while it waits (an approval
	// can take days), never once it has ended or while nothing shows it.
	private schedule() {
		const view = untrack(() => this.view);
		const pace =
			!this.users || !this.followsWorkflow || !view || view.status === 'done' || view.status === 'failed'
				? 0
				: untrack(() => this.steps).some((step) => step.status === 'running')
					? 15_000
					: 120_000;
		if (pace === this.pace) return;
		this.pace = pace;
		clearInterval(this.timer);
		this.timer = pace ? setInterval(() => void this.load(), pace) : undefined;
	}

	release() {
		if (--this.users > 0) return false;
		this.request++;
		clearTimeout(this.again);
		this.schedule();
		return true;
	}
}

/**
 * The shared run of a task for the component that calls it; the component is
 * mounted per task (keyed by its id). For a task without a workflow, history
 * asks for its state changes as well and chart for the Standard workflow's
 * graph.
 */
export function useTaskRun(
	chat: ConversationState,
	task: () => AgentTask,
	wants: { history?: boolean; chart?: boolean } = {}
): TaskRun {
	const id = untrack(() => task().id);
	let run = chat.taskRuns.get(id);
	if (!run) chat.taskRuns.set(id, (run = new TaskRun(chat.slug, untrack(task))));
	const shared = run;
	shared.users++;
	// The task moving (state, attempt, workflow step) or one of its reviews
	// changing arrives as an event and reloads the steps.
	$effect(() => {
		const current = task();
		const key = [
			current.state,
			current.run_id,
			current.generation,
			current.workflow_version_id,
			current.workflow_node,
			current.workflow_status,
			current.workflow_paused_at,
			current.updated_at,
			chat.taskWorkflowCursor[id],
			chat.taskReviewCursor[id]
		].join('|');
		untrack(() => shared.sync(current, key, wants));
	});
	$effect(() => () => {
		if (shared.release() && chat.taskRuns.get(id) === shared) chat.taskRuns.delete(id);
	});
	return shared;
}
