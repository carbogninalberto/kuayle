<script lang="ts">
	import { page } from '$app/state';
	import TelegramSettings from '$lib/features/telegram/TelegramSettings.svelte';
</script>

{#key page.params.workspaceSlug}
	<TelegramSettings slug={page.params.workspaceSlug ?? ''} />
{/key}
