<script lang="ts">
	import '../app.css';
	import { Toaster } from 'svelte-sonner';
	import { onMount } from 'svelte';
	import { setCurrentWorkspace } from '$lib/features/workspaces/current-workspace.svelte';
	import ReleaseNotice from '$lib/components/shared/ReleaseNotice.svelte';
	import { preferencesState } from '$lib/features/preferences/preferences.state.svelte';
	import { registerServiceWorker } from '$lib/features/pwa/register-service-worker';
	import { m } from '$lib/paraglide/messages.js';
	import { getLocale, setLocale } from '$lib/paraglide/runtime.js';

	let { children } = $props();
	setCurrentWorkspace();

	onMount(() => {
		preferencesState.init();
		registerServiceWorker();
	});
</script>

<Toaster position="bottom-right" theme={preferencesState.resolvedMode} />
<ReleaseNotice />
{@render children()}
