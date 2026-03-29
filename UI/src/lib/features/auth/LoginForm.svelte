<script lang="ts">
	import { goto } from '$app/navigation';
	import { login } from '$lib/api/auth';
	import { Loader2 } from 'lucide-svelte';
	import { authState } from './auth.state.svelte';
	import { toast } from 'svelte-sonner';

	let email = $state('');
	let password = $state('');
	let loading = $state(false);

	async function handleSubmit(e: Event) {
		e.preventDefault();
		loading = true;
		try {
			const user = await login({ email, password });
			authState.setUser(user);
			goto('/');
		} catch (err: any) {
			toast.error(err?.error?.message || 'Login failed');
		} finally {
			loading = false;
		}
	}
</script>

<form onsubmit={handleSubmit} class="space-y-4">
	<input
		type="email"
		bind:value={email}
		placeholder="Email"
		required
		class="w-full rounded border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-3 py-2 text-sm text-[var(--color-text-primary)]"
	/>
	<input
		type="password"
		bind:value={password}
		placeholder="Password"
		required
		class="w-full rounded border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-3 py-2 text-sm text-[var(--color-text-primary)]"
	/>
	<button
		type="submit"
		disabled={loading}
		class="w-full rounded bg-[var(--app-accent)] py-2 text-sm text-[var(--app-accent-foreground)] disabled:opacity-50"
	>
		{#if loading}<Loader2 size={14} class="animate-spin" />{:else}Sign in{/if}
	</button>
</form>
