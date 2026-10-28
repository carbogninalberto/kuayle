import { api } from './client';

// The current user's own sign-in for a coding agent. The API never returns
// the sign-in itself, and nobody else (admins included) can read it.
export interface AgentSignIn {
	provider: string;
	/** The agent accepts a pasted token (Claude Code's `claude setup-token`). */
	supports_token: boolean;
	/** expires_at is estimated as one year after the token was saved. */
	token: { saved_at: string; expires_at: string | null; expired: boolean } | null;
	/** A sign-in captured from the machine's terminal. */
	terminal: { saved_at: string } | null;
}

const base = (slug: string) => `/api/workspaces/${encodeURIComponent(slug)}/agent-sign-ins`;

export const agentSignIns = {
	list: (slug: string) => api.get<AgentSignIn[]>(base(slug)),
	saveToken: (slug: string, provider: string, token: string) =>
		api.put<AgentSignIn>(`${base(slug)}/${encodeURIComponent(provider)}/token`, { token }),
	/** Removes the token, the terminal sign-in, or both when kind is omitted. */
	remove: (slug: string, provider: string, kind?: 'token' | 'credentials') =>
		api.delete<void>(`${base(slug)}/${encodeURIComponent(provider)}${kind ? `?kind=${kind}` : ''}`)
};
