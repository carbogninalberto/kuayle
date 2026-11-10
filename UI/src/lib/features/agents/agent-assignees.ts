// Rules for AI agents as issue assignees (KUA-107), kept free of Svelte and
// API imports so they are unit tested directly.

export interface AssignableAgent {
	id: string;
	name: string;
	team_ids: string[];
}

export interface AssignedAgent {
	id: string;
	name: string;
	task_state: string | null;
	error?: string | null;
}

/** Agents an administrator enabled for the team, by name. */
export function agentsForTeam<T extends AssignableAgent>(agents: T[], teamId: string | null | undefined): T[] {
	if (!teamId) return [];
	return agents.filter((agent) => agent.team_ids.includes(teamId)).sort((a, b) => a.name.localeCompare(b.name));
}

/** The agent IDs after toggling one in an issue's picker. */
export function toggleAgent(current: string[], id: string): string[] {
	return current.includes(id) ? current.filter((value) => value !== id) : [...current, id];
}

// Mirrors domain.AgentTaskWorking: unassigning cancels a task in these
// states; one awaiting review keeps its pull request.
const WORKING = new Set(['queued', 'preparing', 'running', 'waiting_input', 'validating', 'publishing', 'blocked', 'failed']);

/** Whether unassigning the agent cancels work in progress (ask first). */
export function unassignCancelsTask(agent: AssignedAgent | undefined): boolean {
	return !!agent?.task_state && WORKING.has(agent.task_state);
}

/** Whether the agent's status can still change without anyone acting. */
export function agentIsLive(agent: AssignedAgent): boolean {
	if (agent.error) return false;
	if (!agent.task_state) return true; // the task is being started
	return !['completed', 'cancelled', 'failed', 'blocked', 'awaiting_review'].includes(agent.task_state);
}

export const AGENT_GROUP_PREFIX = 'agent:';

/**
 * The group of an issue when grouping by assignee: its first person, else the
 * agent with the lowest ID (the server orders pages the same way), else
 * unassigned.
 */
export function assigneeGroup(issue: {
	assignee_id: string | null;
	assignee?: { name: string };
	agent_assignees?: { id: string; name: string }[];
}): { key: string; label: string } {
	if (issue.assignee_id) return { key: issue.assignee_id, label: issue.assignee?.name ?? issue.assignee_id };
	const agents = [...(issue.agent_assignees ?? [])].sort((a, b) => a.id.localeCompare(b.id));
	if (agents.length > 0) return { key: AGENT_GROUP_PREFIX + agents[0].id, label: agents[0].name };
	return { key: 'unassigned', label: 'unassigned' };
}

/** The agent ID of an assignee group key, or null for a person or none. */
export function groupAgentId(key: string): string | null {
	return key.startsWith(AGENT_GROUP_PREFIX) ? key.slice(AGENT_GROUP_PREFIX.length) : null;
}
