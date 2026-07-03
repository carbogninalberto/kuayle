<script lang="ts">
	import { goto } from '$app/navigation';
	import { login, register } from '$lib/api/auth';
	import { Loader2 } from 'lucide-svelte';
	import { Input } from '$lib/components/ui/input';
	import { Password } from '$lib/components/ui/password';
	import { authState } from '$lib/features/auth/auth.state.svelte';
	import { listWorkspaces, createWorkspace } from '$lib/api/workspaces';
	import { toast } from 'svelte-sonner';

	let mode = $state<'login' | 'register'>('login');
	let email = $state('');
	let password = $state('');
	let name = $state('');
	let loading = $state(false);
	const authInputClass =
		'mt-1 h-auto w-full rounded-md border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-3 py-2 text-sm text-[var(--color-text-primary)] outline-none focus:border-[var(--app-accent)] focus-visible:border-[var(--app-accent)] focus-visible:ring-0';

	async function handleSubmit(e: Event) {
		e.preventDefault();
		loading = true;

		try {
			let user;
			if (mode === 'login') {
				user = await login({ email, password });
			} else {
				user = await register({ email, password, name });
			}
			authState.setUser(user);

			const workspaces = await listWorkspaces();
			if (workspaces.length > 0) {
				goto(`/${workspaces[0].slug}/inbox`);
			} else {
				// Create default workspace
				const slug = user.name
					.toLowerCase()
					.replace(/[^a-z0-9]/g, '-')
					.replace(/-+/g, '-');
				const ws = await createWorkspace(`${user.name}'s Workspace`, slug || 'my-workspace');
				goto(`/${ws.slug}/inbox`);
			}
		} catch (err: any) {
			toast.error(err?.error?.message || 'Something went wrong');
		} finally {
			loading = false;
		}
	}
</script>

<div class="flex min-h-screen items-center justify-center bg-[var(--color-bg)]">
	<div class="w-full max-w-sm space-y-6 p-8">
		<div class="text-center">
			<h1 class="text-2xl font-bold text-[var(--color-text-primary)]">Kuayle</h1>
			<p class="mt-1 text-sm text-[var(--color-text-secondary)]">
				{mode === 'login' ? 'Sign in to your account' : 'Create a new account'}
			</p>
		</div>

		<form onsubmit={handleSubmit} class="space-y-4">
			{#if mode === 'register'}
				<div>
					<label for="name" class="block text-sm text-[var(--color-text-secondary)]">Name</label>
					<Input id="name" type="text" bind:value={name} required class={authInputClass} />
				</div>
			{/if}

			<div>
				<label for="email" class="block text-sm text-[var(--color-text-secondary)]">Email</label>
				<Input id="email" type="email" bind:value={email} required class={authInputClass} />
			</div>

			<div>
				<label for="password" class="block text-sm text-[var(--color-text-secondary)]">Password</label>
				<Password id="password" bind:value={password} required minlength={8} class={authInputClass} />
			</div>

			<button
				type="submit"
				disabled={loading}
				class="w-full rounded-md bg-[var(--app-accent)] px-4 py-2 text-sm font-medium text-[var(--app-accent-foreground)] hover:bg-[var(--app-accent-hover)] disabled:opacity-50"
			>
				{#if loading}<Loader2 size={14} class="animate-spin" />{:else}{mode === 'login'
						? 'Sign in'
						: 'Create account'}{/if}
			</button>
		</form>

		<p class="text-center text-sm text-[var(--color-text-secondary)]">
			{mode === 'login' ? "Don't have an account?" : 'Already have an account?'}
			<button
				onclick={() => (mode = mode === 'login' ? 'register' : 'login')}
				class="text-[var(--app-accent)] hover:underline"
			>
				{mode === 'login' ? 'Sign up' : 'Sign in'}
			</button>
		</p>
	</div>
</div>
