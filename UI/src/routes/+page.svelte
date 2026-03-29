<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { authState } from '$lib/features/auth/auth.state.svelte';
	import { listWorkspaces } from '$lib/api/workspaces';

	onMount(async () => {
		await authState.init();
		if (!authState.authenticated) {
			goto('/login');
			return;
		}
		try {
			const workspaces = await listWorkspaces();
			if (workspaces.length > 0) {
				goto(`/${workspaces[0].slug}/inbox`);
			}
		} catch {
			goto('/login');
		}
	});
</script>

<div class="flex h-screen items-center justify-center">
</div>
