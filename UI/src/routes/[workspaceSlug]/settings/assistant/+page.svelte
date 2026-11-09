<script lang="ts">
	import PageTitle from '$lib/components/shared/PageTitle.svelte';
	import { m } from '$lib/paraglide/messages.js';
	import { page } from '$app/state';
	import AssistantSettings from '$lib/features/assistant/AssistantSettings.svelte';
</script>

{#key page.params.workspaceSlug}
	<AssistantSettings slug={page.params.workspaceSlug ?? ''} />
{/key}

<PageTitle parts={[m['settings.nav.assistant'](), m['settings.title']()]} />
