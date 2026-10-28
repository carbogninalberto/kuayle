<script lang="ts">
	import { page } from '$app/state';
	import VoiceSettings from '$lib/features/voice/VoiceSettings.svelte';
</script>

{#key page.params.workspaceSlug}
	<VoiceSettings slug={page.params.workspaceSlug ?? ''} />
{/key}
