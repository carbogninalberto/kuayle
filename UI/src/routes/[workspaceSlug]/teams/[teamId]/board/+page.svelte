<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';

	const slug = $derived(page.params.workspaceSlug ?? '');
	const teamId = $derived(page.params.teamId ?? '');

	// Board view is now integrated into the team page via ViewSwitcher.
	// Redirect to team page — the user can toggle to board layout there.
	onMount(() => {
		goto(`/${slug}/teams/${teamId}`, { replaceState: true });
	});
</script>

<div class="flex h-full items-center justify-center">
	<p class="text-sm text-[var(--color-text-secondary)]">Redirecting...</p>
</div>
