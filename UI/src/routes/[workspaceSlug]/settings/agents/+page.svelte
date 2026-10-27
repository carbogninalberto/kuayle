<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import LoadingState from '$lib/components/shared/LoadingState.svelte';
	import ErrorState from '$lib/components/shared/ErrorState.svelte';
	import { getDevMachinePolicy, updateDevMachinePolicy } from '$lib/api/dev-machines';
	import { getWorkspace } from '$lib/api/workspaces';
	import type { DevMachinePolicy } from '$lib/types/dev-machine';
	import { appToast } from '$lib/features/toast/toast';
	import CodingAgentsCard from '$lib/features/agents/CodingAgentsCard.svelte';
	import ModelServersCard from '$lib/features/agents/ModelServersCard.svelte';
	import { m } from '$lib/paraglide/messages.js';

	const slug = $derived(page.params.workspaceSlug ?? '');
	let policy = $state<DevMachinePolicy | null>(null),
		canAdmin = $state(false),
		loading = $state(true),
		failed = $state(false),
		saving = $state(false);

	async function load() {
		loading = true;
		failed = false;
		try {
			const [workspace, loaded] = await Promise.all([getWorkspace(slug), getDevMachinePolicy(slug)]);
			canAdmin = workspace.current_user_role === 'owner' || workspace.current_user_role === 'admin';
			policy = loaded;
		} catch (error) {
			failed = true;
			appToast.apiError(error, m['settings.agents.failed_load']());
		} finally {
			loading = false;
		}
	}
	onMount(load);

	// The policy endpoint replaces the whole policy: send it back with only the
	// agent choices changed.
	async function saveAgents(allowed: string[], allowCustom: boolean) {
		if (!policy || !canAdmin || saving) return;
		saving = true;
		const { workspace_id: _, disk_quota_mode: __, size_options: ___, ...current } = policy;
		try {
			policy = await updateDevMachinePolicy(slug, {
				...current,
				agent_git_name: (current.agent_git_name ?? '').trim(),
				agent_git_email: (current.agent_git_email ?? '').trim(),
				allowed_providers: allowed,
				allow_custom_providers: allowCustom
			});
			appToast.success(m['settings.agents.agents_saved']());
		} catch (error) {
			appToast.apiError(error, m['settings.dev_machines.failed_save']());
		} finally {
			saving = false;
		}
	}
</script>

{#if loading}<LoadingState />
{:else if failed || !policy}<ErrorState message={m['settings.agents.failed_load']()} onretry={load} />
{:else}
	<div class="mx-auto max-w-3xl space-y-6 p-6">
		<div>
			<h1 class="text-lg font-semibold">{m['settings.agents.title']()}</h1>
			<p class="mt-1 text-sm text-muted-foreground">{m['settings.agents.desc']()}</p>
		</div>
		{#if !canAdmin}<p class="rounded-lg border bg-muted/40 p-3 text-xs text-muted-foreground">
				{m['settings.dev_machines.admin_only']()}
			</p>{/if}
		<ModelServersCard {slug} {canAdmin} />
		<CodingAgentsCard {policy} {canAdmin} {saving} onchange={saveAgents} />
	</div>
{/if}
