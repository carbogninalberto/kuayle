<script lang="ts">
	import '../app.css';
	import { Toaster } from 'svelte-sonner';
	import { onMount } from 'svelte';
	import { replaceState } from '$app/navigation';
	import { page } from '$app/state';
	import { exchangeSignInToken } from '$lib/api/auth';
	import { consumeSignInLink } from '$lib/features/auth/sign-in-link';
	import { setCurrentWorkspace } from '$lib/features/workspaces/current-workspace.svelte';
	import ReleaseNotice from '$lib/components/shared/ReleaseNotice.svelte';
	import PasskeyPrompt from '$lib/features/auth/PasskeyPrompt.svelte';
	import { preferencesState } from '$lib/features/preferences/preferences.state.svelte';
	import { registerServiceWorker } from '$lib/features/pwa/register-service-worker';
	import { listenForNotificationNavigation } from '$lib/features/pwa/notification-navigation';
	import { lockInstalledAppZoom } from '$lib/features/pwa/installed-viewport';
	import { m, getLocale, setLocale } from '$lib/features/i18n/messages';
	import { remoteImages } from '$lib/components/shared/remote-images.action';

	let { children } = $props();
	let exchangingSignIn = $state(page.url.searchParams.has('sign_in'));
	setCurrentWorkspace();

	onMount(async () => {
		if (!exchangingSignIn) return;
		try {
			await consumeSignInLink(new URL(page.url.href), page.params.workspaceSlug, (clean) => replaceState(clean, page.state), exchangeSignInToken);
		} catch {
			// The destination can still open with the existing session or manual sign-in.
		} finally {
			exchangingSignIn = false;
		}
	});

	onMount(() => {
		preferencesState.init();
		registerServiceWorker();
		// A clicked notification routes inside the open app instead of reloading it.
		listenForNotificationNavigation();
		return lockInstalledAppZoom();
	});
</script>

<!-- Remote images in user content wait for the reader, on every page. -->
<svelte:body use:remoteImages />

<Toaster position="bottom-right" theme={preferencesState.resolvedMode} />
<ReleaseNotice />
<PasskeyPrompt />
{#if exchangingSignIn}
	<p role="status" class="grid min-h-dvh place-items-center text-sm text-muted-foreground">{m['common.loading']()}</p>
{:else}
	{@render children()}
{/if}
