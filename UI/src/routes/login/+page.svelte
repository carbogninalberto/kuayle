<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { login, register } from '$lib/api/auth';
	import { ChevronDown, ChevronUp, Loader2 } from 'lucide-svelte';
	import { Input } from '$lib/components/ui/input';
	import { Password } from '$lib/components/ui/password';
	import { authState } from '$lib/features/auth/auth.state.svelte';
	import { listWorkspaces } from '$lib/api/workspaces';
	import { getConfig } from '$lib/api/invite';
	import { demoMode, demoUsers, type DemoUser } from '$lib/demo';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/paraglide/messages.js';
	import { getLocale, setLocale } from '$lib/paraglide/runtime.js';

	let mode = $state<'login' | 'register'>('login');
	let email = $state('');
	let password = $state('');
	let name = $state('');
	let loading = $state(false);
	let demoDrawerOpen = $state(false);
	let registrationEnabled = $state(true);
	const inviteToken = $derived(page.url.searchParams.get('invite') ?? '');
	const authInputClass =
		'mt-1 h-auto w-full rounded-md border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-3 py-2 text-sm text-[var(--color-text-primary)] outline-none focus:border-[var(--app-accent)] focus-visible:border-[var(--app-accent)] focus-visible:ring-0';

	onMount(async () => {
		try {
			const config = await getConfig();
			registrationEnabled = config.registration_enabled;
			// With an invite token, registration stays available even when
			// public registration is disabled (the backend accepts the token).
			if (!registrationEnabled) mode = inviteToken ? 'register' : 'login';
		} catch {
			// Config unavailable — keep registration visible.
		}
	});

	async function handleSubmit(e: Event) {
		e.preventDefault();
		loading = true;

		try {
			let user;
			if (mode === 'login') {
				user = await login({ email, password });
			} else {
				user = await register({
					email,
					password,
					name,
					...(inviteToken ? { invite_token: inviteToken } : {})
				});
			}
			authState.setUser(user);

			if (inviteToken && mode === 'login') {
				// Login does not redeem the invite — let the invite accept page finish joining.
				goto(`/invite/${encodeURIComponent(inviteToken)}`);
				return;
			}
			// For register, the backend already redeemed the invite token and added the
			// membership, so fall through to the normal workspace landing below.

			const workspaces = await listWorkspaces();
			if (workspaces.length > 0) {
				goto(`/${workspaces[0].slug}/inbox`);
			} else {
				goto('/workspace-setup');
			}
		} catch (err: any) {
			appToast.apiError(err, m['login.error_generic']());
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
			<img src="/favicon.svg" alt={m['login.logo_alt']()} class="mx-auto mb-4 h-14 w-14" />
			<h1 class="text-2xl font-bold text-[var(--color-text-primary)]">Kuayle</h1>
			<p class="mt-1 text-sm text-[var(--color-text-secondary)]">
				{mode === 'login' ? m['login.subtitle.signin']() : m['login.subtitle.register']()}
			</p>
			{#if inviteToken}
				<p class="mt-3 rounded-md border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-3 py-2 text-xs text-[var(--color-text-secondary)]">
					{m['login.invite_notice']()}
				</p>
			{/if}
		</div>

		<form onsubmit={handleSubmit} class="space-y-4">
			{#if mode === 'register'}
				<div>
					<label for="name" class="block text-sm text-[var(--color-text-secondary)]">{m['login.name']()}</label>
					<Input id="name" type="text" bind:value={name} required class={authInputClass} />
				</div>
			{/if}

			<div>
				<label for="email" class="block text-sm text-[var(--color-text-secondary)]">{m['login.email']()}</label>
				<Input id="email" type="email" bind:value={email} required class={authInputClass} />
			</div>

			<div>
				<label for="password" class="block text-sm text-[var(--color-text-secondary)]">{m['login.password']()}</label>
				<Password id="password" bind:value={password} required minlength={8} class={authInputClass} />
			</div>

			<button
				type="submit"
				disabled={loading}
				class="w-full rounded-md bg-[var(--app-accent)] px-4 py-2 text-sm font-medium text-[var(--app-accent-foreground)] hover:bg-[var(--app-accent-hover)] disabled:opacity-50"
			>
				{#if loading}<Loader2 size={14} class="animate-spin" />{:else}{mode === 'login'
						? m['login.button.signin']()
						: m['login.button.create_account']()}{/if}
			</button>
		</form>

		{#if registrationEnabled || inviteToken}
			<p class="text-center text-sm text-[var(--color-text-secondary)]">
				{mode === 'login' ? m['login.no_account']() : m['login.has_account']()}
				<button
					onclick={() => (mode = mode === 'login' ? 'register' : 'login')}
					class="text-[var(--app-accent)] hover:underline"
				>
					{mode === 'login' ? m['login.button.sign_up']() : m['login.button.signin']()}
				</button>
			</p>
		{/if}
	</div>

	{#if demoMode && mode === 'login'}
		<div class="fixed right-4 bottom-4 z-50 flex w-[calc(100vw-2rem)] max-w-sm flex-col items-end gap-3 sm:right-6 sm:bottom-6">
			{#if demoDrawerOpen}
				<section
					aria-label={m['login.demo.aria_details']()}
					class="w-full origin-bottom-right rounded-xl border border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-4 text-sm text-[var(--color-text-secondary)] shadow-2xl"
				>
					<div class="mb-3 flex items-start justify-between gap-4">
						<div>
							<h2 class="font-medium text-[var(--color-text-primary)]">{m['login.demo.title']()}</h2>
							<p class="mt-1 text-xs">{m['login.demo.description']()}</p>
						</div>
						<button
							type="button"
							onclick={() => (demoDrawerOpen = false)}
							aria-label={m['login.demo.aria_close']()}
							class="rounded px-2 py-1 text-xs text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg)] hover:text-[var(--color-text-primary)]"
						>
							{m['login.demo.close']()}
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
									<span class="text-xs text-[var(--app-accent)]">{m['login.demo.use']()}</span>
								</div>
								<dl class="mt-2 space-y-1 text-xs">
									<div class="flex justify-between gap-3">
										<dt>{m['login.email']()}</dt>
										<dd class="font-mono text-[var(--color-text-primary)]">{user.email}</dd>
									</div>
									<div class="flex justify-between gap-3">
										<dt>{m['login.password']()}</dt>
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
				{m['login.demo.users_button']()}
			</button>
		</div>
	{/if}
</div>
