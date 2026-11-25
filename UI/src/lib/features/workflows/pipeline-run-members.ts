import type { PipelineDefinition, WorkflowStepRun } from '$lib/api/workflows';
import { LEAD } from './pipeline-editor.ts';

/** The pinned roster, including an optional lead, with live activity from its role-attributed runs. */
export function pipelineRunMembers(pipeline: PipelineDefinition, runs: readonly WorkflowStepRun[]) {
	const working = new Set(runs.filter((run) => run.status === 'running').map((run) => run.role));
	const roles = pipeline.roster.map((member) => member.role);
	if (pipeline.lead) roles.unshift(LEAD);
	return roles.map((role) => ({ role, working: working.has(role) }));
}
