<script lang="ts">
	import PageTitle from '$lib/components/shared/PageTitle.svelte';
	import { m } from '$lib/features/i18n/messages';
	import { page } from '$app/state';
	import TelegramSettings from '$lib/features/telegram/TelegramSettings.svelte';
</script>

{#key page.params.workspaceSlug}
	<TelegramSettings slug={page.params.workspaceSlug ?? ''} />
{/key}

<PageTitle parts={['Telegram', m['settings.title']()]} />
