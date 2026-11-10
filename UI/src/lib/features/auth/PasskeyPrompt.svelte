<script lang="ts">
	// After a password sign-in, offers once to add a passkey: only when the
	// instance and the browser support passkeys and the account has none. The
	// session is fresh at that point, so no password is asked again.
	import { page } from '$app/state';
	import { getConfig } from '$lib/api/invite';
	import { listPasskeys } from '$lib/api/passkeys';
	import { demoMode } from '$lib/demo';
	import { m } from '$lib/features/i18n/messages';
	import AddPasskeyDialog from './AddPasskeyDialog.svelte';
	import { authState } from './auth.state.svelte';
	import { dismissPasskeyPrompt, takePasskeyPrompt } from './passkey';
	import { passkeysSupported } from './webauthn';

	let open = $state(false);
	let userId = '';

	$effect(() => {
		const user = authState.user;
		// Wait until sign-in has moved on to the app.
		if (!user || page.url.pathname === '/login') return;
		void offer(user.id);
	});

	async function offer(id: string) {
		if (demoMode || !passkeysSupported() || !takePasskeyPrompt(id)) return;
		try {
			const [config, passkeys] = await Promise.all([getConfig(), listPasskeys()]);
			if (!config.passkeys_enabled || passkeys.length > 0) return;
		} catch {
			return;
		}
		userId = id;
		open = true;
	}
</script>

<AddPasskeyDialog
	bind:open
	title={m['passkeys.prompt_title']()}
	description={m['passkeys.prompt_desc']()}
	cancelLabel={m['passkeys.prompt_later']()}
	onCancel={() => dismissPasskeyPrompt(userId)}
	onAdded={() => dismissPasskeyPrompt(userId)}
/>
