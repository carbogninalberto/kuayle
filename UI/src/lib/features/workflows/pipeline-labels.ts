import { m } from '$lib/features/i18n/messages';
import type { PipelineDefinition, WorkflowIssue } from '$lib/api/workflows';
import { issueLabel } from './labels';
import { HUMAN, KUAYLE, LEAD, type PipelineFact } from './pipeline-editor';

// Translated names for the pipeline editor (KUA-284): the facts a check can
// require, who owns a phase, and the server's pipeline issues and drafting
// reasons. Unknown values fall back to the graph's labels, then to the
// server's text.

const facts: Record<PipelineFact, () => string> = {
	pr_exists: m['workflows.pipeline_fact.pr_exists'],
	pr_draft: m['workflows.pipeline_fact.pr_draft'],
	head_contains_base: m['workflows.pipeline_fact.head_contains_base'],
	conflict: m['workflows.pipeline_fact.conflict'],
	migration_collision: m['workflows.pipeline_fact.migration_collision'],
	ci_green: m['workflows.pipeline_fact.ci_green'],
	verdict: m['workflows.pipeline_fact.verdict'],
	approved: m['workflows.pipeline_fact.approved'],
	merged: m['workflows.pipeline_fact.merged'],
	closed: m['workflows.pipeline_fact.closed'],
	overlap: m['workflows.pipeline_fact.overlap'],
	script: m['workflows.pipeline_fact.script']
};

const issues: Record<string, () => string> = {
	pipeline_size: m['workflows.pipeline_issue.pipeline_size'],
	invalid_role: m['workflows.pipeline_issue.invalid_role'],
	duplicate_role: m['workflows.pipeline_issue.duplicate_role'],
	invalid_writer: m['workflows.pipeline_issue.invalid_writer'],
	writer_count: m['workflows.pipeline_issue.writer_count'],
	invalid_owner: m['workflows.pipeline_issue.invalid_owner'],
	invalid_guard: m['workflows.pipeline_issue.invalid_guard'],
	invalid_check: m['workflows.pipeline_issue.invalid_check'],
	missing_approval: m['workflows.pipeline_issue.missing_approval'],
	invalid_autonomy: m['workflows.pipeline_issue.invalid_autonomy']
};

const reasons: Record<string, () => string> = {
	loop: m['workflows.pipeline_reason.loop'],
	stage_assistant: m['workflows.pipeline_reason.stage_assistant'],
	planning_step: m['workflows.pipeline_reason.planning_step'],
	second_writer: m['workflows.pipeline_reason.second_writer'],
	code_after_approval: m['workflows.pipeline_reason.code_after_approval'],
	early_approval: m['workflows.pipeline_reason.early_approval'],
	dead_end: m['workflows.pipeline_reason.dead_end'],
	branching: m['workflows.pipeline_reason.branching']
};

export const factLabel = (fact: string) => facts[fact as PipelineFact]?.() ?? fact;

/** Who owns a phase: a role by its name, Kuayle, a person or the lead. */
export function ownerLabel(owner: string): string {
	if (owner === KUAYLE) return m['workflows.pipeline_editor.owner_kuayle']();
	if (owner === HUMAN) return m['workflows.pipeline_editor.owner_human']();
	if (owner === LEAD) return m['workflows.pipeline_editor.owner_lead']();
	return owner;
}

/** The owner and check count shown on an editor or run chart phase. */
export function phaseCaption(pipeline: PipelineDefinition | null, id: string): string {
	const phase = pipeline?.phases.find((candidate) => candidate.id === id);
	if (!phase) return '';
	const count = phase.checks?.length ?? 0;
	return [ownerLabel(phase.owner), count ? m['workflows.pipeline_editor.check_count']({ count }) : '']
		.filter(Boolean)
		.join(' · ');
}

/** What the validator says about a pipeline. */
export const pipelineIssueLabel = (issue: Pick<WorkflowIssue, 'code' | 'message'>) =>
	issues[issue.code]?.() ?? issueLabel(issue.code, issue.message);

/** Why a graph workflow cannot be drafted as a pipeline. */
export const draftReasonLabel = (issue: Pick<WorkflowIssue, 'code' | 'message'>) =>
	reasons[issue.code]?.() ?? pipelineIssueLabel(issue);
