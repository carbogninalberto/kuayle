<script lang="ts">
	import { untrack } from 'svelte';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Button } from '$lib/components/ui/button';
	import * as Dialog from '$lib/components/ui/dialog';
	import * as Select from '$lib/components/ui/select';
	import RichEditor from '$lib/components/shared/RichEditor.svelte';
	import { LabelSelector } from '$lib/features/issues/selectors';
	import IssueStatusIcon from '$lib/features/issues/IssueStatusIcon.svelte';
	import IssuePriorityIcon from '$lib/features/issues/IssuePriorityIcon.svelte';
	import { getPriorityLabel, getStatusLabel } from '$lib/types/issue';
	import type {
		CreateIssueTemplateRequest,
		IssuePriority,
		IssueStatus,
		IssueTemplate,
		UpdateIssueTemplateRequest
	} from '$lib/types/issue';
	import type { Label } from '$lib/types/label';
	import type { StatusCategory } from '$lib/types/team-status';
	import type { Team } from '$lib/types/team';
	import type { WorkspaceMember } from '$lib/types/workspace';
	import { m } from '$lib/paraglide/messages.js';
	import { FileText, ImagePlus, Paperclip, Tag, User } from '@lucide/svelte';

	let {
		open = $bindable(false),
		slug,
		template = null,
		teams = [],
		labels = [],
		members = [],
		saving = false,
		onlabelcreated,
		onsubmit
	}: {
		open: boolean;
		slug: string;
		template?: IssueTemplate | null;
		teams?: Team[];
		labels?: Label[];
		members?: WorkspaceMember[];
		saving?: boolean;
		onlabelcreated?: (label: Label) => void;
		onsubmit: (data: CreateIssueTemplateRequest | UpdateIssueTemplateRequest) => Promise<void>;
	} = $props();

	const STATUS_CATEGORY: Record<IssueStatus, StatusCategory> = {
		backlog: 'backlog',
		todo: 'unstarted',
		in_progress: 'started',
		in_review: 'started',
		done: 'completed',
		cancelled: 'cancelled'
	};
	const STATUS_OPTIONS: IssueStatus[] = ['backlog', 'todo', 'in_progress', 'in_review', 'done', 'cancelled'];
	const PRIORITY_OPTIONS: IssuePriority[] = [0, 1, 2, 3, 4];

	let formTitle = $state('');
	let formDescription = $state('');
	let formTeamId = $state<string | null>(null);
	let formStatus = $state<IssueStatus | null>(null);
	let formPriority = $state<IssuePriority | null>(null);
	let formAssigneeId = $state<string | null>(null);
	let formLabelIds = $state<string[]>([]);
	let labelsOpen = $state(false);
	let editorVersion = $state(0);
	let pendingUploads = $state(0);
	let descriptionEditor = $state<{ insertFiles: (files: File[]) => void } | null>(null);

	const editing = $derived(template !== null);
	const dialogTitle = $derived(editing ? m['settings.templates.edit_title']() : m['settings.templates.create_title']());
	const selectedTeam = $derived(teams.find((team) => team.id === formTeamId));
	const selectedAssignee = $derived(members.find((member) => member.user_id === formAssigneeId));
	const selectedLabels = $derived(labels.filter((label) => formLabelIds.includes(label.id)));
	const uploadUrl = $derived(slug && formTeamId ? `/api/workspaces/${slug}/upload?team_id=${formTeamId}` : undefined);

	function initialize(source: IssueTemplate | null) {
		formTitle = source?.title ?? '';
		formDescription = source?.description ?? '';
		formTeamId = source?.team_id ?? null;
		formStatus = source?.status ?? null;
		formPriority = source?.priority ?? null;
		formAssigneeId = source?.assignee_id ?? null;
		formLabelIds = Array.isArray(source?.label_ids) ? [...source.label_ids] : [];
		pendingUploads = 0;
		editorVersion++;
	}

	$effect(() => {
		if (open) {
			untrack(() => initialize(template));
		}
	});

	function normalizedDescription(html: string): string {
		const value = html.trim();
		return /^<p>(?:\s|&nbsp;|<br\s*\/?>)*<\/p>$/i.test(value) ? '' : value;
	}

	function sameLabels(left: string[], right: string[]): boolean {
		if (left.length !== right.length) return false;
		const expected = new Set(right);
		return left.every((id) => expected.has(id));
	}

	async function handleSubmit() {
		const title = formTitle.trim();
		if (!title || saving || pendingUploads > 0) return;
		const description = normalizedDescription(formDescription);

		if (!template) {
			const data: CreateIssueTemplateRequest = { title };
			if (description) data.description = description;
			if (formTeamId) data.team_id = formTeamId;
			if (formStatus) data.status = formStatus;
			if (formPriority !== null) data.priority = formPriority;
			if (formAssigneeId) data.assignee_id = formAssigneeId;
			if (formLabelIds.length > 0) data.label_ids = [...formLabelIds];
			await onsubmit(data);
			return;
		}

		const data: UpdateIssueTemplateRequest = { title };
		if (description !== normalizedDescription(template.description ?? '')) {
			data.description = description;
		}
		if (formTeamId !== template.team_id) data.team_id = formTeamId;
		if (formStatus !== template.status) data.status = formStatus;
		if (formPriority !== template.priority) data.priority = formPriority;
		if (formAssigneeId !== template.assignee_id) data.assignee_id = formAssigneeId;
		if (!sameLabels(formLabelIds, template.label_ids)) data.label_ids = [...formLabelIds];
		await onsubmit(data);
	}

	function handleKeydown(event: KeyboardEvent) {
		if (event.key === 'Enter' && (event.metaKey || event.ctrlKey)) {
			event.preventDefault();
			void handleSubmit();
		}
	}

	function handleOpenChange(next: boolean) {
		if (!next && saving) return;
		open = next;
	}

	function toggleLabel(labelId: string) {
		formLabelIds = formLabelIds.includes(labelId)
			? formLabelIds.filter((id) => id !== labelId)
			: [...formLabelIds, labelId];
	}

	function chooseFiles(imagesOnly = false) {
		if (!uploadUrl) return;
		const input = document.createElement('input');
		input.type = 'file';
		input.multiple = !imagesOnly;
		if (imagesOnly) input.accept = 'image/*';
		input.onchange = () => {
			const files = Array.from(input.files ?? []);
			if (files.length > 0) descriptionEditor?.insertFiles(files);
		};
		input.click();
	}

	function statusLabel(status: IssueStatus | null): string {
		return status ? getStatusLabel(status) : m['settings.templates.no_default_status']();
	}

	function priorityLabel(priority: IssuePriority | null): string {
		return priority !== null ? getPriorityLabel(priority) : m['settings.templates.no_default_priority']();
	}
