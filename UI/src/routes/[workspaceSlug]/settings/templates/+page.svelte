<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { listTemplates, createTemplate, updateTemplate, deleteTemplate } from '$lib/api/issue-templates';
	import { listTeams } from '$lib/api/teams';
	import { listLabels } from '$lib/api/labels';
	import { listMembers } from '$lib/api/members';
	import type {
		IssueTemplate,
		CreateIssueTemplateRequest,
		UpdateIssueTemplateRequest,
		IssueStatus,
		IssuePriority
	} from '$lib/types/issue';
	import { getStatusLabel, getPriorityLabel } from '$lib/types/issue';
	import type { Team } from '$lib/types/team';
	import type { Label } from '$lib/types/label';
	import type { WorkspaceMember } from '$lib/types/workspace';
	import EmptyState from '$lib/components/shared/EmptyState.svelte';
	import { Button } from '$lib/components/ui/button';
	import { Badge } from '$lib/components/ui/badge';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/paraglide/messages.js';
	import { Plus, Trash2, FileText, Pencil } from '@lucide/svelte';
	import IssueTemplateDialog from './IssueTemplateDialog.svelte';

	const slug = $derived(page.params.workspaceSlug ?? '');

	let templates = $state<IssueTemplate[]>([]);
	let teams = $state<Team[]>([]);
	let labels = $state<Label[]>([]);
	let members = $state<WorkspaceMember[]>([]);
	let loading = $state(true);
	let showDialog = $state(false);
	let editingTemplate = $state<IssueTemplate | null>(null);
	let saving = $state(false);

	onMount(async () => {
		const [templateResult, teamResult, labelResult, memberResult] = await Promise.allSettled([
			listTemplates(slug),
			listTeams(slug),
			listLabels(slug),
			listMembers(slug)
		]);
		if (templateResult.status === 'fulfilled') templates = templateResult.value;
		if (teamResult.status === 'fulfilled') teams = teamResult.value;
		if (labelResult.status === 'fulfilled') labels = labelResult.value;
		if (memberResult.status === 'fulfilled') members = memberResult.value;
		loading = false;
	});

	function openCreateDialog() {
		editingTemplate = null;
		showDialog = true;
	}

	function openEditDialog(template: IssueTemplate) {
		editingTemplate = template;
		showDialog = true;
	}

	async function handleSave(data: CreateIssueTemplateRequest | UpdateIssueTemplateRequest) {
		const target = editingTemplate;
		saving = true;
		try {
			if (target) {
				const template = await updateTemplate(slug, target.id, data as UpdateIssueTemplateRequest);
				templates = templates.map((item) => (item.id === template.id ? template : item));
				appToast.success(m['settings.templates.updated']());
			} else {
				const template = await createTemplate(slug, data as CreateIssueTemplateRequest);
				templates = [template, ...templates];
				appToast.success(m['settings.templates.created']());
			}
			showDialog = false;
			editingTemplate = null;
		} catch (err: any) {
			appToast.apiError(
				err,
				target ? m['settings.templates.failed_update']() : m['settings.templates.failed_create']()
			);
		} finally {
			saving = false;
		}
	}

	async function handleDelete(id: string) {
		try {
			await deleteTemplate(slug, id);
			templates = templates.filter((template) => template.id !== id);
			appToast.success(m['settings.templates.deleted']());
		} catch (err: any) {
			appToast.apiError(err, m['settings.templates.failed_delete']());
		}
	}

	function statusLabel(status: IssueStatus | null): string {
		return status ? getStatusLabel(status) || status : '';
	}

	function priorityLabel(priority: IssuePriority | null): string {
		return priority !== null ? getPriorityLabel(priority) || `P${priority}` : '';
	}

	function templateTeam(template: IssueTemplate): Team | undefined {
		return teams.find((team) => team.id === template.team_id);
	}
</script>

<div class="mx-auto max-w-2xl px-8 py-10">
	<div class="flex items-center justify-between">
		<h1 class="text-2xl font-semibold text-[var(--color-text-primary)]">
			{m['settings.templates.title']()}
		</h1>
		<Button
			variant="plain"
			onclick={openCreateDialog}
			class="flex items-center gap-1 rounded-md bg-[var(--app-accent)] px-3 py-1.5 text-sm text-[var(--app-accent-foreground)] hover:bg-[var(--app-accent-hover)]"
		>
			<Plus size={14} />
			{m['settings.templates.new']()}
		</Button>
	</div>

	<div class="mt-8">
		{#if !loading && templates.length === 0}
			<EmptyState
				title={m['settings.templates.no_templates']()}
				description={m['settings.templates.no_templates_desc']()}
				action={{ label: m['settings.templates.new'](), onclick: openCreateDialog }}
			/>
		{:else}
			<div class="rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
				{#each templates as template, index (template.id)}
					{@const templateGroup = templateTeam(template)}
					<div
						class="group flex items-center gap-4 px-5 py-3.5 {index > 0 ? 'border-t border-[var(--app-border)]' : ''}"
					>
						<FileText size={16} class="shrink-0 text-[var(--color-text-tertiary)]" />
						<div class="min-w-0 flex-1">
							<div class="text-sm font-medium text-[var(--color-text-primary)]">
								{template.title || m['settings.templates.untitled']()}
							</div>
							{#if template.default_issue_title}
								<div class="mt-0.5 truncate text-xs text-[var(--color-text-tertiary)]">
									{m['settings.templates.issue_title_field']()}: {template.default_issue_title}
								</div>
							{/if}
						</div>
						{#if templateGroup}
							<Badge variant="outline" class="text-[10px]">{templateGroup.key}</Badge>
						{/if}
						{#if template.status}
							<Badge variant="outline" class="text-[10px]">{statusLabel(template.status)}</Badge>
						{/if}
						{#if template.priority != null}
							<Badge variant="outline" class="text-[10px]">{priorityLabel(template.priority)}</Badge>
						{/if}
						<Button
							aria-label={m['settings.templates.edit_action']({
								title: template.title || m['settings.templates.untitled']()
							})}
							variant="ghost"
							size="icon-sm"
							onclick={() => openEditDialog(template)}
							class="text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)]"
						>
							<Pencil size={14} />
						</Button>
						<Button
							aria-label={m['common.delete']()}
							variant="ghost"
							size="icon-sm"
							onclick={() => handleDelete(template.id)}
							class="opacity-0 group-hover:opacity-100 text-[var(--color-text-tertiary)] hover:text-[var(--color-error)] focus:opacity-100 max-sm:opacity-100"
						>
							<Trash2 size={14} />
						</Button>
					</div>
				{/each}
			</div>
		{/if}
	</div>
</div>

<IssueTemplateDialog
	bind:open={showDialog}
	{slug}
	template={editingTemplate}
	{teams}
	{labels}
	{members}
	{saving}
	onlabelcreated={(label) => {
		labels = [...labels.filter((item) => item.id !== label.id), label];
	}}
	onsubmit={handleSave}
/>
