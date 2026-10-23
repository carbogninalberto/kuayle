<script lang="ts">
	import { Input } from '$lib/components/ui/input/index.js';
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { listTemplates, createTemplate, updateTemplate, deleteTemplate } from '$lib/api/issue-templates';
	import type { IssueTemplate, CreateIssueTemplateRequest, UpdateIssueTemplateRequest } from '$lib/types/issue';
	import { getStatusLabel, getPriorityLabel } from '$lib/types/issue';
	import type { IssueStatus, IssuePriority } from '$lib/types/issue';
	import EmptyState from '$lib/components/shared/EmptyState.svelte';
	import RichEditor from '$lib/components/shared/RichEditor.svelte';
	import { Button } from '$lib/components/ui/button';
	import { Badge } from '$lib/components/ui/badge';
	import * as Dialog from '$lib/components/ui/dialog';
	import * as Select from '$lib/components/ui/select';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/paraglide/messages.js';
	import { getLocale, setLocale } from '$lib/paraglide/runtime.js';
	import { Plus, Trash2, FileText, Pencil } from '@lucide/svelte';

	const slug = $derived(page.params.workspaceSlug ?? '');

	let templates = $state<IssueTemplate[]>([]);
	let loading = $state(true);
	let showDialog = $state(false);
	let editingTemplate = $state<IssueTemplate | null>(null);

	let formTitle = $state('');
	let formDescription = $state('');
	let formStatus = $state<IssueStatus>('backlog');
	let formPriority = $state<IssuePriority>(0);
	let saving = $state(false);
	let editorVersion = $state(0);
	const dialogTitle = $derived(
		editingTemplate ? m['settings.templates.edit_title']() : m['settings.templates.create_title']()
	);

	onMount(async () => {
		try {
			templates = await listTemplates(slug);
		} finally {
			loading = false;
		}
	});

	function resetForm() {
		formTitle = '';
		formDescription = '';
		formStatus = 'backlog';
		formPriority = 0;
		editorVersion++;
	}

	function openCreateDialog() {
		editingTemplate = null;
		resetForm();
		showDialog = true;
	}

	function openEditDialog(template: IssueTemplate) {
		editingTemplate = template;
		formTitle = template.title;
		formDescription = template.description ?? '';
		formStatus = template.status ?? 'backlog';
		formPriority = template.priority ?? 0;
		editorVersion++;
		showDialog = true;
	}

	function normalizedDescription(html: string): string {
		const value = html.trim();
		return /^<p>(?:\s|&nbsp;|<br\s*\/?>)*<\/p>$/i.test(value) ? '' : value;
	}

	async function handleSave() {
		if (!formTitle.trim()) {
			appToast.error(m['settings.templates.title_required']());
			return;
		}
		saving = true;
		try {
			const description = normalizedDescription(formDescription);
			if (editingTemplate) {
				const data: UpdateIssueTemplateRequest = {
					title: formTitle.trim(),
					description,
					status: formStatus,
					priority: formPriority
				};
				const template = await updateTemplate(slug, editingTemplate.id, data);
				templates = templates.map((item) => (item.id === template.id ? template : item));
				appToast.success(m['settings.templates.updated']());
			} else {
				const data: CreateIssueTemplateRequest = {
					title: formTitle.trim(),
					description: description || undefined,
					status: formStatus,
					priority: formPriority
				};
				const template = await createTemplate(slug, data);
				templates = [template, ...templates];
				appToast.success(m['settings.templates.created']());
			}
			showDialog = false;
			editingTemplate = null;
			resetForm();
		} catch (err: any) {
			appToast.apiError(
				err,
				editingTemplate ? m['settings.templates.failed_update']() : m['settings.templates.failed_create']()
			);
		} finally {
			saving = false;
		}
	}

	async function handleDelete(id: string) {
		try {
			await deleteTemplate(slug, id);
			templates = templates.filter((t) => t.id !== id);
			appToast.success(m['settings.templates.deleted']());
		} catch (err: any) {
			appToast.apiError(err, m['settings.templates.failed_delete']());
		}
	}

	function statusLabel(status: IssueStatus | null): string {
		return status ? getStatusLabel(status) || status : '';
	}

	function priorityLabel(priority: IssuePriority | null): string {
		return priority != null ? getPriorityLabel(priority) || `P${priority}` : '';
	}

	function setFormPriority(value: string | undefined) {
		if (value) formPriority = Number(value) as IssuePriority;
	}
</script>

