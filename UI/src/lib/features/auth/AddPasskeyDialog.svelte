<script lang="ts">
	// Names and creates a passkey for the signed-in account. The password field
	// appears only when the server says the session is too old to add one.
	import * as Dialog from '$lib/components/ui/dialog';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Password } from '$lib/components/ui/password';
	import { Loader2 } from '@lucide/svelte';
	import type { Passkey } from '$lib/api/passkeys';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/features/i18n/messages';
	import { registerPasskey } from './passkey';
	import { apiErrorCode, isPasskeyCancellation, isPasskeyDuplicate, suggestPasskeyName } from './webauthn';

	let {
		open = $bindable(false),
		title = m['passkeys.add'](),
		description = m['passkeys.desc'](),
		cancelLabel = m['common.cancel'](),
		onAdded,
		onCancel
	}: {
		open?: boolean;
		title?: string;
		description?: string;
		cancelLabel?: string;
		onAdded?: (passkey: Passkey) => void;
		onCancel?: () => void;
	} = $props();

	let name = $state('');
	let password = $state('');
	let needsPassword = $state(false);
	let busy = $state(false);
	let wasOpen = false;

	$effect(() => {
		if (open && !wasOpen) {
			const suggestion = suggestPasskeyName(navigator.userAgent);
			name = suggestion ? m['passkeys.name_on'](suggestion) : '';
			password = '';
			needsPassword = false;
		}
		wasOpen = open;
	});

	function cancel() {
		if (busy) return;
		open = false;
		onCancel?.();
	}

	async function submit(event: Event) {
		event.preventDefault();
		if (busy || (needsPassword && !password)) return;
		busy = true;
		try {
			const passkey = await registerPasskey(name.trim(), needsPassword ? password : undefined);
			if (!passkey) return;
			open = false;
			appToast.success(m['passkeys.added']());
			onAdded?.(passkey);
		} catch (error) {
			const code = apiErrorCode(error);
			if (code === 'REAUTH_REQUIRED') needsPassword = true;
			else if (isPasskeyDuplicate(error) || code === 'PASSKEY_ALREADY_REGISTERED')
				appToast.error(m['passkeys.already_registered']());
			else if (!isPasskeyCancellation(error)) appToast.apiError(error, m['passkeys.failed_add']());
		} finally {
			busy = false;
		}
	}
</script>

<Dialog.Root
	{open}
	onOpenChange={(next) => {
		if (!next) cancel();
	}}
>
	<Dialog.Content
		class="overflow-hidden rounded-xl border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-0 sm:max-w-[440px]"
		showCloseButton={!busy}
		interactOutsideBehavior={busy ? 'ignore' : 'close'}
		escapeKeydownBehavior={busy ? 'ignore' : 'close'}
	>
		<form onsubmit={submit}>
			<div class="space-y-4 px-5 pt-5 pb-4">
				<div>
					<Dialog.Title class="text-base font-semibold text-[var(--color-text-primary)]">{title}</Dialog.Title>
					<Dialog.Description class="mt-1 text-xs text-[var(--color-text-tertiary)]">{description}</Dialog.Description>
				</div>
				<div class="space-y-1.5">
					<Label for="passkey-name" class="text-xs text-[var(--color-text-secondary)]">{m['passkeys.name_label']()}</Label>
					<Input
						id="passkey-name"
						bind:value={name}
						maxlength={64}
						placeholder={m['passkeys.name_placeholder']()}
						disabled={busy}
					/>
				</div>
				{#if needsPassword}
					<div class="space-y-1.5">
						<Label for="passkey-password" class="text-xs text-[var(--color-text-secondary)]"
							>{m['passkeys.password_label']()}</Label
						>
						<Password id="passkey-password" bind:value={password} required autocomplete="current-password" disabled={busy} />
						<p class="text-xs text-[var(--color-text-tertiary)]" role="status">{m['passkeys.password_hint']()}</p>
					</div>
				{/if}
			</div>
			<div class="flex justify-end gap-2 border-t border-[var(--app-border)] px-5 py-3">
				<Button variant="outline" size="sm" type="button" onclick={cancel} disabled={busy}>{cancelLabel}</Button>
				<Button size="sm" type="submit" disabled={busy || (needsPassword && !password)}>
					{#if busy}<Loader2 size={14} class="animate-spin" />{m['passkeys.adding']()}{:else}{m['passkeys.add']()}{/if}
				</Button>
			</div>
		</form>
	</Dialog.Content>
</Dialog.Root>
