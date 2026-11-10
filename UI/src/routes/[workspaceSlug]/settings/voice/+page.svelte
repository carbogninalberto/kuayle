<script lang="ts">
	import PageTitle from '$lib/components/shared/PageTitle.svelte';
	import { m } from '$lib/features/i18n/messages';
	import { page } from '$app/state';
	import VoiceSettings from '$lib/features/voice/VoiceSettings.svelte';
</script>

{#key page.params.workspaceSlug}
	<VoiceSettings slug={page.params.workspaceSlug ?? ''} />
{/key}

<PageTitle parts={[m['settings.nav.voice'](), m['settings.title']()]} />