<div class="mx-auto max-w-2xl px-8 py-10">
	<div class="flex items-center justify-between">
		<h1 class="text-2xl font-semibold text-[var(--color-text-primary)]">{m['settings.templates.title']()}</h1>
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
				{#each templates as template, i (template.id)}
					<div class="group flex items-center gap-4 px-5 py-3.5 {i > 0 ? 'border-t border-[var(--app-border)]' : ''}">
						<FileText size={16} class="shrink-0 text-[var(--color-text-tertiary)]" />
						<div class="flex-1 min-w-0">
							<span class="text-sm font-medium text-[var(--color-text-primary)]"
								>{template.title || m['settings.templates.untitled']()}</span
							>
						</div>
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

<Dialog.Root bind:open={showDialog}>
	<Dialog.Content class="sm:max-w-md">
		<Dialog.Header>
			<Dialog.Title>{dialogTitle}</Dialog.Title>
			<Dialog.Description>
				{editingTemplate ? m['settings.templates.edit_desc']() : m['settings.templates.create_desc']()}
			</Dialog.Description>
		</Dialog.Header>
		<div class="flex flex-col gap-4 py-4">
			<div class="flex flex-col gap-1.5">
				<label for="tpl-title" class="text-sm text-[var(--color-text-secondary)]"
					>{m['settings.templates.title_field']()}</label
				>
				<Input
					variant="plain"
					id="tpl-title"
					type="text"
					bind:value={formTitle}
					placeholder={m['settings.templates.title_placeholder']()}
					class="rounded-md border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-3 py-2 text-sm text-[var(--color-text-primary)] outline-none focus:border-[var(--app-accent)]"
				/>
			</div>
			<div class="flex flex-col gap-1.5">
				<label for="tpl-desc" class="text-sm text-[var(--color-text-secondary)]"
					>{m['settings.templates.description']()}</label
				>
				{#key editorVersion}
					<RichEditor
						content={formDescription}
						workspaceSlug={slug}
						placeholder={m['settings.templates.description_placeholder']()}
						bubbleMenu={true}
						borderless={true}
						minHeight="120px"
						onupdate={(html: string) => (formDescription = html)}
					/>
				{/key}
			</div>
			<div class="flex gap-4">
				<div class="flex flex-1 flex-col gap-1.5">
					<span class="text-sm text-[var(--color-text-secondary)]">{m['settings.templates.default_status']()}</span>
					<Select.Root
						type="single"
						value={formStatus}
						onValueChange={(value) => {
							if (value) formStatus = value as IssueStatus;
						}}
					>
						<Select.Trigger
							aria-label={m['settings.templates.default_status']()}
							class="w-full border-[var(--app-border)] bg-[var(--color-bg-secondary)] text-[var(--color-text-primary)]"
						>
							{statusLabel(formStatus)}
						</Select.Trigger>
						<Select.Content>
							<Select.Item value="backlog">Backlog</Select.Item>
							<Select.Item value="todo">Todo</Select.Item>
							<Select.Item value="in_progress">In Progress</Select.Item>
							<Select.Item value="in_review">In Review</Select.Item>
							<Select.Item value="done">Done</Select.Item>
							<Select.Item value="cancelled">Cancelled</Select.Item>
						</Select.Content>
					</Select.Root>
				</div>
				<div class="flex flex-1 flex-col gap-1.5">
					<span class="text-sm text-[var(--color-text-secondary)]">{m['settings.templates.default_priority']()}</span>
					<Select.Root type="single" value={String(formPriority)} onValueChange={setFormPriority}>
						<Select.Trigger
							aria-label={m['settings.templates.default_priority']()}
							class="w-full border-[var(--app-border)] bg-[var(--color-bg-secondary)] text-[var(--color-text-primary)]"
						>
							{priorityLabel(formPriority)}
						</Select.Trigger>
						<Select.Content>
							<Select.Item value="0">No priority</Select.Item>
							<Select.Item value="1">Urgent</Select.Item>
							<Select.Item value="2">High</Select.Item>
							<Select.Item value="3">Medium</Select.Item>
							<Select.Item value="4">Low</Select.Item>
						</Select.Content>
					</Select.Root>
				</div>
			</div>
		</div>
		<Dialog.Footer>
			<Button variant="outline" onclick={() => (showDialog = false)} disabled={saving}>{m['settings.cancel']()}</Button>
			<Button onclick={handleSave} disabled={saving}>
				{saving
					? m['settings.saving']()
					: editingTemplate
						? m['settings.templates.save_changes']()
						: m['settings.templates.create_title']()}
			</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
