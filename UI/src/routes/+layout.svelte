<script lang="ts">
	import '../app.css';
	import { Toaster } from 'svelte-sonner';
	import { onMount } from 'svelte';
	import { setCurrentWorkspace } from '$lib/features/workspaces/current-workspace.svelte';
	import ReleaseNotice from '$lib/components/shared/ReleaseNotice.svelte';
	import { preferencesState } from '$lib/features/preferences/preferences.state.svelte';
	import { registerServiceWorker } from '$lib/features/pwa/register-service-worker';
	import { lockInstalledAppZoom } from '$lib/features/pwa/installed-viewport';
	import { m, getLocale, setLocale } from '$lib/features/i18n/messages';

	let { children } = $props();
	setCurrentWorkspace();

	onMount(() => {
		preferencesState.init();
		registerServiceWorker();
		return lockInstalledAppZoom();
	});
</script>

<Toaster position="bottom-right" theme={preferencesState.resolvedMode} />
<ReleaseNotice />
{@render children()}
