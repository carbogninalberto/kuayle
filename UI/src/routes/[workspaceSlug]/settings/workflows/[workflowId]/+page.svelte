<script lang="ts">
	import PageTitle from '$lib/components/shared/PageTitle.svelte';
	import { page } from '$app/state';
	import LoadingState from '$lib/components/shared/LoadingState.svelte';
	import ErrorState from '$lib/components/shared/ErrorState.svelte';
	import { agentProfiles, isPipeline, workflows, type AgentProfile, type WorkflowDetail } from '$lib/api/workflows';
	import { appToast } from '$lib/features/toast/toast';
	import PipelineEditor from '$lib/features/workflows/PipelineEditor.svelte';
	import PipelineView from '$lib/features/workflows/PipelineView.svelte';
	import WorkflowEditor from '$lib/features/workflows/WorkflowEditor.svelte';
	import { m } from '$lib/features/i18n/messages';

	// One workflow in the editor. Owners and admins edit any workflow but an
	// archived one, Standard included (their edits replace the built-in flow);
	// members read them. The server enforces who may save.
	const slug = $derived(page.params.workspaceSlug ?? '');
	const workflowId = $derived(page.params.workflowId ?? '');

	let detail = $state<WorkflowDetail | null>(null),
		profiles = $state<AgentProfile[]>([]),
		canManage = $state(false),
		pipelinesEnabled = $state(false),
		loading = $state(true),
		failed = $state(false);

	async function load(id: string) {
		loading = true;
		failed = false;
		try {
			const [loaded, profileList] = await Promise.all([workflows.get(slug, id), agentProfiles.list(slug)]);
			// Only the workflow list says whether the server has pipelines on;
			// without it, a pipeline is shown read only.
			pipelinesEnabled =
				isPipeline(loaded.graph) && !!(await workflows.list(slug).catch(() => null))?.pipelines_enabled;
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
	// An experimental pipeline is edited only while the server has pipelines
	// on; otherwise it is shown read only.
	const pipeline = $derived(detail && isPipeline(detail.graph) ? detail.graph : null);
	const graphDetail = $derived(detail && !isPipeline(detail.graph) ? { ...detail, graph: detail.graph } : null);
</script>

{#if loading && !detail}<div class="p-6"><LoadingState /></div>
{:else if failed || !detail}<div class="p-6">
		<ErrorState message={m['workflows.failed_load']()} onretry={() => load(workflowId)} />
	</div>
{:else if pipeline && pipelinesEnabled}
	<div class="h-full min-h-0">
		{#key detail.id}
			<PipelineEditor
				{slug}
				workflow={{ ...detail, graph: pipeline }}
				{profiles}
				{editable}
				backHref="/{slug}/settings/workflows"
				onsaved={(saved) => {
					if (detail) detail = { ...detail, ...saved, versions: detail.versions };
				}}
			/>
		{/key}
	</div>
{:else if pipeline}
	<div class="h-full min-h-0">
		<PipelineView name={detail.name} {pipeline} {profiles} backHref="/{slug}/settings/workflows" />
	</div>
{:else if graphDetail}
	<div class="h-full min-h-0">
		{#key graphDetail.id}
			<WorkflowEditor
				{slug}
				workflow={graphDetail.builtin ? { ...graphDetail, name: m['workflows.standard']() } : graphDetail}
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

<PageTitle parts={[detail?.name, m['workflows.title'](), m['settings.title']()]} />
