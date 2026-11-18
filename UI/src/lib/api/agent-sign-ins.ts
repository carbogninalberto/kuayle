import { api } from './client';

// The current user's own sign-in for a coding agent. The API never returns
// the sign-in itself, and nobody else (admins included) can read it.
export interface AgentSignIn {
	provider: string;
	/** The agent accepts a pasted token (Claude Code's `claude setup-token`). */
	supports_token: boolean;
	/** expires_at is estimated as one year after the token was saved. */
	token: { saved_at: string; expires_at: string | null; expired: boolean } | null;
	/** Saved CLI credentials, whether captured from a terminal or connected from Settings. */
	terminal: { saved_at: string } | null;
}

export interface CodexDeviceChallenge {
	attempt_id: string;
	verification_url: string;
	user_code: string;
	expires_at: string;
}

type CodexDeviceStatus = 'pending' | 'connected' | 'expired' | 'failed';

const base = (slug: string) => `/api/workspaces/${encodeURIComponent(slug)}/agent-sign-ins`;

const codexDevice = (slug: string) => `${base(slug)}/codex/device`;

export const agentSignIns = {
	startCodexDevice: (slug: string) => api.post<CodexDeviceChallenge>(`${codexDevice(slug)}/start`),
	pollCodexDevice: (slug: string, attempt_id: string, signal: AbortSignal) =>
		api.post<{ status: CodexDeviceStatus }>(`${codexDevice(slug)}/poll`, { attempt_id }, { signal }),
	cancelCodexDevice: (slug: string, attempt_id: string) =>
		api.post<void>(`${codexDevice(slug)}/cancel`, { attempt_id }, { redirectOnUnauthorized: false, retryUnauthorized: false, keepalive: true }),
	list: (slug: string) => api.get<AgentSignIn[]>(base(slug)),
	saveToken: (slug: string, provider: string, token: string) =>
		api.put<AgentSignIn>(`${base(slug)}/${encodeURIComponent(provider)}/token`, { token }),
	/** Removes the token, the terminal sign-in, or both when kind is omitted. */
	remove: (slug: string, provider: string, kind?: 'token' | 'credentials') =>
		api.delete<void>(`${base(slug)}/${encodeURIComponent(provider)}${kind ? `?kind=${kind}` : ''}`)
};
