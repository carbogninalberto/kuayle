<script lang="ts">
	import PageTitle from '$lib/components/shared/PageTitle.svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { login, register } from '$lib/api/auth';
	import { ChevronDown, ChevronUp, Loader2 } from '@lucide/svelte';
	import { Input } from '$lib/components/ui/input';
	import { Password } from '$lib/components/ui/password';
	import { authState } from '$lib/features/auth/auth.state.svelte';
	import PasskeySignInButton from '$lib/features/auth/PasskeySignInButton.svelte';
	import { markPasskeyPromptPending } from '$lib/features/auth/passkey';
	import type { User } from '$lib/types/auth';
	import { listWorkspaces } from '$lib/api/workspaces';
	import { getConfig } from '$lib/api/invite';
	import { sso } from '$lib/api/sso';
	import {
		normalizeWorkspaceSlug,
		ssoErrorKey,
		ssoRequiredSlug,
		ssoStartUrl
	} from '$lib/features/sso/sso-rules';
	import { demoMode, demoUsers, type DemoUser } from '$lib/demo';
	import { appToast } from '$lib/features/toast/toast';
	import { m, getLocale, setLocale } from '$lib/features/i18n/messages';

	let mode = $state<'login' | 'register'>('login');
	let email = $state('');
	let password = $state('');
	let name = $state('');
	let loading = $state(false);
	let demoDrawerOpen = $state(false);
	let registrationEnabled = $state(true);
	let passkeysEnabled = $state(false);
	let ssoInstanceEnabled = $state(false);
	let ssoOpen = $state(false);
	let ssoSlug = $state('');
	let ssoBusy = $state(false);
	let ssoMessage = $state<string | null>(null);
	const inviteToken = $derived(page.url.searchParams.get('invite') ?? '');
	const ssoError = $derived(page.url.searchParams.get('sso_error'));
	const authInputClass =
		'mt-1 h-auto w-full rounded-md border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-3 py-2 text-sm text-[var(--color-text-primary)] outline-none focus:border-[var(--app-accent)] focus-visible:border-[var(--app-accent)] focus-visible:ring-0';

	onMount(async () => {
		try {
			const config = await getConfig();
			registrationEnabled = config.registration_enabled;
			passkeysEnabled = config.passkeys_enabled === true;
			ssoInstanceEnabled = config.sso_enabled_instance === true;
			// With an invite token, registration stays available even when
			// public registration is disabled (the backend accepts the token).
			if (!registrationEnabled && !inviteToken) mode = 'login';
		} catch {
			// Config unavailable — keep registration visible.
		}
	});

	async function handleSubmit(e: Event) {
		e.preventDefault();
		if (loading) return;
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
			// A password sign-in is the moment to offer a passkey for next time.
			if (mode === 'login') markPasskeyPromptPending();
			await enterApp(user);
		} catch (err: any) {
			const requiredSlug = ssoRequiredSlug(err);
			if (requiredSlug) {
				ssoSlug = requiredSlug;
				ssoOpen = true;
				ssoMessage = m['login.sso.required']();
			} else {
				appToast.apiError(err, m['login.error_generic']());
			}
		} finally {
			loading = false;
		}
	}

	async function startSSO(e: Event) {
		e.preventDefault();
		const slug = normalizeWorkspaceSlug(ssoSlug);
		if (!slug || ssoBusy) return;
		ssoBusy = true;
		ssoMessage = null;
		try {
			const info = await sso.publicInfo(slug);
			if (!info.enabled) {
				ssoMessage = m['login.sso.not_enabled']();
				return;
			}
			const after = page.url.pathname === '/login' ? '' : page.url.pathname + page.url.search;
			window.location.href = ssoStartUrl(slug, after);
		} catch {
			ssoMessage = m['login.sso.not_enabled']();
		} finally {
			ssoBusy = false;
		}
	}

	// Where a signed-in user goes next; the same for password and passkey.
	async function enterApp(user: User) {
		authState.setUser(user);

		if (inviteToken) {
			// The invite route resolves the intended workspace, including after atomic signup.
			await goto(`/invite/${encodeURIComponent(inviteToken)}`);
			return;
		}

		const workspaces = await listWorkspaces();
		if (workspaces.length > 0) {
			goto(`/${workspaces[0].slug}/inbox`);
		} else {
			goto('/workspace-setup');
		}
	}

	async function passkeySignedIn(user: User) {
		try {
			await enterApp(user);
		} catch (err: any) {
			appToast.apiError(err, m['login.error_generic']());
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
				<p
					class="mt-3 rounded-md border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-3 py-2 text-xs text-[var(--color-text-secondary)]"
				>
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
				<Input
					id="email"
					type="email"
					bind:value={email}
					required
					autocomplete={mode === 'login' ? 'username webauthn' : 'email'}
					class={authInputClass}
				/>
			</div>

			<div>
				<label for="password" class="block text-sm text-[var(--color-text-secondary)]">{m['login.password']()}</label>
				<Password id="password" bind:value={password} required minlength={8} class={authInputClass} />
			</div>

			<Button variant="plain" type="submit"
				disabled={loading}
				class="inline-flex h-9 w-full items-center justify-center rounded-md bg-[var(--app-accent)] px-4 text-sm font-medium text-[var(--app-accent-foreground)] hover:bg-[var(--app-accent-hover)] disabled:opacity-50"
			>
				{#if loading}<Loader2 size={14} class="animate-spin" />{:else}{mode === 'login'
						? m['login.button.signin']()
						: m['login.button.create_account']()}{/if}
			</Button>
			{#if mode === 'login'}
				<p class="text-center text-sm">
					<a href="/forgot-password" class="text-[var(--app-accent)] hover:underline">{m['login.forgot_password']()}</a>
				</p>
			{/if}
		</form>

		{#if mode === 'login'}
			<PasskeySignInButton enabled={passkeysEnabled} disabled={loading} autofill onSignedIn={passkeySignedIn} />

			{#if ssoError || ssoInstanceEnabled || ssoOpen}
				<div class="space-y-2 rounded-md border border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-3">
					{#if ssoError}
						<p class="text-sm text-[var(--status-error)]" role="alert">{m[ssoErrorKey(ssoError)]()}</p>
					{/if}
					{#if !ssoOpen}
						<Button variant="plain" type="button"
							onclick={() => (ssoOpen = true)}
							class="w-full rounded-md border border-[var(--app-border)] px-4 py-2 text-sm font-medium text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)]"
						>
							{m['login.sso.button']()}
						</Button>
					{:else}
						<form onsubmit={startSSO} class="space-y-2">
							<label for="sso-workspace" class="block text-sm text-[var(--color-text-secondary)]">
								{m['login.sso.slug_label']()}
							</label>
							<Input
								id="sso-workspace"
								bind:value={ssoSlug}
								placeholder={m['login.sso.slug_placeholder']()}
								autocomplete="off"
								class={authInputClass}
							/>
							{#if ssoMessage}
								<p class="text-xs text-[var(--status-error)]" role="alert">{ssoMessage}</p>
							{/if}
							<div class="flex gap-2">
								<Button variant="plain" type="submit" disabled={ssoBusy || !ssoSlug.trim()}
									class="inline-flex h-9 flex-1 items-center justify-center rounded-md bg-[var(--app-accent)] px-4 text-sm font-medium text-[var(--app-accent-foreground)] hover:bg-[var(--app-accent-hover)] disabled:opacity-50"
								>
									{#if ssoBusy}<Loader2 size={14} class="animate-spin" />{:else}{m['login.sso.continue']()}{/if}
								</Button>
								<Button variant="plain" type="button"
									onclick={() => { ssoOpen = false; ssoMessage = null; }}
									class="rounded-md border border-[var(--app-border)] px-3 py-2 text-sm text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]"
								>
									{m['login.sso.cancel']()}
								</Button>
							</div>
						</form>
					{/if}
				</div>
			{:else}
				<Button variant="plain" type="button"
					onclick={() => (ssoOpen = true)}
					class="w-full rounded-md border border-[var(--app-border)] px-4 py-2 text-sm font-medium text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)]"
				>
					{m['login.sso.button']()}
				</Button>
			{/if}
		{/if}

		{#if registrationEnabled || inviteToken}
			<p class="text-center text-sm text-[var(--color-text-secondary)]">
				{mode === 'login' ? m['login.no_account']() : m['login.has_account']()}
				<Button variant="plain" onclick={() => (mode = mode === 'login' ? 'register' : 'login')}
					disabled={loading}
					class="text-[var(--app-accent)] hover:underline"
				>
					{mode === 'login' ? m['login.button.sign_up']() : m['login.button.signin']()}
				</Button>
			</p>
		{/if}
	</div>

	{#if demoMode && mode === 'login'}
		<div
			class="fixed right-4 bottom-4 z-50 flex w-[calc(100vw-2rem)] max-w-sm flex-col items-end gap-3 sm:right-6 sm:bottom-6"
		>
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
						<Button variant="plain" type="button"
							onclick={() => (demoDrawerOpen = false)}
							aria-label={m['login.demo.aria_close']()}
							class="rounded px-2 py-1 text-xs text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg)] hover:text-[var(--color-text-primary)]"
						>
							{m['login.demo.close']()}
						</Button>
					</div>

					<div class="max-h-[60vh] space-y-2 overflow-y-auto pr-1">
						{#each demoUsers as user}
							<Button variant="plain" type="button"
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
							</Button>
						{/each}
					</div>
				</section>
			{/if}

			<Button variant="plain" type="button"
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
			</Button>
		</div>
	{/if}
</div>

<PageTitle parts={[mode === 'login' ? m['login.button.signin']() : m['login.button.create_account']()]} />
