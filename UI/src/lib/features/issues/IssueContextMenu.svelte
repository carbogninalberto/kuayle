<script lang="ts">
	import type { Issue, IssuePriority, RelationType } from '$lib/types/issue';
	import { PRIORITY_LABELS } from '$lib/types/issue';
	import { teamStatusesState } from './team-statuses.state.svelte';
	import type { WorkspaceMember } from '$lib/types/workspace';
	import type { Label } from '$lib/types/label';
	import type { Project } from '$lib/types/project';
	import type { Cycle } from '$lib/types/cycle';
	import * as ContextMenu from '$lib/components/ui/context-menu';
	import * as AlertDialog from '$lib/components/ui/alert-dialog';
	import IssueStatusIcon from './IssueStatusIcon.svelte';
	import IssuePriorityIcon from './IssuePriorityIcon.svelte';
	import IssuePickerDialog from './IssuePickerDialog.svelte';
	import { issuesState } from './issues.state.svelte';
	import { convertIssueToProject, duplicateIssue } from '$lib/api/issues';
	import { showIssueDeletedToast } from './issue-deleted-toast';
	import { toast } from 'svelte-sonner';
	import type { Snippet } from 'svelte';
	import {
		ArrowUpCircle,
		CalendarDays,
		CircleDot,
		CircleUser,
		Copy,
		CopyPlus,
		CornerDownRight,
		ExternalLink,
		FolderKanban,
		GitBranch,
		Link as LinkIcon,
		RefreshCw,
		Tag,
		Trash2,
		X
	} from 'lucide-svelte';

	let {
		issue,
		slug,
		members = [],
		labels = [],
		projects = [],
		cycles = [],
		onaddrelation,
		children
	}: {
		issue: Issue;
		slug: string;
		members?: WorkspaceMember[];
		labels?: Label[];
		projects?: Project[];
		cycles?: Cycle[];
		onaddrelation?: (type: RelationType) => void;
		children: Snippet;
	} = $props();

	type PickerMode = 'sub_issue_of' | 'parent_of';

	const priorityValues: IssuePriority[] = [0, 1, 2, 3, 4];
	const iconClass = 'h-4 w-4 shrink-0 text-[var(--color-text-tertiary)]';
	const rowClass = 'flex items-center gap-2';

	let pickerOpen = $state(false);
	let pickerMode = $state<PickerMode>('sub_issue_of');
	let deleteOpen = $state(false);
	let duplicateOpen = $state(false);
	let convertOpen = $state(false);
	let removeParentOpen = $state(false);
	let includeSubIssues = $state(false);

	let pickerTitle = $derived(pickerMode === 'sub_issue_of' ? 'Set parent issue' : 'Make parent of issue');
	let pickerDescription = $derived(
		pickerMode === 'sub_issue_of'
			? `${issue.identifier} will become a sub-issue of the selected issue.`
			: `The selected issue will become a sub-issue of ${issue.identifier}.`
	);
	let pickerActionLabel = $derived(pickerMode === 'sub_issue_of' ? 'Set parent' : 'Make child');

	async function updateField(field: string, value: any) {
		try {
			await issuesState.update(slug, issue.identifier, { [field]: value });
		} catch (err: any) {
			toast.error(err?.error?.message || `Failed to update ${field}`);
		}
	}

	function copyToClipboard(text: string) {
		navigator.clipboard.writeText(text);
		toast.success('Copied to clipboard');
	}

	function dateOffset(days: number): string {
		const d = new Date();
		d.setDate(d.getDate() + days);
		return d.toISOString().split('T')[0];
	}

	function openPicker(mode: PickerMode) {
		pickerMode = mode;
		pickerOpen = true;
	}

	async function handlePickedIssue(selected: Issue) {
		try {
			if (pickerMode === 'sub_issue_of') {
				await issuesState.update(slug, issue.identifier, { parent_id: selected.id });
				toast.success(`${issue.identifier} is now a sub-issue of ${selected.identifier}`);
			} else {
				await issuesState.update(slug, selected.identifier, { parent_id: issue.id });
				toast.success(`${selected.identifier} is now a sub-issue of ${issue.identifier}`);
			}
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to update parent');
		}
	}

	async function handleRemoveParent() {
		try {
			await issuesState.update(slug, issue.identifier, { parent_id: '' });
			toast.success('Removed parent');
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to remove parent');
		}
		removeParentOpen = false;
	}

	async function handleDelete() {
		try {
			await issuesState.remove(slug, issue.identifier);
			showIssueDeletedToast(issue);
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to delete issue');
		}
		deleteOpen = false;
	}

	async function handleDuplicate() {
		try {
			const duplicated = await duplicateIssue(slug, issue.identifier, includeSubIssues);
			issuesState.issues = [duplicated, ...issuesState.issues.filter((item) => item.id !== duplicated.id)];
			issuesState.totalCount += 1;
			toast.success(`Duplicated as ${duplicated.identifier}`);
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to duplicate issue');
		}
		duplicateOpen = false;
	}

	async function handleConvertToProject() {
		try {
			const result = await convertIssueToProject(slug, issue.identifier);
			toast.success(`Converted to project ${result.project.name}`);
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to convert issue');
		}
		convertOpen = false;
	}
