<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { getInvitePreview, acceptInvite, type InvitePreview } from '$lib/api/invite';
	import { authState } from '$lib/features/auth/auth.state.svelte';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/paraglide/messages.js';
	import { Loader2, Link2Off } from 'lucide-svelte';

	const token = $derived(page.params.token ?? '');

	let loading = $state(true);
	let preview = $state<InvitePreview | null>(null);
	let invalidReason = $state<'expired' | 'revoked' | 'exhausted' | 'not_found' | null>(null);
	let joining = $state(false);
	let loadFailed = $state(false);

	let loadVersion = 0;
	$effect(() => {
		void loadInvite(token);
		return () => {
			++loadVersion;
		};
	});

	async function loadInvite(currentToken: string) {
		const version = ++loadVersion;
		loading = true;
		loadFailed = false;
		invalidReason = null;
		preview = null;
		try {
			const result = await getInvitePreview(currentToken);
			if (version !== loadVersion) return;
			preview = result;
		} catch (err: any) {
			if (version !== loadVersion) return;
			if (err?.error?.code === 'INVITE_NOT_FOUND' || err?.error?.code === 'NOT_FOUND') {
				invalidReason = 'not_found';
			} else {
				loadFailed = true;
			}
			loading = false;
			return;
		}
		// Hydrate the session without the global 401 redirect, even for exhausted links.
		await authState.init(false);
		if (version !== loadVersion) return;
		if (!authState.authenticated) {
			if (preview.status === 'valid') {
				await goto(`/login?invite=${encodeURIComponent(currentToken)}`);
			} else {
				invalidReason = preview.status;
			}
		} else if (preview.status !== 'valid') {
			try {
				const res = await acceptInvite(currentToken);
				if (version !== loadVersion) return;
				await goto(`/${res.workspace_slug}/inbox`);
			} catch (err: any) {
				if (version !== loadVersion) return;
				const code = err?.error?.code;
				if (['INVITE_EXPIRED', 'INVITE_REVOKED', 'INVITE_EXHAUSTED', 'NOT_FOUND'].includes(code)) {
					invalidReason = preview.status;
				} else {
					loadFailed = true;
				}
			}
		}
		loading = false;
	}

	async function handleJoin() {
		if (joining) return;
		joining = true;
		try {
			const res = await acceptInvite(token);
			appToast.success(m['invite.joined']({ name: res.workspace_name }));
			goto(`/${res.workspace_slug}/inbox`);
		} catch (err: any) {
			const code = err?.error?.code;
			if (code === 'INVITE_EXPIRED') invalidReason = 'expired';
			else if (code === 'INVITE_REVOKED') invalidReason = 'revoked';
			else if (code === 'INVITE_EXHAUSTED') invalidReason = 'exhausted';
			else if (code === 'INVITE_NOT_FOUND' || code === 'NOT_FOUND') invalidReason = 'not_found';
			else if (code === 'UNAUTHORIZED') await goto(`/login?invite=${encodeURIComponent(token)}`);
			else appToast.apiError(err, m['invite.failed_accept']());
		} finally {
			joining = false;
		}
	}
</script>

<svelte:head>
	<title>{preview ? `${preview.workspace_name} - ${m['invite.title']()}` : m['invite.title']()}</title>
</svelte:head>

<div class="flex min-h-screen items-center justify-center bg-[var(--color-bg)]">
	{#if loading}
		<span class="sr-only">{m['invite.loading']()}</span>
		<Loader2 size={20} class="animate-spin text-[var(--color-text-tertiary)]" />
	{:else if loadFailed}
		<div class="w-full max-w-sm space-y-4 p-8 text-center" role="alert">
			<p class="text-sm text-[var(--color-text-secondary)]">{m['invite.failed_load']()}</p>
			<button onclick={() => loadInvite(token)} class="text-sm text-[var(--app-accent)] hover:underline"
				>{m['invite.retry']()}</button
			>
		</div>
	{:else if invalidReason}
		<div class="w-full max-w-sm space-y-4 p-8 text-center">
			<Link2Off size={32} class="mx-auto text-[var(--color-text-tertiary)]" />
			<h1 class="text-xl font-semibold text-[var(--color-text-primary)]">{m['invite.invalid.title']()}</h1>
			<p class="text-sm text-[var(--color-text-secondary)]">
				{(m as unknown as Record<string, () => string>)['invite.invalid.' + invalidReason]()}
			</p>
			<button onclick={() => goto('/login')} class="text-sm text-[var(--app-accent)] hover:underline">
				{m['invite.back_to_login']()}
			</button>
		</div>
	{:else if preview}
		<div class="w-full max-w-sm space-y-6 p-8 text-center">
			{#if preview.workspace_logo_url}
				<img src={preview.workspace_logo_url} alt={preview.workspace_name} class="mx-auto h-14 w-14 rounded-lg" />
			{:else}
				<div
					class="mx-auto flex h-14 w-14 items-center justify-center rounded-lg bg-[var(--app-accent)] text-xl font-semibold text-[var(--app-accent-foreground)]"
				>
					{preview.workspace_name.charAt(0).toUpperCase()}
				</div>
			{/if}
			<div>
				<h1 class="text-xl font-semibold text-[var(--color-text-primary)]">
					{m['invite.heading']({ name: preview.workspace_name })}
				</h1>
				<p class="mt-1 text-sm text-[var(--color-text-secondary)]">
					{m['invite.role_desc']({
						role: (m as unknown as Record<string, () => string>)['common.role.' + preview.role]()
					})}
				</p>
			</div>
			<button
				onclick={handleJoin}
				disabled={joining}
				class="flex w-full items-center justify-center gap-2 rounded-md bg-[var(--app-accent)] px-4 py-2 text-sm font-medium text-[var(--app-accent-foreground)] hover:bg-[var(--app-accent-hover)] disabled:opacity-50"
			>
				{#if joining}
					<Loader2 size={14} class="animate-spin" />
					{m['invite.joining']()}
				{:else}
					{m['invite.join']()}
				{/if}
			</button>
		</div>
	{/if}
</div>
