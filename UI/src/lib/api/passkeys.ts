import { api } from './client';
import type { User } from '$lib/types/auth';
import type { PasskeyOptionsJSON } from '$lib/features/auth/webauthn';

export interface Passkey {
	id: string;
	name: string;
	transports: string[];
	aaguid: string;
	backed_up: boolean;
	created_at: string;
	last_used_at: string | null;
	/** The passkey this session signed in with. */
	current: boolean;
}

// Sign-in is anonymous: a 401 is the answer, not a session to refresh.
const anonymous = { redirectOnUnauthorized: false, retryUnauthorized: false };

export function beginPasskeyLogin(): Promise<PasskeyOptionsJSON> {
	return api.post<PasskeyOptionsJSON>('/api/auth/passkey/login/begin', undefined, anonymous);
}

export function finishPasskeyLogin(credential: Record<string, unknown>): Promise<User> {
	return api.post<User>('/api/auth/passkey/login/complete', { credential }, anonymous);
}

/** The password is needed only when the server answers REAUTH_REQUIRED. */
export function beginPasskeyRegistration(password?: string): Promise<PasskeyOptionsJSON> {
	return api.post<PasskeyOptionsJSON>('/api/auth/passkey/register/begin', password ? { password } : {});
}

export function finishPasskeyRegistration(name: string, credential: Record<string, unknown>): Promise<Passkey> {
	return api.post<Passkey>('/api/auth/passkey/register/complete', { name, credential });
}

export function listPasskeys(): Promise<Passkey[]> {
	return api.get<Passkey[]>('/api/auth/passkey/credentials');
}

export function renamePasskey(id: string, name: string): Promise<Passkey> {
	return api.patch<Passkey>(`/api/auth/passkey/credentials/${id}`, { name });
}

export function deletePasskey(id: string): Promise<void> {
	return api.delete<void>(`/api/auth/passkey/credentials/${id}`);
}
