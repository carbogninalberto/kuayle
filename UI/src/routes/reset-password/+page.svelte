<script lang="ts">
	import { Button } from '$lib/components/ui/button/index.js';
	import { Password } from '$lib/components/ui/password';
	import { Loader2 } from '@lucide/svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { resetPassword } from '$lib/api/auth';
	import { validateNewPassword } from '$lib/features/auth/password-rules';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/paraglide/messages.js';

	const token = $derived(page.url.searchParams.get('token') ?? '');
	let password = $state('');
	let confirm = $state('');
	let loading = $state(false);
	let error = $state('');
	let linkInvalid = $state(false);
	const inputClass =
		'mt-1 h-auto w-full rounded-md border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-3 py-2 text-sm text-[var(--color-text-primary)] outline-none focus:border-[var(--app-accent)] focus-visible:border-[var(--app-accent)] focus-visible:ring-0';

	async function handleSubmit(e: Event) {
		e.preventDefault();
		if (loading) return;
		const issue = validateNewPassword(password, confirm);
		if (issue) {
			error = m[`reset.${issue}`]();
			return;
		}
		error = '';
		loading = true;
		try {
			await resetPassword(token, password);
			appToast.success(m['reset.success']());
			await goto('/login');
		} catch (err: any) {
			if (err?.error?.code === 'INVALID_RESET_TOKEN') linkInvalid = true;
			else error = m['reset.error']();
		} finally {
			loading = false;
		}
	}
</script>

<div class="flex min-h-screen items-center justify-center bg-[var(--color-bg)]">
	<div class="w-full max-w-sm space-y-6 p-8">
		<div class="text-center">
			<img src="/favicon.svg" alt={m['login.logo_alt']()} class="mx-auto mb-4 h-14 w-14" />
			<h1 class="text-2xl font-bold text-[var(--color-text-primary)]">{m['reset.title']()}</h1>
			{#if token && !linkInvalid}
				<p class="mt-1 text-sm text-[var(--color-text-secondary)]">{m['reset.subtitle']()}</p>
			{/if}
		</div>

		{#if !token || linkInvalid}
			<p
				role="alert"
				class="rounded-md border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-3 py-2 text-sm text-[var(--color-text-secondary)]"
			>
				{linkInvalid ? m['reset.invalid_link']() : m['reset.missing_token']()}
			</p>
			<p class="text-center text-sm">
				<a href="/forgot-password" class="text-[var(--app-accent)] hover:underline">{m['reset.request_new']()}</a>
			</p>
		{:else}
			<form onsubmit={handleSubmit} class="space-y-4">
				<div>
					<label for="new-password" class="block text-sm text-[var(--color-text-secondary)]">{m['reset.new_password']()}</label>
					<Password id="new-password" bind:value={password} required minlength={12} autocomplete="new-password" class={inputClass} />
				</div>
				<div>
					<label for="confirm-password" class="block text-sm text-[var(--color-text-secondary)]">{m['reset.confirm_password']()}</label>
					<Password id="confirm-password" bind:value={confirm} required minlength={12} autocomplete="new-password" class={inputClass} />
				</div>
				{#if error}
					<p role="alert" class="text-sm text-red-500">{error}</p>
				{/if}
				<Button
					variant="plain"
					type="submit"
					disabled={loading}
					class="inline-flex h-9 w-full items-center justify-center rounded-md bg-[var(--app-accent)] px-4 text-sm font-medium text-[var(--app-accent-foreground)] hover:bg-[var(--app-accent-hover)] disabled:opacity-50"
				>
					{#if loading}<Loader2 size={14} class="animate-spin" />{:else}{m['reset.button']()}{/if}
				</Button>
			</form>
			<p class="text-center text-sm">
				<a href="/login" class="text-[var(--app-accent)] hover:underline">{m['login.back_to_signin']()}</a>
			</p>
		{/if}
	</div>
</div>