</script>

<Dialog.Root {open} onOpenChange={handleOpenChange}>
	<Dialog.Content
		class="sm:max-w-[640px] gap-0 overflow-hidden rounded-xl border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-0 max-sm:w-screen max-sm:h-dvh max-sm:max-w-none max-sm:rounded-none max-sm:top-0 max-sm:left-0 max-sm:translate-x-0 max-sm:flex max-sm:flex-col"
		showCloseButton={!saving}
		escapeKeydownBehavior={saving ? 'ignore' : 'close'}
		interactOutsideBehavior={saving ? 'ignore' : 'close'}
		onOpenAutoFocus={(event) => {
			event.preventDefault();
			document.getElementById('issue-template-title')?.focus();
		}}
	>
		<Dialog.Title class="sr-only">{dialogTitle}</Dialog.Title>
		<Dialog.Description class="sr-only">
			{editing ? m['settings.templates.edit_desc']() : m['settings.templates.create_desc']()}
		</Dialog.Description>

		<div class="flex items-center gap-2 border-b border-[var(--app-border)] px-4 py-3 pr-12">
			<FileText size={15} class="text-[var(--color-text-tertiary)]" />
			<span class="text-sm font-medium text-[var(--color-text-primary)]">{dialogTitle}</span>
		</div>

		<!-- svelte-ignore a11y_no_static_element_interactions -->
		<div
			class="px-4 py-3 max-sm:flex max-sm:min-h-0 max-sm:flex-1 max-sm:flex-col max-sm:overflow-y-auto"
			onkeydown={handleKeydown}
		>
			<label for="issue-template-title" class="sr-only">
				{m['settings.templates.title_field']()}
			</label>
			<Input
				variant="plain"
				id="issue-template-title"
				type="text"
				bind:value={formTitle}
				placeholder={m['settings.templates.title_placeholder']()}
				class="w-full bg-transparent text-lg font-semibold text-[var(--color-text-primary)] outline-none placeholder:text-[var(--color-text-tertiary)] max-sm:shrink-0"
			/>
			<div class="mt-4 max-h-[calc(55vh-150px)] overflow-y-auto max-sm:flex-1 max-sm:[max-height:none]">
				{#key editorVersion}
					{@const currentEditorVersion = editorVersion}
					<RichEditor
						bind:this={descriptionEditor}
						content={formDescription}
						workspaceSlug={slug}
						{members}
						ariaLabel={m['settings.templates.description']()}
						placeholder={m['settings.templates.description_placeholder']()}
						bubbleMenu={true}
						borderless={true}
						minHeight="120px"
						{uploadUrl}
						hideUploadButtons={true}
						onuploadschange={(pending) => {
							if (open && currentEditorVersion === editorVersion) pendingUploads = pending;
						}}
						onupdate={(html) => (formDescription = html)}
					/>
				{/key}
			</div>
		</div>

		<div class="flex flex-wrap items-center gap-1.5 border-t border-[var(--app-border)] px-4 py-2.5 max-sm:shrink-0">
			<Select.Root
				type="single"
				value={formTeamId ?? 'workspace'}
				onValueChange={(value) => (formTeamId = value === 'workspace' ? null : value)}
			>
				<Select.Trigger
					variant="plain"
					aria-label={m['sharedComponents.create_issue.team']()}
					class="flex items-center gap-1.5 rounded-full border border-[var(--app-border)] px-2.5 py-1 text-xs text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] max-sm:px-3 max-sm:py-1.5"
				>
					<span
						class="flex h-4 w-4 items-center justify-center rounded bg-[var(--app-accent)] text-[9px] font-bold text-[var(--app-accent-foreground)]"
					>
						{selectedTeam?.key?.charAt(0) ?? 'W'}
					</span>
					{selectedTeam?.key ?? m['settings.templates.workspace_wide']()}
				</Select.Trigger>
				<Select.Content>
					<Select.Item value="workspace">
						{m['settings.templates.workspace_wide']()}
					</Select.Item>
					{#each teams as team (team.id)}
						<Select.Item value={team.id}>{team.name}</Select.Item>
					{/each}
				</Select.Content>
			</Select.Root>

			<Select.Root
				type="single"
				value={formStatus ?? 'none'}
				onValueChange={(value) => (formStatus = value === 'none' ? null : (value as IssueStatus))}
			>
				<Select.Trigger
					variant="plain"
					aria-label={m['settings.templates.default_status']()}
					class="flex items-center gap-1.5 rounded-full border border-[var(--app-border)] px-2.5 py-1 text-xs text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] max-sm:px-3 max-sm:py-1.5"
				>
					<IssueStatusIcon category={formStatus ? STATUS_CATEGORY[formStatus] : undefined} size={12} />
					{statusLabel(formStatus)}
				</Select.Trigger>
				<Select.Content>
					<Select.Item value="none">{m['settings.templates.no_default_status']()}</Select.Item>
					{#each STATUS_OPTIONS as status (status)}
						<Select.Item value={status}>{getStatusLabel(status)}</Select.Item>
					{/each}
				</Select.Content>
			</Select.Root>

			<Select.Root
				type="single"
				value={formPriority === null ? 'none' : String(formPriority)}
				onValueChange={(value) => (formPriority = value === 'none' ? null : (Number(value) as IssuePriority))}
			>
				<Select.Trigger
					variant="plain"
					aria-label={m['settings.templates.default_priority']()}
					class="flex items-center gap-1.5 rounded-full border border-[var(--app-border)] px-2.5 py-1 text-xs text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] max-sm:px-3 max-sm:py-1.5"
				>
					<IssuePriorityIcon priority={formPriority ?? 0} size={12} />
					{priorityLabel(formPriority)}
				</Select.Trigger>
				<Select.Content>
					<Select.Item value="none">
						{m['settings.templates.no_default_priority']()}
					</Select.Item>
					{#each PRIORITY_OPTIONS as priority (priority)}
						<Select.Item value={String(priority)}>{getPriorityLabel(priority)}</Select.Item>
					{/each}
				</Select.Content>
			</Select.Root>

			<Select.Root
				type="single"
				value={formAssigneeId ?? 'unassigned'}
				onValueChange={(value) => (formAssigneeId = value === 'unassigned' ? null : value)}
			>
				<Select.Trigger
					variant="plain"
					aria-label={m['sharedComponents.create_issue.assignee']()}
					class="flex items-center gap-1.5 rounded-full border border-[var(--app-border)] px-2.5 py-1 text-xs text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] max-sm:px-3 max-sm:py-1.5"
				>
					<User size={12} />
					{selectedAssignee?.name ?? m['settings.templates.unassigned']()}
				</Select.Trigger>
				<Select.Content>
					<Select.Item value="unassigned">{m['settings.templates.unassigned']()}</Select.Item>
					{#each members as member (member.user_id)}
						<Select.Item value={member.user_id}>{member.name || member.email}</Select.Item>
					{/each}
				</Select.Content>
			</Select.Root>

			<LabelSelector
				bind:open={labelsOpen}
				{labels}
				value={formLabelIds}
				onchange={toggleLabel}
				oncreated={onlabelcreated}
				{slug}
			>
				{#snippet trigger(props)}
					<Button
						{...props}
						variant="plain"
						aria-label={m['sharedComponents.create_issue.labels']()}
						class="flex items-center gap-1.5 rounded-full border border-[var(--app-border)] px-2.5 py-1 text-xs text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] max-sm:px-3 max-sm:py-1.5"
					>
						<Tag size={12} />
						{#if selectedLabels.length === 0}
							{m['sharedComponents.create_issue.labels']()}
						{:else if selectedLabels.length === 1}
							{selectedLabels[0].name}
						{:else}
							{m['sharedComponents.create_issue.labels_count']({
								count: selectedLabels.length
							})}
						{/if}
					</Button>
				{/snippet}
			</LabelSelector>
		</div>

		<div
			class="flex items-center justify-between gap-3 border-t border-[var(--app-border)] px-4 py-2.5 max-sm:sticky max-sm:bottom-0 max-sm:shrink-0 max-sm:flex-col max-sm:items-stretch max-sm:bg-[var(--color-bg-secondary)] max-sm:pb-[calc(1rem+env(safe-area-inset-bottom,0px))]"
		>
			<div class="flex items-center gap-1 max-sm:order-2 max-sm:justify-center">
				<Button
					variant="plain"
					type="button"
					onclick={() => chooseFiles(true)}
					disabled={!uploadUrl || saving}
					class="flex h-7 w-7 items-center justify-center rounded text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-secondary)] disabled:opacity-40"
					title={uploadUrl
						? m['sharedComponents.rich_editor.upload_image']()
						: m['settings.templates.team_required_for_uploads']()}
					aria-label={uploadUrl
						? m['sharedComponents.rich_editor.upload_image']()
						: m['settings.templates.team_required_for_uploads']()}
				>
					<ImagePlus size={14} />
				</Button>
				<Button
					variant="plain"
					type="button"
					onclick={() => chooseFiles()}
					disabled={!uploadUrl || saving}
					class="flex h-7 w-7 items-center justify-center rounded text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-secondary)] disabled:opacity-40"
					title={uploadUrl
						? m['sharedComponents.rich_editor.attach_files']()
						: m['settings.templates.team_required_for_uploads']()}
					aria-label={uploadUrl
						? m['sharedComponents.rich_editor.attach_files']()
						: m['settings.templates.team_required_for_uploads']()}
				>
					<Paperclip size={14} />
				</Button>
			</div>
			<div class="flex items-center gap-2 max-sm:order-1">
				<Button variant="outline" size="sm" onclick={() => handleOpenChange(false)} disabled={saving}>
					{m['settings.cancel']()}
				</Button>
				<Button size="sm" onclick={handleSubmit} disabled={!formTitle.trim() || saving || pendingUploads > 0}>
					{saving
						? m['settings.saving']()
						: editing
							? m['settings.templates.save_changes']()
							: m['settings.templates.create_title']()}
				</Button>
			</div>
		</div>
	</Dialog.Content>
</Dialog.Root>
