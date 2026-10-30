<script lang="ts">
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input';
	import { Loader2 } from '@lucide/svelte';
	import { forgotPassword } from '$lib/api/auth';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/paraglide/messages.js';

	let email = $state('');
	let loading = $state(false);
	let sent = $state(false);
	const inputClass =
		'mt-1 h-auto w-full rounded-md border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-3 py-2 text-sm text-[var(--color-text-primary)] outline-none focus:border-[var(--app-accent)] focus-visible:border-[var(--app-accent)] focus-visible:ring-0';

	async function handleSubmit(e: Event) {
		e.preventDefault();
		if (loading) return;
		loading = true;
		try {
			await forgotPassword(email.trim());
			sent = true;
		} catch (err: any) {
			appToast.apiError(err, m['forgot.error']());
		} finally {
			loading = false;
		}
	}
</script>

<div class="flex min-h-screen items-center justify-center bg-[var(--color-bg)]">
	<div class="w-full max-w-sm space-y-6 p-8">
		<div class="text-center">
			<img src="/favicon.svg" alt={m['login.logo_alt']()} class="mx-auto mb-4 h-14 w-14" />
			<h1 class="text-2xl font-bold text-[var(--color-text-primary)]">{m['forgot.title']()}</h1>
			<p class="mt-1 text-sm text-[var(--color-text-secondary)]">{m['forgot.subtitle']()}</p>
		</div>

		{#if sent}
			<p
				role="status"
				class="rounded-md border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-3 py-2 text-sm text-[var(--color-text-secondary)]"
			>
				{m['forgot.sent']()}
			</p>
		{:else}
			<form onsubmit={handleSubmit} class="space-y-4">
				<div>
					<label for="email" class="block text-sm text-[var(--color-text-secondary)]">{m['login.email']()}</label>
					<Input id="email" type="email" bind:value={email} required autocomplete="email" class={inputClass} />
				</div>
				<Button
					variant="plain"
					type="submit"
					disabled={loading}
					class="inline-flex h-9 w-full items-center justify-center rounded-md bg-[var(--app-accent)] px-4 text-sm font-medium text-[var(--app-accent-foreground)] hover:bg-[var(--app-accent-hover)] disabled:opacity-50"
				>
					{#if loading}<Loader2 size={14} class="animate-spin" />{:else}{m['forgot.button']()}{/if}
				</Button>
			</form>
		{/if}

		<p class="text-center text-sm">
			<a href="/login" class="text-[var(--app-accent)] hover:underline">{m['login.back_to_signin']()}</a>
		</p>
	</div>
</div>
