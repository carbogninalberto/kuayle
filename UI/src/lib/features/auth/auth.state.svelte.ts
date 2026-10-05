import type { User } from '$lib/types/auth';
import { getMe } from '$lib/api/auth';

class AuthState {
	user = $state<User | null>(null);
	loading = $state(true);
	authenticated = $derived(this.user !== null);

	async init(redirectOnUnauthorized = true) {
		try {
			this.user = await getMe(redirectOnUnauthorized);
		} catch {
			this.user = null;
		} finally {
			this.loading = false;
		}
	}

	setUser(user: User | null) {
		this.user = user;
	}

	clear() {
		this.user = null;
	}
}

export const authState = new AuthState();
