import type { AgentTask } from '$lib/api/agent-tasks';
import type { AgentProfile, PipelineDefinition, TaskWorkflow, WorkflowStepRun } from '$lib/api/workflows';
import type { StepStatus } from './agent-timeline';

// The task column of the agent view (KUA-281): where a task is in its
// pipeline, who of its roster works now and how much of its budget it used.
// Everything comes from what the task and its workflow view already carry;
// what they do not say is left out, never guessed.

/** A phase before the current one has passed; the others are ahead or the current one. */
export type PhaseState = 'passed' | 'current' | 'ahead';

export interface PhaseRow {
	id: string;
	label: string;
	/** A role of the roster, "kuayle" or "human". */
	owner: string;
	state: PhaseState;
	/** How the current phase goes. */
	status?: StepStatus;
}

type Task = Pick<AgentTask, 'id' | 'state' | 'workflow_node' | 'workflow_paused_at'>;
type View = Pick<TaskWorkflow, 'node' | 'status'>;

function currentStatus(view: View | null, task: Task): StepStatus | 'done' {
	if (task.state === 'failed' || view?.status === 'failed') return 'failed';
	if (task.state === 'cancelled') return 'stopped';
	if (task.state === 'completed' || view?.status === 'done') return 'done';
	if (task.workflow_paused_at || view?.status === 'paused' || view?.status === 'waiting') return 'waiting';
	return 'running';
}

/**
 * The phases in order. The task's own phase is newer than its view's (it
 * follows the task's events), so it wins. Without a lead the current phase
 * is the first one with an unmet required check, so every earlier one has
 * passed; a lead may skip or reorder phases, so then nothing is claimed.
 */
export function pipelinePhases(pipeline: PipelineDefinition, view: View | null, task: Task): PhaseRow[] {
	const status = currentStatus(view, task);
	const current = pipeline.phases.findIndex((phase) => phase.id === (task.workflow_node || view?.node));
	return pipeline.phases.map((phase, index) => {
		const row: PhaseRow = { id: phase.id, label: phase.label || phase.id, owner: phase.owner, state: 'ahead' };
		if (status === 'done' || (index < current && !pipeline.lead)) row.state = 'passed';
		else if (index === current) {
			row.state = 'current';
			row.status = status;
		}
		return row;
	});
}

export interface MemberRow {
	role: string;
	/** The profile's name, when the workspace still lists it. */
	profile?: string;
	harness?: string;
	model?: string;
	writer: boolean;
	lead: boolean;
	/** Its turns so far: the runs of its role. */
	turns: number;
	working: boolean;
}

// A run that has not ended; a task that runs or is about to.
export const activeRun = new Set(['queued', 'starting', 'running', 'waiting_input']);
const activeTask = new Set<AgentTask['state']>(['queued', 'preparing', 'waiting_for_disk', 'running']);

/**
 * The roster in its order, then the lead. A member works while its newest
 * turn has not ended. The task itself is its writer's turns (its runs name
 * the role): while it runs, or is about to, its writer works, also before
 * that turn's run exists.
 */
export function pipelineMembers(
	pipeline: PipelineDefinition,
	runs: readonly WorkflowStepRun[] | undefined,
	profiles: readonly AgentProfile[],
	task: Pick<AgentTask, 'id' | 'state'>
): MemberRow[] {
	const all = runs ?? [];
	const own = all.find((run) => run.task_id === task.id && run.role)?.role ?? pipeline.roster.find((m) => m.writer)?.role;
	const member = (role: string, profileId: string, writer: boolean, lead: boolean): MemberRow => {
		const turns = all.filter((run) => run.role === role);
		const last = turns.at(-1);
		const profile = profiles.find((p) => p.id === profileId);
		return {
			role,
			profile: profile?.name,
			harness: profile?.harness ?? last?.provider,
			model: profile?.model ?? last?.model,
			writer,
			lead,
			turns: turns.length,
			working: (!!last && activeRun.has(last.status)) || (role === own && activeTask.has(task.state))
		};
	};
	const rows = pipeline.roster.map((m) => member(m.role, m.profile_id, !!m.writer, false));
	if (pipeline.lead) rows.push(member('lead', pipeline.lead.profile_id, false, true));
	return rows;
}

export interface BudgetUse {
	/** A pipeline counts its members' turns, a graph workflow its iterations. */
	unit: 'turns' | 'iterations';
	used: number;
	/** The limit, with the extensions a person granted; absent without one. */
	max?: number;
}

/**
 * What a task on a workflow used of its budget: its turns or iterations so
 * far against the limit and each extension of it. Nothing when it has no
 * view, or no limit and nothing used.
 */
export function budgetUse(
	view: Pick<TaskWorkflow, 'iterations' | 'graph'> | null,
	pipeline: PipelineDefinition | null,
	extensions = 0
): BudgetUse | null {
	if (!view) return null;
	const limit = pipeline ? pipeline.budget?.max_turns : view.graph.budget?.max_iterations;
	const use: BudgetUse = { unit: pipeline ? 'turns' : 'iterations', used: view.iterations ?? 0 };
	if (limit && limit > 0) use.max = limit * (1 + Math.max(0, extensions));
	else if (!use.used) return null;
	return use;
}

/** The share of the limit used, in percent up to the whole; null without a limit. */
export function budgetPercent(use: BudgetUse): number | null {
	return use.max ? Math.min(100, (use.used / use.max) * 100) : null;
}
