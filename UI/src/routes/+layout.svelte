<script lang="ts">
	import '../app.css';
	import { Toaster } from 'svelte-sonner';
	import { onMount } from 'svelte';
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
	setCurrentWorkspace();

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
{@render children()}
