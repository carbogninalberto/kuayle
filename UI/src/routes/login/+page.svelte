<script lang="ts">
	import { goto } from '$app/navigation';
	import { login, register } from '$lib/api/auth';
	import { ChevronDown, ChevronUp, Loader2 } from 'lucide-svelte';
	import { Input } from '$lib/components/ui/input';
	import { Password } from '$lib/components/ui/password';
	import { authState } from '$lib/features/auth/auth.state.svelte';
	import { listWorkspaces, createWorkspace } from '$lib/api/workspaces';
	import { demoMode, demoUsers, type DemoUser } from '$lib/demo';
	import { toast } from 'svelte-sonner';

	let mode = $state<'login' | 'register'>('login');
	let email = $state('');
	let password = $state('');
	let name = $state('');
	let loading = $state(false);
	let demoDrawerOpen = $state(false);
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

	function useDemoUser(user: DemoUser) {
		mode = 'login';
		email = user.email;
		password = user.password;
		demoDrawerOpen = false;
	}
</script>

<div class="flex min-h-screen items-center justify-center bg-[var(--color-bg)]">
	<div class="w-full max-w-sm space-y-6 p-8">
		<div class="text-center">
			<img src="/favicon.svg" alt="Kuayle logo" class="mx-auto mb-4 h-14 w-14" />
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

	{#if demoMode && mode === 'login'}
		<div class="fixed right-4 bottom-4 z-50 flex w-[calc(100vw-2rem)] max-w-sm flex-col items-end gap-3 sm:right-6 sm:bottom-6">
			{#if demoDrawerOpen}
				<section
					aria-label="Demo login details"
					class="w-full origin-bottom-right rounded-xl border border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-4 text-sm text-[var(--color-text-secondary)] shadow-2xl"
				>
					<div class="mb-3 flex items-start justify-between gap-4">
						<div>
							<h2 class="font-medium text-[var(--color-text-primary)]">Demo login</h2>
							<p class="mt-1 text-xs">Pick a seeded user to fill the login form.</p>
						</div>
						<button
							type="button"
							onclick={() => (demoDrawerOpen = false)}
							aria-label="Close demo login drawer"
							class="rounded px-2 py-1 text-xs text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg)] hover:text-[var(--color-text-primary)]"
						>
							Close
						</button>
					</div>

					<div class="max-h-[60vh] space-y-2 overflow-y-auto pr-1">
						{#each demoUsers as user}
							<button
								type="button"
								onclick={() => useDemoUser(user)}
								class="w-full rounded-lg border border-[var(--app-border)] bg-[var(--color-bg)] p-3 text-left transition hover:border-[var(--app-accent)] hover:bg-[var(--color-bg-secondary)]"
							>
								<div class="flex items-center justify-between gap-3">
									<p class="font-medium text-[var(--color-text-primary)]">{user.label}</p>
									<span class="text-xs text-[var(--app-accent)]">Use</span>
								</div>
								<dl class="mt-2 space-y-1 text-xs">
									<div class="flex justify-between gap-3">
										<dt>Email</dt>
										<dd class="font-mono text-[var(--color-text-primary)]">{user.email}</dd>
									</div>
									<div class="flex justify-between gap-3">
										<dt>Password</dt>
										<dd class="font-mono text-[var(--color-text-primary)]">{user.password}</dd>
									</div>
								</dl>
							</button>
						{/each}
					</div>
				</section>
			{/if}

			<button
				type="button"
				onclick={() => (demoDrawerOpen = !demoDrawerOpen)}
				aria-expanded={demoDrawerOpen}
				class="inline-flex items-center gap-2 rounded-full bg-[var(--app-accent)] px-4 py-2 text-sm font-medium text-[var(--app-accent-foreground)] shadow-lg transition hover:bg-[var(--app-accent-hover)]"
			>
				{#if demoDrawerOpen}
					<ChevronDown size={14} aria-hidden="true" />
				{:else}
					<ChevronUp size={14} aria-hidden="true" />
				{/if}
				Demo users
			</button>
		</div>
	{/if}
</div>
