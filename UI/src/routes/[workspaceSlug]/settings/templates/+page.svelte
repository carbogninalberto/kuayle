<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { listTemplates, createTemplate, deleteTemplate } from '$lib/api/issue-templates';
	import type { IssueTemplate, CreateIssueTemplateRequest } from '$lib/types/issue';
	import { STATUS_LABELS, PRIORITY_LABELS } from '$lib/types/issue';
	import type { IssueStatus, IssuePriority } from '$lib/types/issue';
	import EmptyState from '$lib/components/shared/EmptyState.svelte';
	import { Button } from '$lib/components/ui/button';
	import { Badge } from '$lib/components/ui/badge';
	import * as Dialog from '$lib/components/ui/dialog';
	import { toast } from 'svelte-sonner';
	import { Plus, Trash2, FileText } from 'lucide-svelte';

	const slug = $derived(page.params.workspaceSlug ?? '');

	let templates = $state<IssueTemplate[]>([]);
	let loading = $state(true);
	let showCreate = $state(false);

	let formName = $state('');
	let formTitle = $state('');
	let formDescription = $state('');
	let formStatus = $state<IssueStatus>('backlog');
	let formPriority = $state<IssuePriority>(0);
	let creating = $state(false);

	onMount(async () => {
		try {
			templates = await listTemplates(slug);
		} finally {
			loading = false;
		}
	});

	function resetForm() {
		formName = '';
		formTitle = '';
		formDescription = '';
		formStatus = 'backlog';
		formPriority = 0;
	}

	async function handleCreate() {
		if (!formName.trim() || !formTitle.trim()) {
			toast.error('Name and title are required');
			return;
		}
		creating = true;
		try {
			const data: CreateIssueTemplateRequest = {
				name: formName.trim(),
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

	function statusLabel(status: IssueStatus): string {
		return STATUS_LABELS[status] || status;
	}

	function priorityLabel(priority: IssuePriority): string {
		return PRIORITY_LABELS[priority] || `P${priority}`;
	}
</script>

<div class="mx-auto max-w-2xl px-8 py-10">
	<div class="flex items-center justify-between">
		<h1 class="text-2xl font-semibold text-[var(--color-text-primary)]">Templates</h1>
		<button
			onclick={() => (showCreate = true)}
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
				action={{ label: 'New Template', onclick: () => (showCreate = true) }}
			/>
		{:else}
			<div class="rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
				{#each templates as template, i (template.id)}
					<div class="group flex items-center gap-4 px-5 py-3.5 {i > 0 ? 'border-t border-[var(--app-border)]' : ''}">
						<FileText size={16} class="shrink-0 text-[var(--color-text-tertiary)]" />
						<div class="flex-1 min-w-0">
							<div class="flex items-center gap-2">
								<span class="text-sm font-medium text-[var(--color-text-primary)]">{template.name}</span>
							</div>
							<div class="mt-0.5 flex items-center gap-2">
								<span class="text-xs text-[var(--color-text-tertiary)]">{template.title}</span>
								{#if template.description}
									<span class="text-xs text-[var(--color-text-tertiary)]">· {template.description}</span>
								{/if}
							</div>
						</div>
						<Badge variant="outline" class="text-[10px]">{statusLabel(template.status)}</Badge>
						<Badge variant="outline" class="text-[10px]">{priorityLabel(template.priority)}</Badge>
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
				<label for="tpl-name" class="text-sm text-[var(--color-text-secondary)]">Template name</label>
				<input
					id="tpl-name"
					type="text"
					bind:value={formName}
					placeholder="e.g. Bug Report"
					class="rounded-md border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-3 py-2 text-sm text-[var(--color-text-primary)] outline-none focus:border-[var(--app-accent)]"
				/>
			</div>
			<div class="flex flex-col gap-1.5">
				<label for="tpl-title" class="text-sm text-[var(--color-text-secondary)]">Issue title template</label>
				<input
					id="tpl-title"
					type="text"
					bind:value={formTitle}
					placeholder="e.g. [Bug] ..."
					class="rounded-md border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-3 py-2 text-sm text-[var(--color-text-primary)] outline-none focus:border-[var(--app-accent)]"
				/>
			</div>
			<div class="flex flex-col gap-1.5">
				<label for="tpl-desc" class="text-sm text-[var(--color-text-secondary)]">Description</label>
				<textarea
					id="tpl-desc"
					bind:value={formDescription}
					placeholder="Template description..."
					rows={3}
					class="rounded-md border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-3 py-2 text-sm text-[var(--color-text-primary)] outline-none focus:border-[var(--app-accent)]"
				></textarea>
			</div>
			<div class="flex gap-4">
				<div class="flex flex-1 flex-col gap-1.5">
					<label for="tpl-status" class="text-sm text-[var(--color-text-secondary)]">Default status</label>
					<select
						id="tpl-status"
						bind:value={formStatus}
						class="rounded-md border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-3 py-2 text-sm text-[var(--color-text-primary)] outline-none focus:border-[var(--app-accent)]"
					>
						<option value="backlog">Backlog</option>
						<option value="todo">Todo</option>
						<option value="in_progress">In Progress</option>
						<option value="in_review">In Review</option>
						<option value="done">Done</option>
					</select>
				</div>
				<div class="flex flex-1 flex-col gap-1.5">
					<label for="tpl-priority" class="text-sm text-[var(--color-text-secondary)]">Default priority</label>
					<select
						id="tpl-priority"
						bind:value={formPriority}
						class="rounded-md border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-3 py-2 text-sm text-[var(--color-text-primary)] outline-none focus:border-[var(--app-accent)]"
					>
						<option value={0}>No priority</option>
						<option value={1}>Urgent</option>
						<option value={2}>High</option>
						<option value={3}>Medium</option>
						<option value={4}>Low</option>
					</select>
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
