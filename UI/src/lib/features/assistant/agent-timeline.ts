import type { TaskWorkflow, WorkflowNodeType, WorkflowStep, WorkflowStepRun } from '$lib/api/workflows';
import type { AgentTask } from '$lib/api/agent-tasks';
import type { AgentRun } from '$lib/types/dev-machine';

// A coding task as a timeline: one section per workflow step, in the order
// the task took them, each with the agent runs it started. A task without a
// workflow is one step per attempt. The server places every run on its step
// (TaskWorkflow.runs); this only shapes what it returned for the view.

export type StepStatus = 'running' | 'waiting' | 'done' | 'changes' | 'failed' | 'stopped';

export interface TimelineStep {
	/** Stable across reloads; also the section's element id, for links to it. */
	anchor: string;
	/** An agent's work, or a compact row (start, publish, check, approval, merge, end). */
	kind: 'agent' | 'row';
	nodeType: WorkflowNodeType;
	/** The step's name in its workflow; empty when it has none (the view names it by kind or harness). */
	label: string;
	profile?: string;
	harness?: string;
	model?: string;
	status: StepStatus;
	visit: number;
	/** The step the task is on. */
	current: boolean;
	startedAt: string;
	finishedAt?: string;
	/** Seconds a person's pause held the step; not time it worked. */
	pausedSeconds?: number;
	/** Oldest first; a resumed step has several. */
	runs: WorkflowStepRun[];
	/** The review task a reviewer step started; its runs are read under it. */
	reviewTaskId?: string;
	plan?: string;
	detail: string;
	reason: string;
	/** The step's node in its workflow; absent for a task without one. */
	nodeId?: string;
	/** The transition the task took to get here. */
	condition?: string;
	/** What a check step checks, and what an approval step asks. */
	check?: 'tests' | 'ci';
	question?: string;
}

type Task = Pick<AgentTask, 'id' | 'state' | 'run_id' | 'provider' | 'model' | 'updated_at'>;
type View = Pick<TaskWorkflow, 'graph' | 'profiles' | 'steps'> & { runs?: WorkflowStepRun[] };

const failedOutcomes = new Set([
	'agent_failed',
	'checks_failed',
	'publish_failed',
	'approval_denied',
	'timeout',
	'task_failed',
	'failed'
]);
const stoppedOutcomes = new Set(['task_cancelled', 'retried']);

function outcomeStatus(outcome: string | null | undefined): StepStatus {
	if (!outcome) return 'done';
	if (outcome === 'review_changes_requested') return 'changes';
	if (failedOutcomes.has(outcome)) return 'failed';
	return stoppedOutcomes.has(outcome) ? 'stopped' : 'done';
}

// The status of the step the task is on, from the task's own state.
function liveStatus(state: string, agent: boolean, reviewing: boolean): StepStatus {
	switch (state) {
		case 'failed':
			return 'failed';
		case 'cancelled':
			return 'stopped';
		case 'completed':
			return 'done';
		case 'waiting_input':
		case 'blocked':
			return 'waiting';
		case 'awaiting_review':
			// A reviewer works while the task waits for it; anything else waits
			// for a person, CI or a timeout.
			return reviewing ? 'running' : 'waiting';
	}
	return agent || state !== 'queued' ? 'running' : 'waiting';
}

function runStatus(status: string): StepStatus | undefined {
	switch (status) {
		case 'succeeded':
			return 'done';
		case 'failed':
		case 'timeout':
			return 'failed';
		case 'cancelled':
			return 'stopped';
	}
	return undefined;
}

