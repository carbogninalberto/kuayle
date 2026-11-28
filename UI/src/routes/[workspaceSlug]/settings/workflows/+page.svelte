<script lang="ts">
	import { SettingsPageHeader } from '$lib/components/settings';
	import PageTitle from '$lib/components/shared/PageTitle.svelte';
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { Copy, Info, Pencil, Plus, Star, Trash2, Workflow } from '@lucide/svelte';
	import * as Popover from '$lib/components/ui/popover/index.js';
	import * as AlertDialog from '$lib/components/ui/alert-dialog/index.js';
	import * as Item from '$lib/components/ui/item/index.js';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import LoadingState from '$lib/components/shared/LoadingState.svelte';
	import ErrorState from '$lib/components/shared/ErrorState.svelte';
	import { isPipeline, workflows, type WorkflowList, type WorkflowSummary } from '$lib/api/workflows';
	import { getWorkspace } from '$lib/api/workspaces';
	import { listTeams } from '$lib/api/teams';
	import { listProjects } from '$lib/api/projects';
	import { appToast } from '$lib/features/toast/toast';
	import NewWorkflowDialog from '$lib/features/workflows/NewWorkflowDialog.svelte';
	import WorkflowDefaultsCard from '$lib/features/workflows/WorkflowDefaultsCard.svelte';
	import { m } from '$lib/features/i18n/messages';

	// Settings → Workflows: the built-in Standard flow (read only: every task
	// without a workflow runs it) and the workspace's pipelines, with the
	// defaults new tasks use. Owners and admins manage the pipelines (the
	// server enforces it); members read them and choose one per task. With the
	// server's pipelines off, no pipeline is created.
	const slug = $derived(page.params.workspaceSlug ?? '');

	let list = $state<WorkflowList | null>(null),
		workspaceId = $state(''),
		teams = $state<{ id: string; name: string }[]>([]),
		projects = $state<{ id: string; name: string }[]>([]),
		loading = $state(true),
		failed = $state(false),
		creating = $state(false),
		busy = $state<string | null>(null),
		deleting = $state<WorkflowSummary | null>(null);

	async function load() {
		loading = true;
		failed = false;
		try {
			const [loaded, workspace, loadedTeams, loadedProjects] = await Promise.all([
				workflows.list(slug),
				getWorkspace(slug),
				listTeams(slug).catch(() => []),
				listProjects(slug).catch(() => [])
			]);
			list = loaded;
			workspaceId = workspace.id;
			teams = loadedTeams.map((t) => ({ id: t.id, name: t.name }));
			projects = loadedProjects.map((p) => ({ id: p.id, name: p.name }));
		} catch (error) {
			failed = true;
			appToast.apiError(error, m['workflows.failed_load']());
		} finally {
			loading = false;
		}
	}
	onMount(load);

	const defaultId = $derived.by(() => {
		const row = list?.defaults.find((d) => d.scope === 'workspace');
		return row?.workflow_id ?? 'standard';
	});
	const editorHref = (id: string) => `/${slug}/settings/workflows/${encodeURIComponent(id)}`;

	async function setDefault(workflow: WorkflowSummary) {
		if (!list?.can_manage || busy) return;
		busy = workflow.id;
		try {
			list = await workflows.setDefault(slug, 'workspace', workspaceId, workflow.id);
			appToast.success(m['workflows.default_set']());
		} catch (error) {
			appToast.apiError(error, m['workflows.failed_save']());
		} finally {
			busy = null;
		}
	}

	async function duplicate(workflow: WorkflowSummary) {
		if (!list?.can_manage || busy) return;
		busy = workflow.id;
		try {
			const created = await workflows.duplicate(slug, workflow.id);
			appToast.success(m['workflows.duplicated']());
			await goto(editorHref(created.id));
		} catch (error) {
			appToast.apiError(error, m['workflows.failed_save']());
		} finally {
			busy = null;
		}
	}

	async function remove() {
		const workflow = deleting;
		if (!workflow || busy) return;
		busy = workflow.id;
		try {
			await workflows.remove(slug, workflow.id);
			appToast.success(m['workflows.deleted_done']());
			deleting = null;
			await load();
		} catch (error) {
			appToast.apiError(error, m['workflows.failed_save']());
		} finally {
			busy = null;
		}
	}
</script>

