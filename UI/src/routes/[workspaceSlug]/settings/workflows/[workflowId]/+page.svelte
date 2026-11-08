<script lang="ts">
	import { page } from '$app/state';
	import LoadingState from '$lib/components/shared/LoadingState.svelte';
	import ErrorState from '$lib/components/shared/ErrorState.svelte';
	import { agentProfiles, workflows, type AgentProfile, type WorkflowDetail } from '$lib/api/workflows';
	import { appToast } from '$lib/features/toast/toast';
	import WorkflowEditor from '$lib/features/workflows/WorkflowEditor.svelte';
	import { m } from '$lib/paraglide/messages.js';

	// One workflow in the editor. Owners and admins edit any workflow but an
	// archived one, Standard included (their edits replace the built-in flow);
	// members read them. The server enforces who may save.
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

	const editable = $derived(!!detail && canManage && !detail.archived);
</script>

{#if loading && !detail}<div class="p-6"><LoadingState /></div>
{:else if failed || !detail}<div class="p-6">
		<ErrorState message={m['workflows.failed_load']()} onretry={() => load(workflowId)} />
	</div>
{:else}
	<div class="h-full min-h-[36rem]">
		{#key detail.id}
			<WorkflowEditor
				{slug}
				workflow={detail.builtin ? { ...detail, name: m['workflows.standard']() } : detail}
				{profiles}
				{editable}
				backHref="/{slug}/settings/workflows"
				onsaved={(saved) => {
					if (detail) detail = { ...detail, ...saved, versions: detail.versions };
				}}
			/>
		{/key}
	</div>
{/if}
