// Where the assistant composer keeps an unsent draft, per person, workspace
// and conversation. Other features prefill a conversation through it.
export function assistantDraftKey(userId: string | undefined, slug: string, conversationId: string | null): string {
	return `assistant-draft:${userId}:${slug}:${conversationId ?? 'new'}`;
}
