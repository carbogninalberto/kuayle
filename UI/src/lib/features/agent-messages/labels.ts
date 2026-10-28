import { m } from '$lib/paraglide/messages.js';
import { agentMessages, type AgentMessage, type AgentMessageState } from '$lib/api/agent-messages';
import type { MessageAction } from './agent-messages';

export const stateLabel = (state: AgentMessageState) =>
	({
		queued: m['agent_messages.state_queued'],
		held: m['agent_messages.state_held'],
		delivered: m['agent_messages.state_delivered'],
		dropped: m['agent_messages.state_dropped'],
		rejected: m['agent_messages.state_rejected']
	})[state]();

export const actionLabel = (action: MessageAction) =>
	({
		hold: m['agent_messages.hold'],
		release: m['agent_messages.release'],
		drop: m['agent_messages.drop'],
		hold_conversation: m['agent_messages.hold_conversation'],
		release_conversation: m['agent_messages.release_conversation']
	})[action]();

// Runs a moderator's action; the API re-checks the permission and audits it.
export async function moderate(slug: string, message: AgentMessage, action: MessageAction): Promise<void> {
	switch (action) {
		case 'hold':
		case 'release':
		case 'drop':
			await agentMessages.act(slug, message.id, action);
			return;
		case 'hold_conversation':
			if (message.to_issue_id) await agentMessages.holdConversation(slug, [message.from_issue_id, message.to_issue_id]);
			return;
		case 'release_conversation':
			if (message.conversation_hold_id) await agentMessages.releaseConversation(slug, message.conversation_hold_id);
	}
}
