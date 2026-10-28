import type { AgentReview, AgentSession } from '$lib/api/agent-tasks';

// The coding agents that can review a pull request, in menu order.
export const reviewHarnessIds = ['claude-code', 'codex', 'opencode'] as const;

// The review harnesses the workspace allows, with the task's own harness first
// so the default matches the implementer while any other can be picked.
export function reviewHarnesses(allowed: string[], implementer: string): string[] {
	const available = reviewHarnessIds.filter((id) => allowed.includes(id));
	return [...available.filter((id) => id === implementer), ...available.filter((id) => id !== implementer)];
}

// Review sessions with their review, newest first; the card shows them as
// review cards and the implement session in its execution details.
export function reviewSessions(sessions: AgentSession[]): (AgentSession & { review: AgentReview })[] {
	return sessions
		.filter((s): s is AgentSession & { review: AgentReview } => s.purpose === 'review' && !!s.review)
		.sort((a, b) => Date.parse(b.created_at) - Date.parse(a.created_at));
}

export type ReviewTone = 'success' | 'error' | 'warning' | 'neutral' | 'running';

// The tone of a review card: the reviewer's verdict once it finished,
// otherwise its session's progress. An unknown verdict is never a pass.
export function reviewTone(
	session: Pick<AgentSession, 'state'> & { review: Pick<AgentReview, 'verdict'> }
): ReviewTone {
	if (session.state === 'failed' || session.state === 'cancelled') return 'error';
	if (session.state !== 'completed') return 'running';
	switch (session.review.verdict) {
		case 'approve':
			return 'success';
		case 'request_changes':
			return 'warning';
		default:
			return 'neutral';
	}
}
