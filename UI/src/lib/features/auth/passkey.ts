import {
	beginPasskeyLogin,
	beginPasskeyRegistration,
	finishPasskeyLogin,
	finishPasskeyRegistration,
	type Passkey
} from '$lib/api/passkeys';
import type { User } from '$lib/types/auth';
import { credentialToJSON, toCreationOptions, toRequestOptions } from './webauthn';

/**
 * Signs in with a passkey the browser or a security key offers. With
 * `conditional` the request waits in the email field's autofill instead of
 * opening a prompt; `signal` cancels it when another request takes over.
 * Resolves to null when the browser returns no credential.
 */
export async function signInWithPasskey(options: { conditional?: boolean; signal?: AbortSignal } = {}): Promise<User | null> {
	const request = toRequestOptions(await beginPasskeyLogin());
	const credential = await navigator.credentials.get({
		...request,
		signal: options.signal,
		...(options.conditional ? { mediation: 'conditional' as CredentialMediationRequirement } : {})
	});
	if (!credential) return null;
	return finishPasskeyLogin(credentialToJSON(credential));
}

/**
 * Adds a passkey to the signed-in account. The server asks for the password
 * (error code REAUTH_REQUIRED) when the session is not fresh.
 */
export async function registerPasskey(name: string, password?: string): Promise<Passkey | null> {
	const creation = toCreationOptions(await beginPasskeyRegistration(password));
	const credential = await navigator.credentials.create(creation);
	if (!credential) return null;
	return finishPasskeyRegistration(name, credentialToJSON(credential));
}

// Remembered per browser so the offer after sign-in appears once per account.
const promptDismissedKey = (userId: string) => `kuayle:passkey-prompt-dismissed:${userId}`;
const promptPendingKey = 'kuayle:passkey-prompt-pending';

/** Called after a password sign-in: the next page may offer a passkey. */
export function markPasskeyPromptPending(): void {
	try {
		sessionStorage.setItem(promptPendingKey, '1');
	} catch {
		// Storage unavailable: no offer.
	}
}

/** Whether to offer a passkey now; the pending mark is consumed either way. */
export function takePasskeyPrompt(userId: string): boolean {
	try {
		if (sessionStorage.getItem(promptPendingKey) !== '1') return false;
		sessionStorage.removeItem(promptPendingKey);
		return localStorage.getItem(promptDismissedKey(userId)) !== '1';
	} catch {
		return false;
	}
}

export function dismissPasskeyPrompt(userId: string): void {
	try {
		localStorage.setItem(promptDismissedKey(userId), '1');
	} catch {
		// Storage unavailable: the offer may appear again.
	}
}
