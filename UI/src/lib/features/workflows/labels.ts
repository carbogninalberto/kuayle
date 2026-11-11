import { m } from '$lib/features/i18n/messages';
import type { AgentBehaviour, AgentProfile, WorkflowCondition, WorkflowNodeType } from '$lib/api/workflows';

// Translated names of node types, transition conditions, behaviours, validation
// issues and run statuses. Unknown values fall back to the server's text.

const types: Record<WorkflowNodeType, () => string> = {
	start: m['workflows.type.start'],
	agent: m['workflows.type.agent'],
	check: m['workflows.type.check'],
	approval: m['workflows.type.approval'],
	publish: m['workflows.type.publish'],
	merge: m['workflows.type.merge'],
	end: m['workflows.type.end']
};

const conditions: Record<WorkflowCondition, () => string> = {
	agent_finished: m['workflows.condition.agent_finished'],
	agent_failed: m['workflows.condition.agent_failed'],
	checks_passed: m['workflows.condition.checks_passed'],
	checks_failed: m['workflows.condition.checks_failed'],
	review_approved: m['workflows.condition.review_approved'],
	review_changes_requested: m['workflows.condition.review_changes_requested'],
	approval_approved: m['workflows.condition.approval_approved'],
	approval_denied: m['workflows.condition.approval_denied'],
	published: m['workflows.condition.published'],
	publish_failed: m['workflows.condition.publish_failed'],
	merged: m['workflows.condition.merged'],
	timeout: m['workflows.condition.timeout'],
	else: m['workflows.condition.else']
};

const behaviours: Record<AgentBehaviour, () => string> = {
	plans: m['profiles.behaviour_plans'],
	changes_code: m['profiles.behaviour_changes_code'],
	reviews: m['profiles.behaviour_reviews']
};

const issues: Record<string, () => string> = {
	too_large: m['workflows.issue.too_large'],
	malformed: m['workflows.issue.malformed'],
	invalid_id: m['workflows.issue.invalid_id'],
	duplicate_id: m['workflows.issue.duplicate_id'],
	invalid_label: m['workflows.issue.invalid_label'],
	prompt_too_long: m['workflows.issue.prompt_too_long'],
	invalid_limit: m['workflows.issue.invalid_limit'],
	invalid_timeout: m['workflows.issue.invalid_timeout'],
	invalid_end: m['workflows.issue.invalid_end'],
	missing_profile: m['workflows.issue.missing_profile'],
	invalid_check: m['workflows.issue.invalid_check'],
	invalid_type: m['workflows.issue.invalid_type'],
	unexpected_profile: m['workflows.issue.unexpected_profile'],
	start_count: m['workflows.issue.start_count'],
	missing_end: m['workflows.issue.missing_end'],
	invalid_budget: m['workflows.issue.invalid_budget'],
	dangling_edge: m['workflows.issue.dangling_edge'],
	invalid_condition: m['workflows.issue.invalid_condition'],
	duplicate_condition: m['workflows.issue.duplicate_condition'],
	end_has_exit: m['workflows.issue.end_has_exit'],
	start_has_entry: m['workflows.issue.start_has_entry'],
	start_exit: m['workflows.issue.start_exit'],
	start_target: m['workflows.issue.start_target'],
	dead_end: m['workflows.issue.dead_end'],
	unreachable: m['workflows.issue.unreachable'],
	unguarded_loop: m['workflows.issue.unguarded_loop'],
	merge_target: m['workflows.issue.merge_target'],
	plan_target: m['workflows.issue.plan_target']
};

const statuses: Record<string, () => string> = {
	running: m['workflows.task.status_running'],
	waiting: m['workflows.task.status_waiting'],
	paused: m['workflows.task.status_paused'],
	done: m['workflows.task.status_done'],
	failed: m['workflows.task.status_failed']
};

export const nodeTypeLabel = (type: WorkflowNodeType) => types[type]?.() ?? type;
export const conditionLabel = (condition: string) => conditions[condition as WorkflowCondition]?.() ?? condition;
export const behaviourLabel = (behaviour: string) => behaviours[behaviour as AgentBehaviour]?.() ?? behaviour;
/** A profile's name with its role, when it has one that adds to the name. */
export function profileTitle(profile: Pick<AgentProfile, 'name' | 'role'>): string {
	const role = (profile.role ?? '').trim();
	return !role || role.toLowerCase() === profile.name.trim().toLowerCase() ? profile.name : `${profile.name} · ${role}`;
}
export const issueLabel = (code: string, fallback: string) => issues[code]?.() ?? fallback;
export const statusLabel = (status: string) => statuses[status]?.();
