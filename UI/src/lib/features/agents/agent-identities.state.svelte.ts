import { agentsApi, type AgentIdentity } from '$lib/api/agents';
import { agentsForTeam } from './agent-assignees';

// The workspace's agent identities, loaded once per workspace and shared by
// assignee pickers, filters, mentions and group headers.
class AgentIdentitiesState {
	agents = $state<AgentIdentity[]>([]);
	canAssign = $state(false);
	canManage = $state(false);
	private slug = '';
	private pending: Promise<void> | null = null;

	/** Loads the agents of a workspace unless they are already loaded. */
	ensure(slug: string): Promise<void> {
		if (!slug) return Promise.resolve();
		if (this.slug === slug && this.pending) return this.pending;
		return this.reload(slug);
	}

	reload(slug: string): Promise<void> {
		this.slug = slug;
		const request = agentsApi
			.list(slug)
			.then((result) => {
				if (this.slug !== slug) return;
				this.agents = result.agents ?? [];
				this.canAssign = result.can_assign;
				this.canManage = result.can_manage;
			})
			.catch(() => {
				// Agents are optional everywhere they appear; retry on next use.
				if (this.slug === slug && this.pending === request) this.pending = null;
			});
		this.pending = request;
		return request;
	}

	/** Agents the caller may assign or mention in the team. */
	forTeam(teamId: string | null | undefined): AgentIdentity[] {
		return this.canAssign ? agentsForTeam(this.agents, teamId) : [];
	}

	byId(id: string): AgentIdentity | undefined {
		return this.agents.find((agent) => agent.id === id);
	}
}

export const agentIdentities = new AgentIdentitiesState();
