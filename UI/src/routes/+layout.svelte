<script lang="ts">
	import '../app.css';
	import { Toaster } from 'svelte-sonner';
	import { onMount } from 'svelte';
	import ReleaseNotice from '$lib/components/shared/ReleaseNotice.svelte';
	import { preferencesState } from '$lib/features/preferences/preferences.state.svelte';

	let { children } = $props();

	onMount(() => {
		preferencesState.init();
	});
</script>

<Toaster position="bottom-right" theme={preferencesState.resolvedMode} />
<ReleaseNotice />
{@render children()}
