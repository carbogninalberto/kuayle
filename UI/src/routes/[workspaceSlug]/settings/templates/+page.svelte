<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { listTemplates, createTemplate, deleteTemplate } from '$lib/api/issue-templates';
	import type { IssueTemplate, CreateIssueTemplateRequest } from '$lib/types/issue';
	import { STATUS_LABELS, PRIORITY_LABELS } from '$lib/types/issue';
	import type { IssueStatus, IssuePriority } from '$lib/types/issue';
	import EmptyState from '$lib/components/shared/EmptyState.svelte';
	import RichEditor from '$lib/components/shared/RichEditor.svelte';
	import { Button } from '$lib/components/ui/button';
	import { Badge } from '$lib/components/ui/badge';
	import * as Dialog from '$lib/components/ui/dialog';
	import * as Select from '$lib/components/ui/select';
	import { toast } from 'svelte-sonner';
	import { Plus, Trash2, FileText } from 'lucide-svelte';

	const slug = $derived(page.params.workspaceSlug ?? '');

	let templates = $state<IssueTemplate[]>([]);
	let loading = $state(true);
	let showCreate = $state(false);

	let formTitle = $state('');
	let formDescription = $state('');
	let formStatus = $state<IssueStatus>('backlog');
	let formPriority = $state<IssuePriority>(0);
	let creating = $state(false);
	let editorVersion = $state(0);

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
		resetForm();
		showCreate = true;
	}

	async function handleCreate() {
		if (!formTitle.trim()) {
			toast.error('Title is required');
			return;
		}
		creating = true;
		try {
			const data: CreateIssueTemplateRequest = {
				title: formTitle.trim(),
				description: formDescription.trim() || undefined,
				status: formStatus,
				priority: formPriority
			};
			const template = await createTemplate(slug, data);
			templates = [template, ...templates];
			showCreate = false;
			resetForm();
			toast.success('Template created');
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to create template');
		} finally {
			creating = false;
		}
	}

	async function handleDelete(id: string) {
		try {
			await deleteTemplate(slug, id);
			templates = templates.filter((t) => t.id !== id);
			toast.success('Template deleted');
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to delete template');
		}
	}

	function statusLabel(status: IssueStatus | null): string {
		return status ? (STATUS_LABELS[status] || status) : '';
	}

	function priorityLabel(priority: IssuePriority | null): string {
		return priority != null ? (PRIORITY_LABELS[priority] || `P${priority}`) : '';
	}

	function setFormPriority(value: string | undefined) {
		if (value) formPriority = Number(value) as IssuePriority;
	}
</script>

<div class="mx-auto max-w-2xl px-8 py-10">
	<div class="flex items-center justify-between">
		<h1 class="text-2xl font-semibold text-[var(--color-text-primary)]">Templates</h1>
		<button
			onclick={openCreateDialog}
			class="flex items-center gap-1 rounded-md bg-[var(--app-accent)] px-3 py-1.5 text-sm text-[var(--app-accent-foreground)] hover:bg-[var(--app-accent-hover)]"
		>
			<Plus size={14} />
			New Template
		</button>
	</div>

	<div class="mt-8">
		{#if !loading && templates.length === 0}
			<EmptyState
				title="No templates yet"
				description="Create issue templates to standardize your team's workflow"
				action={{ label: 'New Template', onclick: openCreateDialog }}
			/>
		{:else}
			<div class="rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
				{#each templates as template, i (template.id)}
					<div class="group flex items-center gap-4 px-5 py-3.5 {i > 0 ? 'border-t border-[var(--app-border)]' : ''}">
						<FileText size={16} class="shrink-0 text-[var(--color-text-tertiary)]" />
						<div class="flex-1 min-w-0">
							<span class="text-sm font-medium text-[var(--color-text-primary)]">{template.title || 'Untitled template'}</span>
						</div>
						{#if template.status}
							<Badge variant="outline" class="text-[10px]">{statusLabel(template.status)}</Badge>
						{/if}
						{#if template.priority != null}
							<Badge variant="outline" class="text-[10px]">{priorityLabel(template.priority)}</Badge>
						{/if}
						<Button
							variant="ghost"
							size="icon-sm"
							onclick={() => handleDelete(template.id)}
							class="opacity-0 group-hover:opacity-100 text-[var(--color-text-tertiary)] hover:text-[var(--color-error)]"
						>
							<Trash2 size={14} />
						</Button>
					</div>
				{/each}
			</div>
		{/if}
	</div>
</div>

<Dialog.Root bind:open={showCreate}>
	<Dialog.Content class="sm:max-w-md">
		<Dialog.Header>
			<Dialog.Title>Create Issue Template</Dialog.Title>
			<Dialog.Description>Define a reusable template for creating issues.</Dialog.Description>
		</Dialog.Header>
		<div class="flex flex-col gap-4 py-4">
			<div class="flex flex-col gap-1.5">
				<label for="tpl-title" class="text-sm text-[var(--color-text-secondary)]">Title</label>
				<input
					id="tpl-title"
					type="text"
					bind:value={formTitle}
					placeholder="e.g. Bug Report"
					class="rounded-md border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-3 py-2 text-sm text-[var(--color-text-primary)] outline-none focus:border-[var(--app-accent)]"
				/>
			</div>
			<div class="flex flex-col gap-1.5">
				<label for="tpl-desc" class="text-sm text-[var(--color-text-secondary)]">Description</label>
				{#key editorVersion}
				<RichEditor
					content={formDescription}
					placeholder="Template description..."
					bubbleMenu={true}
					borderless={true}
					minHeight="120px"
					onupdate={(html: string) => (formDescription = html)}
				/>
				{/key}
			</div>
			<div class="flex gap-4">
				<div class="flex flex-1 flex-col gap-1.5">
					<span class="text-sm text-[var(--color-text-secondary)]">Default status</span>
					<Select.Root
						type="single"
						value={formStatus}
						onValueChange={(value) => {
							if (value) formStatus = value as IssueStatus;
						}}
					>
						<Select.Trigger class="w-full border-[var(--app-border)] bg-[var(--color-bg-secondary)] text-[var(--color-text-primary)]">
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
					<span class="text-sm text-[var(--color-text-secondary)]">Default priority</span>
					<Select.Root
						type="single"
						value={String(formPriority)}
						onValueChange={setFormPriority}
					>
						<Select.Trigger class="w-full border-[var(--app-border)] bg-[var(--color-bg-secondary)] text-[var(--color-text-primary)]">
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
			<Button variant="outline" onclick={() => (showCreate = false)}>Cancel</Button>
			<Button onclick={handleCreate} disabled={creating}>
				{creating ? 'Creating...' : 'Create Template'}
			</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
