<script lang="ts">
	import { goto } from '$app/navigation';
	import { login } from '$lib/api/auth';
	import { Loader2 } from 'lucide-svelte';
	import { Input } from '$lib/components/ui/input';
	import { Password } from '$lib/components/ui/password';
	import { authState } from './auth.state.svelte';
	import { appToast } from '$lib/features/toast/toast';

	let email = $state('');
	let password = $state('');
	let loading = $state(false);
	const authInputClass =
		'h-auto w-full rounded border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-3 py-2 text-sm text-[var(--color-text-primary)] focus-visible:ring-0';

	async function handleSubmit(e: Event) {
		e.preventDefault();
		loading = true;
		try {
			const user = await login({ email, password });
			authState.setUser(user);
			goto('/');
		} catch (err: any) {
			appToast.apiError(err, 'Login failed');
		} finally {
			loading = false;
		}
	}
</script>

<form onsubmit={handleSubmit} class="space-y-4">
	<Input
		type="email"
		bind:value={email}
		placeholder="Email"
		required
		class={authInputClass}
	/>
	<Password
		bind:value={password}
		placeholder="Password"
		required
		class={authInputClass}
	/>
	<button
		type="submit"
		disabled={loading}
		class="w-full rounded bg-[var(--app-accent)] py-2 text-sm text-[var(--app-accent-foreground)] disabled:opacity-50"
	>
		{#if loading}<Loader2 size={14} class="animate-spin" />{:else}Sign in{/if}
	</button>
</form>
