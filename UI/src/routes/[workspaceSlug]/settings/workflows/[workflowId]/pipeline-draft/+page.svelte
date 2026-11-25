<script lang="ts">
	import PageTitle from '$lib/components/shared/PageTitle.svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { ArrowLeft, CircleAlert } from '@lucide/svelte';
	import * as Alert from '$lib/components/ui/alert/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import LoadingState from '$lib/components/shared/LoadingState.svelte';
	import ErrorState from '$lib/components/shared/ErrorState.svelte';
	import {
		agentProfiles,
		isPipeline,
		workflows,
		type AgentProfile,
		type PipelineDraft,
		type WorkflowDetail
	} from '$lib/api/workflows';
	import { appToast } from '$lib/features/toast/toast';
	import PipelineEditor from '$lib/features/workflows/PipelineEditor.svelte';
	import { draftReasonLabel } from '$lib/features/workflows/pipeline-labels';
	import { m } from '$lib/features/i18n/messages';

	// "Draft a pipeline" from a graph workflow (KUA-283, KUA-284): the server
	// drafts it, and the draft opens in the pipeline editor, unsaved. Nothing
	// is stored until the person saves it, as a new workflow; the graph keeps
	// running as it is. When the graph cannot be one path of phases, the
	// server's reasons are listed on the steps they are about.
	const slug = $derived(page.params.workspaceSlug ?? '');
	const workflowId = $derived(page.params.workflowId ?? '');
	const listHref = $derived(`/${slug}/settings/workflows`);

	let source = $state<WorkflowDetail | null>(null),
		draft = $state<PipelineDraft | null>(null),
		profiles = $state<AgentProfile[]>([]),
		canManage = $state(false),
		loading = $state(true),
		failed = $state(false);

	async function load(id: string) {
		loading = true;
		failed = false;
		try {
			const [loaded, profileList, drafted] = await Promise.all([
				workflows.get(slug, id),
				agentProfiles.list(slug),
				workflows.draftPipeline(slug, id)
			]);
			source = loaded;
			profiles = profileList.profiles;
			canManage = profileList.can_manage;
			draft = drafted;
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

	const sourceName = $derived(source ? (source.builtin ? m['workflows.standard']() : source.name) : '');
	// The step a reason is about, by its name in the graph.
	const stepName = (id?: string) =>
		(id && source && !isPipeline(source.graph) && source.graph.nodes.find((node) => node.id === id)?.label) || id || '';
</script>

{#if loading && !draft}<div class="p-6"><LoadingState /></div>
{:else if failed || !source || !draft}<div class="p-6">
		<ErrorState message={m['workflows.failed_load']()} onretry={() => load(workflowId)} />
	</div>
{:else if draft.pipeline}
	<div class="h-full min-h-0">
		{#key draft}
			<PipelineEditor
				{slug}
				workflow={{
					name: m['workflows.pipeline_editor.draft_name']({ name: sourceName }),
					description: source.description,
					latest_version: 0,
					graph: draft.pipeline
				}}
				{profiles}
				editable={canManage}
				backHref={listHref}
				oncreated={(created) => goto(`${listHref}/${encodeURIComponent(created.id)}`, { replaceState: true })}
			/>
		{/key}
	</div>
{:else}
	<div class="mx-auto max-w-3xl space-y-4 p-6" data-testid="pipeline-draft-reasons">
		<Button variant="link" href={listHref} class="h-auto px-0 text-muted-foreground"
			><ArrowLeft />{m['workflows.title']()}</Button
		>
		<h1 class="text-lg font-semibold">{m['workflows.pipeline_editor.draft_failed']({ name: sourceName })}</h1>
		<Alert.Root>
			<CircleAlert />
			<Alert.Description>{m['workflows.pipeline_editor.draft_failed_desc']()}</Alert.Description>
		</Alert.Root>
		<ul class="divide-y rounded-lg border text-sm">
			{#each draft.reasons as reason, i (i)}
				<li class="space-y-0.5 px-3 py-2" data-testid="pipeline-draft-reason">
					{#if reason.node_id}<p class="font-medium">{stepName(reason.node_id)}</p>{/if}
					<p class="text-muted-foreground">{draftReasonLabel(reason)}</p>
				</li>
			{/each}
		</ul>
		<Button variant="outline" href="{listHref}/{encodeURIComponent(workflowId)}"
			>{m['workflows.open_named']({ name: sourceName })}</Button
		>
	</div>
{/if}

<PageTitle parts={[sourceName, m['workflows.title'](), m['settings.title']()]} />
