<script lang="ts">
	import { page } from '$app/state';
	import { authState } from '$lib/features/auth/auth.state.svelte';
	import AssistantChat from '$lib/features/assistant/AssistantChat.svelte';
</script>

{#key `${page.params.workspaceSlug}:${page.url.searchParams.get('conversation')}:${authState.user?.id}`}
	<AssistantChat slug={page.params.workspaceSlug!} conversationId={page.url.searchParams.get('conversation')} />
{/key}
