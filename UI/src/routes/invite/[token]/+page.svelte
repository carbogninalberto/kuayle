<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { getInvitePreview, acceptInvite, type InvitePreview } from '$lib/api/invite';
	import { authState } from '$lib/features/auth/auth.state.svelte';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/paraglide/messages.js';
	import { getLocale } from '$lib/paraglide/runtime.js';
	import { Loader2, Link2Off } from 'lucide-svelte';

	const token = $derived(page.params.token ?? '');

	let loading = $state(true);
	let preview = $state<InvitePreview | null>(null);
	let invalidReason = $state<'expired' | 'revoked' | 'exhausted' | 'not_found' | null>(null);
	let joining = $state(false);

	onMount(async () => {
		// Fetch the public preview first: for an invalid invite we never call
		// authState.init(), so the ApiClient's 401 → /login redirect can't fire
		// and the invalid state stays visible to logged-out visitors.
		try {
			preview = await getInvitePreview(token);
		} catch {
			invalidReason = 'not_found';
			loading = false;
			return;
		}
		if (preview.status !== 'valid') {
			// The link may be exhausted/revoked yet the current user is already a
			// member (e.g. just registered with this token, or re-opening the link).
			// Accept is idempotent for existing members, so try it before giving up.
			// Only the cached session is used here — calling authState.init() for a
			// logged-out visitor would trigger the ApiClient 401 → /login redirect
			// and hide the invalid state we want to show.
			if (authState.authenticated) {
				try {
					const res = await acceptInvite(token);
					goto(`/${res.workspace_slug}/inbox`);
					return;
				} catch {
					// Not a member — fall through to the invalid state.
				}
			}
			invalidReason = preview.status;
			loading = false;
			return;
		}
		await authState.init();
		loading = false;
		if (!authState.authenticated) {
			goto(`/login?invite=${encodeURIComponent(token)}`);
		}
	});

	async function handleJoin() {
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
		<Loader2 size={20} class="animate-spin text-[var(--color-text-tertiary)]" />
	{:else if invalidReason}
		<div class="w-full max-w-sm space-y-4 p-8 text-center">
			<Link2Off size={32} class="mx-auto text-[var(--color-text-tertiary)]" />
			<h1 class="text-xl font-semibold text-[var(--color-text-primary)]">{m['invite.invalid.title']()}</h1>
			<p class="text-sm text-[var(--color-text-secondary)]">{(m as unknown as Record<string, () => string>)['invite.invalid.' + invalidReason]()}</p>
			<button
				onclick={() => goto('/login')}
				class="text-sm text-[var(--app-accent)] hover:underline"
			>
				{m['invite.back_to_login']()}
			</button>
		</div>
	{:else if preview}
		<div class="w-full max-w-sm space-y-6 p-8 text-center">
			{#if preview.workspace_logo_url}
				<img src={preview.workspace_logo_url} alt={preview.workspace_name} class="mx-auto h-14 w-14 rounded-lg" />
			{:else}
				<div class="mx-auto flex h-14 w-14 items-center justify-center rounded-lg bg-[var(--app-accent)] text-xl font-semibold text-[var(--app-accent-foreground)]">
					{preview.workspace_name.charAt(0).toUpperCase()}
				</div>
			{/if}
			<div>
				<h1 class="text-xl font-semibold text-[var(--color-text-primary)]">
					{m['invite.heading']({ name: preview.workspace_name })}
				</h1>
				<p class="mt-1 text-sm text-[var(--color-text-secondary)]">
					{m['invite.role_desc']({ role: (m as unknown as Record<string, () => string>)['common.role.' + preview.role]() })}
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