{#if loading}<LoadingState />
{:else if failed || !list}<ErrorState message={m['workflows.failed_load']()} onretry={load} />
{:else}
	{@const canManage = list.can_manage}
	<div class="settings-page mx-auto max-w-3xl space-y-6 p-4 sm:p-6">
		<SettingsPageHeader title={m['workflows.title']()}>
			{#snippet action()}
				<Popover.Root>
						<Popover.Trigger>
							{#snippet child({ props })}<Button
									{...props}
									size="icon-sm"
									variant="ghost"
									class="text-muted-foreground"
									aria-label={m['workflows.about']()}><Info /></Button
								>{/snippet}
						</Popover.Trigger>
						<Popover.Content align="start" class="w-80 text-sm text-muted-foreground"
							>{m['workflows.desc']()}</Popover.Content
						>
					</Popover.Root>
				{#if canManage && list?.pipelines_enabled}<Button size="sm" onclick={() => (creating = true)} data-testid="workflow-new"
						><Plus />{m['workflows.new']()}</Button
					>{/if}
			{/snippet}
		</SettingsPageHeader>
		{#if !canManage}<p class="rounded-lg border bg-muted/40 p-3 text-xs text-muted-foreground">
				{m['workflows.member_note']()}
			</p>{/if}

		<section aria-labelledby="workflow-list-title" class="space-y-2">
			<h2 id="workflow-list-title" class="sr-only">{m['workflows.title']()}</h2>
			{#each list.workflows as workflow (workflow.id)}
				{@const name = workflow.builtin ? m['workflows.standard']() : workflow.name}
				<Item.Root variant="outline" size="sm" data-testid="workflow-row">
					<Item.Media variant="icon"><Workflow /></Item.Media>
					<Item.Content class="min-w-0">
						<Item.Title class="flex flex-wrap items-center gap-2">
							<a
								href={editorHref(workflow.id)}
								class="underline-offset-4 hover:underline"
								aria-label={m['workflows.open_named']({ name })}>{name}</a
							>
							{#if workflow.builtin}<Badge variant="secondary">{m['workflows.builtin']()}</Badge>
							{:else}<Badge variant="outline">{m['workflows.version']({ version: workflow.latest_version })}</Badge
								>{/if}
							{#if isPipeline(workflow.graph)}<Badge variant="secondary">{m['workflows.pipeline']()}</Badge>{/if}
							{#if defaultId === workflow.id}<Badge>{m['workflows.default_badge']()}</Badge>{/if}
							{#if workflow.archived}<Badge variant="outline">{m['workflows.archived']()}</Badge>{/if}
						</Item.Title>
						{#if workflow.description}<Item.Description class="line-clamp-2">{workflow.description}</Item.Description
							>{/if}
					</Item.Content>
					{#if canManage}
						<Item.Actions>
							{#if !workflow.builtin && !workflow.archived}<Button
									size="icon-sm"
									variant="ghost"
									href={editorHref(workflow.id)}
									aria-label={m['workflows.edit_named']({ name })}
									title={m['workflows.edit']()}><Pencil /></Button
								>{/if}
							{#if !workflow.archived && defaultId !== workflow.id}<Button
									size="icon-sm"
									variant="ghost"
									disabled={busy !== null}
									aria-label={m['workflows.set_default_named']({ name })}
									title={m['workflows.set_default']()}
									onclick={() => setDefault(workflow)}><Star /></Button
								>{/if}
							{#if !workflow.builtin}
								{#if list.pipelines_enabled}<Button
										size="icon-sm"
										variant="ghost"
										disabled={busy !== null}
										aria-label={m['workflows.duplicate_named']({ name })}
										title={m['workflows.duplicate']()}
										onclick={() => duplicate(workflow)}><Copy /></Button
									>{/if}
								<Button
									size="icon-sm"
									variant="ghost"
									class="text-destructive hover:text-destructive"
									disabled={busy !== null}
									aria-label={m['workflows.delete_named']({ name })}
									title={m['workflows.delete']()}
									onclick={() => (deleting = workflow)}><Trash2 /></Button
								>
							{/if}
						</Item.Actions>
					{/if}
				</Item.Root>
			{/each}
		</section>

		<WorkflowDefaultsCard {slug} {list} {workspaceId} {teams} {projects} onchange={(next) => (list = next)} />
	</div>
{/if}

{#if creating}
	<NewWorkflowDialog
		{slug}
		onclose={() => (creating = false)}
		oncreated={(created) => {
			creating = false;
			void goto(editorHref(created.id));
		}}
	/>
{/if}

<AlertDialog.Root open={!!deleting} onOpenChange={(open) => !open && (deleting = null)}>
	<AlertDialog.Content class="max-h-[calc(100dvh-2rem)] overflow-y-auto wrap-anywhere">
		<AlertDialog.Header>
			<AlertDialog.Title>{m['workflows.delete_title']()}</AlertDialog.Title>
			<AlertDialog.Description>{m['workflows.delete_confirm']({ name: deleting?.name ?? '' })}</AlertDialog.Description>
		</AlertDialog.Header>
		<AlertDialog.Footer>
			<AlertDialog.Cancel>{m['common.cancel']()}</AlertDialog.Cancel>
			<AlertDialog.Action variant="destructive" disabled={busy !== null} onclick={remove}
				>{m['workflows.delete']()}</AlertDialog.Action
			>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>

<PageTitle parts={[m['workflows.title'](), m['settings.title']()]} />
