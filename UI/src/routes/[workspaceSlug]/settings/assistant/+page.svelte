<script lang="ts">
	import { page } from '$app/state';
	import AssistantSettings from '$lib/features/assistant/AssistantSettings.svelte';
</script>

{#key page.params.workspaceSlug}
	<AssistantSettings slug={page.params.workspaceSlug ?? ''} />
{/key}
