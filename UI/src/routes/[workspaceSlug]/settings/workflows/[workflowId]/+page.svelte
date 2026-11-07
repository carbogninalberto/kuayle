<script lang="ts">
	import { page } from '$app/state';
	import { ArrowLeft } from '@lucide/svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import LoadingState from '$lib/components/shared/LoadingState.svelte';
	import ErrorState from '$lib/components/shared/ErrorState.svelte';
	import { agentProfiles, workflows, type AgentProfile, type WorkflowDetail } from '$lib/api/workflows';
	import { appToast } from '$lib/features/toast/toast';
	import WorkflowEditor from '$lib/features/workflows/WorkflowEditor.svelte';
	import { m } from '$lib/paraglide/messages.js';

	// One workflow in the editor. The built-in Standard workflow, archived
	// workflows and every workflow for members open read-only; the server
	// enforces who may save.
	const slug = $derived(page.params.workspaceSlug ?? '');
	const workflowId = $derived(page.params.workflowId ?? '');

	let detail = $state<WorkflowDetail | null>(null),
		profiles = $state<AgentProfile[]>([]),
		canManage = $state(false),
		loading = $state(true),
		failed = $state(false);

	async function load(id: string) {
		loading = true;
		failed = false;
		try {
			const [loaded, profileList] = await Promise.all([workflows.get(slug, id), agentProfiles.list(slug)]);
			detail = loaded;
			profiles = profileList.profiles;
			canManage = profileList.can_manage;
		} catch (error) {
			failed = true;
			appToast.apiError(error, m['workflows.failed_load']());
		} finally {
			loading = false;
		}
	}

	$effect(() => {
		void load(workflowId);
	});

	const editable = $derived(!!detail && canManage && !detail.builtin && !detail.archived);
</script>

<div class="mx-auto max-w-7xl space-y-4 p-6">
	<Button variant="ghost" size="sm" href="/{slug}/settings/workflows" class="-ml-2"
		><ArrowLeft />{m['workflows.back']()}</Button
	>
	{#if loading && !detail}<LoadingState />
	{:else if failed || !detail}<ErrorState message={m['workflows.failed_load']()} onretry={() => load(workflowId)} />
	{:else}
		{#key detail.id}
			<WorkflowEditor
				{slug}
				workflow={detail.builtin ? { ...detail, name: m['workflows.standard']() } : detail}
				{profiles}
				{editable}
				onsaved={(saved) => {
					if (detail) detail = { ...detail, ...saved, versions: detail.versions };
				}}
			/>
		{/key}
	{/if}
</div>
