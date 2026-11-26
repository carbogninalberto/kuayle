<script lang="ts">
	import { SettingsPageHeader, SettingsEmptyState } from '$lib/components/settings';
	import PageTitle from '$lib/components/shared/PageTitle.svelte';
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
	import { Button } from '$lib/components/ui/button';
	import { Badge } from '$lib/components/ui/badge';
	import { appToast } from '$lib/features/toast/toast';
	import { getLocale, m } from '$lib/features/i18n/messages';
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

<div class="settings-page mx-auto max-w-2xl px-4 py-6 sm:px-8 sm:py-10">
	<SettingsPageHeader title={m['settings.templates.title']()}>
		{#snippet action()}
			<Button
				variant="plain"
				onclick={openCreateDialog}
				class="flex items-center gap-1 rounded-md bg-[var(--app-accent)] px-3 py-1.5 text-sm text-[var(--app-accent-foreground)] hover:bg-[var(--app-accent-hover)]"
			>
				<Plus size={14} />
				{m['settings.templates.new']()}
			</Button>
		{/snippet}
	</SettingsPageHeader>

	<div class="mt-8">
		{#if !loading && templates.length === 0}
			<SettingsEmptyState
				title={m['settings.templates.no_templates']()}
				description={m['settings.templates.no_templates_desc']()}
				action={{ label: m['settings.templates.new'](), onclick: openCreateDialog }}
			/>
		{:else}
			<div class="rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
				{#each templates as template, index (template.id)}
					{@const templateGroup = templateTeam(template)}
					<div
						class="group flex flex-wrap items-center gap-x-3 gap-y-2 px-4 py-3.5 lg:flex-nowrap lg:gap-x-4 sm:px-5 {index > 0 ? 'border-t border-[var(--app-border)]' : ''}"
					>
						<FileText size={16} class="shrink-0 text-[var(--color-text-tertiary)]" />
						<div class="min-w-0 flex-1">
							<div class="break-words text-sm font-medium text-[var(--color-text-primary)]">
								{template.title || m['settings.templates.untitled']()}
							</div>
							{#if template.default_issue_title}
								<div class="mt-0.5 truncate text-xs text-[var(--color-text-tertiary)]">
									{m['settings.templates.issue_title_field']()}: {template.default_issue_title}
								</div>
							{/if}
							{#if template.is_active && template.next_run_at}
								<time datetime={template.next_run_at} class="mt-0.5 block text-xs text-muted-foreground">
									{m['settings.backups.next_run']({
										time: new Date(template.next_run_at).toLocaleString(getLocale(), {
											dateStyle: 'medium',
											timeStyle: 'short'
										})
									})}
								</time>
							{/if}
							{#if template.last_error}
								<p class="mt-0.5 break-words text-xs text-[var(--color-error)]">
									{m['settings.webhooks.last_error']({ error: template.last_error })}
								</p>
							{/if}
						</div>
						{#if templateGroup || template.status || template.priority != null}
							<div class="order-last flex w-full flex-wrap items-center gap-2 pl-7 lg:contents">
								{#if templateGroup}
									<Badge variant="outline" class="text-[10px]">{templateGroup.key}</Badge>
								{/if}
								{#if template.status}
									<Badge variant="outline" class="text-[10px]">{statusLabel(template.status)}</Badge>
								{/if}
								{#if template.priority != null}
									<Badge variant="outline" class="text-[10px]">{priorityLabel(template.priority)}</Badge>
								{/if}
							</div>
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
							class="text-[var(--color-text-tertiary)] hover:text-[var(--color-error)]"
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

<PageTitle parts={[m['settings.templates.title'](), m['settings.title']()]} />