/** The steps of a task that follows a workflow, in the order it took them. */
export function workflowTimeline(view: View, task: Task): TimelineStep[] {
	const steps = [...view.steps].sort((a, b) => a.sequence - b.sequence);
	const last = steps.at(-1)?.sequence;
	const runs = view.runs ?? [];
	return steps.map((step: WorkflowStep) => {
		const node = view.graph.nodes.find((n) => n.id === step.node_id);
		const profile = node?.profile_id ? view.profiles.find((p) => p.id === node.profile_id) : undefined;
		const agent = step.node_type === 'agent';
		const current = step.sequence === last && !step.finished_at;
		const own = runs.filter((run) => run.step === step.sequence);
		// The iteration the task just queued has no run yet: it will be the
		// task's run, so its output streams in as soon as it exists.
		if (current && agent && step.generation != null && !own.some((run) => run.id === task.run_id))
			own.push({
				id: task.run_id,
				task_id: task.id,
				step: step.sequence,
				provider: task.provider,
				model: task.model,
				status: 'queued',
				created_at: step.entered_at
			});
		const ran = own.at(-1);
		return {
			anchor: `step-${step.sequence}`,
			kind: agent ? 'agent' : 'row',
			nodeType: step.node_type,
			label: node?.label ?? '',
			profile: profile?.name,
			harness: agent ? ran?.provider || profile?.harness : undefined,
			model: agent ? (ran ? ran.model : profile?.model) || undefined : undefined,
			status: current ? liveStatus(task.state, agent, !!step.review_task_id) : outcomeStatus(step.outcome),
			visit: step.visit,
			current,
			startedAt: step.entered_at,
			finishedAt: step.finished_at ?? undefined,
			pausedSeconds: step.paused_seconds || undefined,
			runs: own,
			reviewTaskId: step.review_task_id ?? undefined,
			plan: step.plan || undefined,
			detail: step.detail,
			reason: step.reason,
			nodeId: step.node_id,
			condition: step.condition ?? undefined,
			check: node?.check,
			question: step.node_type === 'approval' ? node?.prompt || undefined : undefined
		};
	});
}

type Attempt = Pick<AgentRun, 'id' | 'provider_id' | 'status' | 'created_at' | 'started_at' | 'completed_at'> & {
	model?: string;
};

/** A task without a workflow: one step per attempt, oldest first. */
export function attemptTimeline(task: Task, attempts: Attempt[]): TimelineStep[] {
	const runs: WorkflowStepRun[] = attempts
		.map((run) => ({
			id: run.id,
			task_id: task.id,
			provider: run.provider_id,
			model: run.model ?? '',
			status: run.status,
			created_at: run.created_at,
			started_at: run.started_at,
			completed_at: run.completed_at
		}))
		.sort((a, b) => Date.parse(a.created_at) - Date.parse(b.created_at) || a.id.localeCompare(b.id));
	// The attempt the task is on may not have its run yet.
	if (!runs.some((run) => run.id === task.run_id))
		runs.push({
			id: task.run_id,
			task_id: task.id,
			provider: task.provider,
			model: task.model,
			status: 'queued',
			created_at: task.updated_at
		});
	return runs.map((run) => {
		const current = run.id === task.run_id;
		const ended = runStatus(run.status);
		return {
			anchor: `run-${run.id}`,
			kind: 'agent',
			nodeType: 'agent',
			label: '',
			harness: run.provider,
			model: run.model || undefined,
			status: ended ?? (current ? liveStatus(task.state, true, false) : 'stopped'),
			visit: 1,
			current,
			startedAt: run.started_at ?? run.created_at,
			finishedAt: run.completed_at ?? undefined,
			runs: [run],
			detail: '',
			reason: ''
		};
	});
}

/**
 * The step that starts open: the agent working now, otherwise the last agent
 * that worked, so the view never opens on closed rows only.
 */
export function openStep(steps: TimelineStep[]): string | undefined {
	const agents = steps.filter((step) => step.kind === 'agent');
	return (agents.find((step) => step.current && step.status === 'running') ?? agents.at(-1))?.anchor;
}

/**
 * How long a step worked, or has been working while it runs: the time a
 * person's pause held it is left out.
 */
export function stepDuration(
	step: Pick<TimelineStep, 'startedAt' | 'finishedAt' | 'status' | 'pausedSeconds'>,
	now: number
) {
	const end = step.finishedAt ? Date.parse(step.finishedAt) : step.status === 'running' ? now : NaN;
	const ms = end - Date.parse(step.startedAt);
	return Number.isFinite(ms) && ms >= 0 ? Math.max(0, ms - (step.pausedSeconds ?? 0) * 1000) : undefined;
}
