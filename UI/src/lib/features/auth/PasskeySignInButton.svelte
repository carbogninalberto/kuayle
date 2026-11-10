<script lang="ts">
	// "Sign in with a passkey", shown only where passkeys can work: the instance
	// has them enabled and the browser implements WebAuthn. With `autofill` the
	// browser also offers passkeys in the email field (conditional mediation).
	import { onMount } from 'svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import { KeyRound, Loader2 } from '@lucide/svelte';
	import type { User } from '$lib/types/auth';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/features/i18n/messages';
	import { signInWithPasskey } from './passkey';
	import { apiErrorCode, conditionalMediationAvailable, isPasskeyCancellation, passkeysSupported } from './webauthn';

	let {
		enabled,
		disabled = false,
		autofill = false,
		onSignedIn
	}: {
		enabled: boolean;
		disabled?: boolean;
		autofill?: boolean;
		onSignedIn: (user: User) => void | Promise<void>;
	} = $props();

	let supported = $state(false);
	let busy = $state(false);
	let pending: AbortController | null = null;
	let autofillStarted = false;
	// Server answers to a completed ceremony, as opposed to failing to start one.
	const PICKED_PASSKEY_ERRORS = new Set(['PASSKEY_UNKNOWN', 'PASSKEY_INVALID', 'PASSKEY_CHALLENGE_EXPIRED']);

	onMount(() => {
		supported = passkeysSupported();
		return () => pending?.abort();
	});

	$effect(() => {
		if (enabled && supported && autofill && !autofillStarted) {
			autofillStarted = true;
			void offerInAutofill();
		}
	});

	function report(error: unknown) {
		if (isPasskeyCancellation(error)) return;
		const code = apiErrorCode(error);
		if (code === 'PASSKEY_UNKNOWN') appToast.error(m['login.passkey.unknown']());
		else if (code === 'PASSKEY_CHALLENGE_EXPIRED') appToast.error(m['login.passkey.expired']());
		else appToast.apiError(error, m['login.passkey.failed']());
	}

	async function offerInAutofill() {
		if (!(await conditionalMediationAvailable())) return;
		const controller = new AbortController();
		pending = controller;
		try {
			const user = await signInWithPasskey({ conditional: true, signal: controller.signal });
			if (user) await onSignedIn(user);
		} catch (error) {
			if (pending === controller) pending = null;
			// Autofill runs without being asked for: only a passkey the user
			// actually picked is worth reporting, and autofill is then offered
			// again with a new challenge.
			if (!controller.signal.aborted && PICKED_PASSKEY_ERRORS.has(apiErrorCode(error))) {
				report(error);
				void offerInAutofill();
			}
		} finally {
			if (pending === controller) pending = null;
		}
	}

	async function signIn() {
		if (busy) return;
		// Only one WebAuthn request can be pending: the prompt replaces autofill.
		pending?.abort();
		pending = null;
		busy = true;
		try {
			const user = await signInWithPasskey();
			if (user) await onSignedIn(user);
		} catch (error) {
			report(error);
			if (autofill) void offerInAutofill();
		} finally {
			busy = false;
		}
	}
</script>

{#if enabled && supported}
	<div class="space-y-4">
		<div class="flex items-center gap-3 text-xs text-[var(--color-text-tertiary)]" aria-hidden="true">
			<span class="h-px flex-1 bg-[var(--app-border)]"></span>
			{m['login.passkey.or']()}
			<span class="h-px flex-1 bg-[var(--app-border)]"></span>
		</div>
		<Button variant="outline" type="button" class="w-full" disabled={disabled || busy} onclick={signIn}>
			{#if busy}<Loader2 size={14} class="animate-spin" />{:else}<KeyRound size={14} />{/if}
			{m['login.passkey.button']()}
		</Button>
	</div>
{/if}