</script>

<ContextMenu.Root>
	<ContextMenu.Trigger>
		{@render children()}
	</ContextMenu.Trigger>
	<ContextMenu.Content class="w-60">
		<ContextMenu.Sub>
			<ContextMenu.SubTrigger>
				<span class={rowClass}>
					<IssueStatusIcon status={issue.status} category={issue.status_info?.category} color={issue.status_info?.color} size={14} />
					Status
				</span>
			</ContextMenu.SubTrigger>
			<ContextMenu.SubContent class="w-44">
				{#each teamStatusesState.statusOrder as ts}
					<ContextMenu.Item onclick={() => updateField('status_id', ts.id)}>
						<span class={rowClass}>
							<IssueStatusIcon category={ts.category} color={ts.color} size={14} />
							{ts.name}
						</span>
					</ContextMenu.Item>
				{/each}
			</ContextMenu.SubContent>
		</ContextMenu.Sub>

		<ContextMenu.Sub>
			<ContextMenu.SubTrigger>
				<span class={rowClass}>
					<IssuePriorityIcon priority={issue.priority} size={14} />
					Priority
				</span>
			</ContextMenu.SubTrigger>
			<ContextMenu.SubContent class="w-44">
				{#each priorityValues as value}
					<ContextMenu.Item onclick={() => updateField('priority', value)}>
						<span class={rowClass}>
							<IssuePriorityIcon priority={value} size={14} />
							{PRIORITY_LABELS[value]}
						</span>
					</ContextMenu.Item>
				{/each}
			</ContextMenu.SubContent>
		</ContextMenu.Sub>

		{#if members.length > 0}
			<ContextMenu.Sub>
				<ContextMenu.SubTrigger>
					<span class={rowClass}><CircleUser class={iconClass} />Assignee</span>
				</ContextMenu.SubTrigger>
				<ContextMenu.SubContent class="w-48">
					<ContextMenu.Item onclick={() => updateField('assignee_ids', [])}>
						<span class={rowClass}><X class={iconClass} />Clear all</span>
					</ContextMenu.Item>
					{#each members as member}
						{@const isAssigned = (issue.assignees ?? []).some(a => a.id === member.user_id)}
						<ContextMenu.CheckboxItem
							checked={isAssigned}
							onCheckedChange={() => {
								const currentIds = (issue.assignees ?? []).map(a => a.id);
								const newIds = isAssigned ? currentIds.filter(id => id !== member.user_id) : [...currentIds, member.user_id];
								updateField('assignee_ids', newIds);
							}}
						>
							<span class={rowClass}><CircleUser class={iconClass} />{member.name || member.email}</span>
						</ContextMenu.CheckboxItem>
					{/each}
				</ContextMenu.SubContent>
			</ContextMenu.Sub>
		{/if}

		{#if labels.length > 0}
			<ContextMenu.Sub>
				<ContextMenu.SubTrigger>
					<span class={rowClass}><Tag class={iconClass} />Labels</span>
				</ContextMenu.SubTrigger>
				<ContextMenu.SubContent class="w-48">
					{#each labels as label}
						{@const isSelected = (issue.labels ?? []).some(l => l.id === label.id)}
						<ContextMenu.CheckboxItem
							checked={isSelected}
							onCheckedChange={() => {
								const currentIds = (issue.labels ?? []).map(l => l.id);
								const newIds = isSelected ? currentIds.filter(id => id !== label.id) : [...currentIds, label.id];
								updateField('label_ids', newIds);
							}}
						>
							<span class={rowClass}>
								<span class="h-2.5 w-2.5 shrink-0 rounded-full" style="background-color: {label.color}"></span>
								{label.name}
							</span>
						</ContextMenu.CheckboxItem>
					{/each}
				</ContextMenu.SubContent>
			</ContextMenu.Sub>
		{/if}

		<ContextMenu.Sub>
			<ContextMenu.SubTrigger>
				<span class={rowClass}><CalendarDays class={iconClass} />Due date</span>
			</ContextMenu.SubTrigger>
			<ContextMenu.SubContent class="w-40">
				<ContextMenu.Item onclick={() => updateField('due_date', dateOffset(0))}><span class={rowClass}><CalendarDays class={iconClass} />Today</span></ContextMenu.Item>
				<ContextMenu.Item onclick={() => updateField('due_date', dateOffset(1))}><span class={rowClass}><CalendarDays class={iconClass} />Tomorrow</span></ContextMenu.Item>
				<ContextMenu.Item onclick={() => updateField('due_date', dateOffset(7))}><span class={rowClass}><CalendarDays class={iconClass} />Next week</span></ContextMenu.Item>
				<ContextMenu.Item onclick={() => updateField('due_date', dateOffset(14))}><span class={rowClass}><CalendarDays class={iconClass} />In 2 weeks</span></ContextMenu.Item>
				{#if issue.due_date}
					<ContextMenu.Separator />
					<ContextMenu.Item onclick={() => updateField('due_date', '')}><span class={rowClass}><X class={iconClass} />Clear</span></ContextMenu.Item>
				{/if}
			</ContextMenu.SubContent>
		</ContextMenu.Sub>

		{#if projects && projects.length > 0}
			<ContextMenu.Sub>
				<ContextMenu.SubTrigger><span class={rowClass}><FolderKanban class={iconClass} />Project</span></ContextMenu.SubTrigger>
				<ContextMenu.SubContent class="w-48">
					<ContextMenu.Item onclick={() => updateField('project_id', null)}><span class={rowClass}><X class={iconClass} />No project</span></ContextMenu.Item>
					{#each projects as project}
						<ContextMenu.Item onclick={() => updateField('project_id', project.id)}><span class={rowClass}><FolderKanban class={iconClass} />{project.name}</span></ContextMenu.Item>
					{/each}
				</ContextMenu.SubContent>
			</ContextMenu.Sub>
		{/if}

		{#if cycles && cycles.length > 0}
			<ContextMenu.Sub>
				<ContextMenu.SubTrigger><span class={rowClass}><RefreshCw class={iconClass} />Cycle</span></ContextMenu.SubTrigger>
				<ContextMenu.SubContent class="w-48">
					<ContextMenu.Item onclick={() => updateField('cycle_id', null)}><span class={rowClass}><X class={iconClass} />No cycle</span></ContextMenu.Item>
					{#each cycles as cycle}
						<ContextMenu.Item onclick={() => updateField('cycle_id', cycle.id)}><span class={rowClass}><RefreshCw class={iconClass} />{cycle.name}</span></ContextMenu.Item>
					{/each}
				</ContextMenu.SubContent>
			</ContextMenu.Sub>
		{/if}

		<ContextMenu.Sub>
			<ContextMenu.SubTrigger><span class={rowClass}><CircleDot class={iconClass} />Mark as</span></ContextMenu.SubTrigger>
			<ContextMenu.SubContent class="w-52">
				<ContextMenu.Item onclick={() => onaddrelation?.('blocking')}><span class={rowClass}><GitBranch class={iconClass} />Blocking...</span></ContextMenu.Item>
				<ContextMenu.Item onclick={() => onaddrelation?.('blocked_by')}><span class={rowClass}><GitBranch class={iconClass} />Blocked by...</span></ContextMenu.Item>
				<ContextMenu.Item onclick={() => onaddrelation?.('related')}><span class={rowClass}><LinkIcon class={iconClass} />Related issue...</span></ContextMenu.Item>
				<ContextMenu.Item onclick={() => onaddrelation?.('duplicate')}><span class={rowClass}><Copy class={iconClass} />Duplicate of...</span></ContextMenu.Item>
				<ContextMenu.Separator />
				<ContextMenu.Item onclick={() => openPicker('sub_issue_of')}><span class={rowClass}><CornerDownRight class={iconClass} />Sub-issue of...</span></ContextMenu.Item>
				<ContextMenu.Item onclick={() => openPicker('parent_of')}><span class={rowClass}><CornerDownRight class={iconClass} />Parent of...</span></ContextMenu.Item>
				{#if issue.parent_id}
					<ContextMenu.Item onclick={() => (removeParentOpen = true)}><span class={rowClass}><X class={iconClass} />Remove parent...</span></ContextMenu.Item>
				{/if}
			</ContextMenu.SubContent>
		</ContextMenu.Sub>

		<ContextMenu.Separator />

		<ContextMenu.Item onclick={() => copyToClipboard(issue.identifier)}><span class={rowClass}><Copy class={iconClass} />Copy identifier</span></ContextMenu.Item>
		<ContextMenu.Item onclick={() => copyToClipboard(`${window.location.origin}/${slug}/issue/${issue.identifier}`)}><span class={rowClass}><LinkIcon class={iconClass} />Copy link</span></ContextMenu.Item>
		<ContextMenu.Item onclick={() => window.open(`/${slug}/issue/${issue.identifier}`, '_blank')}><span class={rowClass}><ExternalLink class={iconClass} />Open in new tab</span></ContextMenu.Item>
		<ContextMenu.Item onclick={() => { includeSubIssues = false; duplicateOpen = true; }}><span class={rowClass}><CopyPlus class={iconClass} />Duplicate issue...</span></ContextMenu.Item>
		<ContextMenu.Item onclick={() => (convertOpen = true)}><span class={rowClass}><FolderKanban class={iconClass} />Convert to project...</span></ContextMenu.Item>

		<ContextMenu.Separator />

		<ContextMenu.Item class="text-red-500 focus:text-red-500" onclick={() => (deleteOpen = true)}>
			<span class="flex w-full items-center justify-between gap-2">
				<span>Delete</span>
				<Trash2 class="h-4 w-4 shrink-0" />
			</span>
		</ContextMenu.Item>
	</ContextMenu.Content>
</ContextMenu.Root>

<IssuePickerDialog
	bind:open={pickerOpen}
	{slug}
	title={pickerTitle}
	description={pickerDescription}
	actionLabel={pickerActionLabel}
	excludeIds={[issue.id]}
	onselect={handlePickedIssue}
/>

<AlertDialog.Root bind:open={deleteOpen}>
	<AlertDialog.Content>
		<AlertDialog.Header>
			<AlertDialog.Title>Delete {issue.identifier}?</AlertDialog.Title>
			<AlertDialog.Description>This action cannot be undone.</AlertDialog.Description>
		</AlertDialog.Header>
		<AlertDialog.Footer>
			<AlertDialog.Cancel variant="outline">Cancel</AlertDialog.Cancel>
			<AlertDialog.Action variant="destructive" onclick={handleDelete}>Delete</AlertDialog.Action>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>

<AlertDialog.Root bind:open={removeParentOpen}>
	<AlertDialog.Content>
		<AlertDialog.Header>
			<AlertDialog.Title>Remove parent from {issue.identifier}?</AlertDialog.Title>
			<AlertDialog.Description>This will turn the issue back into a regular top-level issue.</AlertDialog.Description>
		</AlertDialog.Header>
		<AlertDialog.Footer>
			<AlertDialog.Cancel variant="outline">Cancel</AlertDialog.Cancel>
			<AlertDialog.Action variant="destructive" onclick={handleRemoveParent}>Remove parent</AlertDialog.Action>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>

<AlertDialog.Root bind:open={duplicateOpen}>
	<AlertDialog.Content>
		<AlertDialog.Header>
			<AlertDialog.Title>Duplicate {issue.identifier}?</AlertDialog.Title>
			<AlertDialog.Description>Create a copy of this issue with a new identifier.</AlertDialog.Description>
		</AlertDialog.Header>
		{#if (issue.sub_issue_count ?? 0) > 0}
			<label class="flex items-center gap-2 rounded-md border border-[var(--app-border)] p-2 text-sm text-[var(--color-text-secondary)]">
				<input type="checkbox" bind:checked={includeSubIssues} class="h-4 w-4 accent-[var(--app-accent)]" />
				Include sub-issues
			</label>
		{/if}
		<AlertDialog.Footer>
			<AlertDialog.Cancel variant="outline">Cancel</AlertDialog.Cancel>
			<AlertDialog.Action onclick={handleDuplicate}>Duplicate</AlertDialog.Action>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>

<AlertDialog.Root bind:open={convertOpen}>
	<AlertDialog.Content>
		<AlertDialog.Header>
			<AlertDialog.Title>Convert {issue.identifier} to project?</AlertDialog.Title>
			<AlertDialog.Description>This will add the issue and its direct sub-issues to a new project and remove those sub-issue links.</AlertDialog.Description>
		</AlertDialog.Header>
		<AlertDialog.Footer>
			<AlertDialog.Cancel variant="outline">Cancel</AlertDialog.Cancel>
			<AlertDialog.Action onclick={handleConvertToProject}>Convert</AlertDialog.Action>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>
