import { goto } from '$app/navigation';
import { authState } from '$lib/features/auth/auth.state.svelte';

export async function requireAuth(): Promise<boolean> {
	if (authState.loading) {
		await authState.init();
	}
	if (!authState.authenticated) {
		goto('/login');
		return false;
	}
	return true;
}
