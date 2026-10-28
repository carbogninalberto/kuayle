import type { AgentSignIn } from '$lib/api/agent-sign-ins';

/** What a run of this agent will use: a saved token comes before the terminal sign-in. */
export type SignInState = 'token' | 'terminal' | 'expired' | 'none';

export function signInState(signIn: AgentSignIn | undefined): SignInState {
	if (signIn?.token && !signIn.token.expired) return 'token';
	if (signIn?.terminal) return 'terminal';
	if (signIn?.token) return 'expired';
	return 'none';
}

// Matches the server's check: one line of printable ASCII, no spaces.
const tokenPattern = /^[\x21-\x7e]{20,1024}$/;

/** Why a pasted token cannot be saved, or null when it can. */
export function tokenProblem(value: string): 'empty' | 'invalid' | null {
	const token = value.trim();
	if (!token) return 'empty';
	return tokenPattern.test(token) ? null : 'invalid';
}
