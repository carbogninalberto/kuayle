import { agentsApi, type IssueAgentAssignee } from '$lib/api/agents';
import { issuesState } from '$lib/features/issues/issues.state.svelte';
import { appToast } from '$lib/features/toast/toast';
import { m } from '$lib/features/i18n/messages';
import { toggleAgent, unassignCancelsTask } from './agent-assignees';

// One confirmation for unassigning a working agent, rendered by
// AgentUnassignConfirm in the workspace layout so it outlives the picker
// popover that asked for it.
class UnassignConfirm {
	pending = $state<{ name: string; resolve: (confirmed: boolean) => void } | null>(null);

	ask(name: string): Promise<boolean> {
		this.pending?.resolve(false);
		return new Promise((resolve) => {
			this.pending = { name, resolve };
		});
	}

	answer(confirmed: boolean) {
		this.pending?.resolve(confirmed);
		this.pending = null;
	}
}

export const unassignConfirm = new UnassignConfirm();

interface IssueWithAgents {
	id: string;
	identifier: string;
	agent_assignees?: IssueAgentAssignee[];
}

/**
 * Assigns or unassigns one agent on an issue. Unassigning an agent whose
 * task is still working cancels that task, so it asks first. Returns the
 * issue's agents after the change, or null when nothing changed.
 */
export async function toggleIssueAgent(slug: string, issue: IssueWithAgents, agentId: string): Promise<IssueAgentAssignee[] | null> {
	const current = issue.agent_assignees ?? [];
	const assigned = current.find((agent) => agent.id === agentId);
	if (assigned && unassignCancelsTask(assigned) && !(await unassignConfirm.ask(assigned.name))) return null;
	try {
		const agents = await agentsApi.setForIssue(slug, issue.identifier, toggleAgent(current.map((agent) => agent.id), agentId));
		issuesState.setAgentAssignees(issue.identifier, agents);
		return agents;
	} catch (err) {
		const code = (err as { error?: { code?: string } })?.error?.code;
		if (code === 'AGENT_ASSIGN_FORBIDDEN') appToast.error(m['agents.assignee.forbidden']());
		else if (code === 'AGENT_NOT_ASSIGNABLE') appToast.error(m['agents.assignee.not_assignable']());
		else appToast.error(m['agents.assignee.failed']());
		return null;
	}
}
