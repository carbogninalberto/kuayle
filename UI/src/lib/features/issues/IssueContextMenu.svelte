<script lang="ts">
	import type { Issue, IssuePriority, RelationType } from '$lib/types/issue';
	import { PRIORITY_LABELS } from '$lib/types/issue';
	import { teamStatusesState } from './team-statuses.state.svelte';
	import type { WorkspaceMember } from '$lib/types/workspace';
	import type { Label } from '$lib/types/label';
	import type { Project } from '$lib/types/project';
	import type { Cycle } from '$lib/types/cycle';
	import * as ContextMenu from '$lib/components/ui/context-menu';
	import IssueStatusIcon from './IssueStatusIcon.svelte';
	import IssuePriorityIcon from './IssuePriorityIcon.svelte';
	import { issuesState } from './issues.state.svelte';
	import { getIssue } from '$lib/api/issues';
	import { showIssueDeletedToast } from './issue-deleted-toast';
	import { toast } from 'svelte-sonner';
	import type { Snippet } from 'svelte';

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

	const priorityValues: IssuePriority[] = [0, 1, 2, 3, 4];

	async function updateField(field: string, value: any) {
		try {
			await issuesState.update(slug, issue.identifier, { [field]: value });
		} catch {
			toast.error(`Failed to update ${field}`);
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

	async function handleDelete() {
		try {
			await issuesState.remove(slug, issue.identifier);
			showIssueDeletedToast(issue);
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to delete issue');
		}
	}

	async function handleDuplicate() {
		try {
			await issuesState.create(slug, {
				title: `${issue.title} (copy)`,
				status: issue.status,
				priority: issue.priority,
				team_id: issue.team_id,
				project_id: issue.project_id ?? undefined,
				cycle_id: issue.cycle_id ?? undefined,
			});
			toast.success('Issue duplicated');
		} catch {
			toast.error('Failed to duplicate issue');
		}
	}

	async function handleSetParent() {
		const identifier = window.prompt('Parent issue identifier');
		if (!identifier?.trim()) return;
		try {
			const parent = await getIssue(slug, identifier.trim().toUpperCase());
			await issuesState.update(slug, issue.identifier, { parent_id: parent.id });
			toast.success(`Set parent to ${parent.identifier}`);
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to set parent');
		}
	}

	async function handleRemoveParent() {
		try {
			await issuesState.update(slug, issue.identifier, { parent_id: '' });
			toast.success('Removed parent');
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to remove parent');
		}
	}
</script>

<ContextMenu.Root>
	<ContextMenu.Trigger>
		{@render children()}
	</ContextMenu.Trigger>
	<ContextMenu.Content class="w-56">
		<!-- Status submenu -->
		<ContextMenu.Sub>
			<ContextMenu.SubTrigger>
				<span class="flex items-center gap-2">
					<IssueStatusIcon status={issue.status} category={issue.status_info?.category} color={issue.status_info?.color} size={14} />
					Status
				</span>
			</ContextMenu.SubTrigger>
			<ContextMenu.SubContent class="w-44">
				{#each teamStatusesState.statusOrder as ts}
					<ContextMenu.Item onclick={() => updateField('status_id', ts.id)}>
						<span class="flex items-center gap-2">
							<IssueStatusIcon category={ts.category} color={ts.color} size={14} />
							{ts.name}
						</span>
					</ContextMenu.Item>
				{/each}
			</ContextMenu.SubContent>
		</ContextMenu.Sub>

		<!-- Priority submenu -->
		<ContextMenu.Sub>
			<ContextMenu.SubTrigger>
				<span class="flex items-center gap-2">
					<IssuePriorityIcon priority={issue.priority} size={14} />
					Priority
				</span>
			</ContextMenu.SubTrigger>
			<ContextMenu.SubContent class="w-44">
				{#each priorityValues as value}
					<ContextMenu.Item onclick={() => updateField('priority', value)}>
						<span class="flex items-center gap-2">
							<IssuePriorityIcon priority={value} size={14} />
							{PRIORITY_LABELS[value]}
						</span>
					</ContextMenu.Item>
				{/each}
			</ContextMenu.SubContent>
		</ContextMenu.Sub>

		<!-- Assignee submenu (multi-select) -->
		{#if members.length > 0}
			<ContextMenu.Sub>
				<ContextMenu.SubTrigger>Assignee</ContextMenu.SubTrigger>
				<ContextMenu.SubContent class="w-48">
					<ContextMenu.Item onclick={() => updateField('assignee_ids', [])}>
						Clear all
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
							{member.name || member.email}
						</ContextMenu.CheckboxItem>
					{/each}
				</ContextMenu.SubContent>
			</ContextMenu.Sub>
		{/if}

		<!-- Labels submenu -->
		{#if labels.length > 0}
			<ContextMenu.Sub>
				<ContextMenu.SubTrigger>Labels</ContextMenu.SubTrigger>
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
							<span class="flex items-center gap-2">
								<div class="h-2.5 w-2.5 rounded-full shrink-0" style="background-color: {label.color}"></div>
								{label.name}
							</span>
						</ContextMenu.CheckboxItem>
					{/each}
				</ContextMenu.SubContent>
			</ContextMenu.Sub>
		{/if}

		<!-- Due date submenu -->
		<ContextMenu.Sub>
			<ContextMenu.SubTrigger>Due date</ContextMenu.SubTrigger>
			<ContextMenu.SubContent class="w-36">
				<ContextMenu.Item onclick={() => updateField('due_date', dateOffset(0))}>Today</ContextMenu.Item>
				<ContextMenu.Item onclick={() => updateField('due_date', dateOffset(1))}>Tomorrow</ContextMenu.Item>
				<ContextMenu.Item onclick={() => updateField('due_date', dateOffset(7))}>Next week</ContextMenu.Item>
				<ContextMenu.Item onclick={() => updateField('due_date', dateOffset(14))}>In 2 weeks</ContextMenu.Item>
				{#if issue.due_date}
					<ContextMenu.Separator />
					<ContextMenu.Item onclick={() => updateField('due_date', '')}>Clear</ContextMenu.Item>
				{/if}
			</ContextMenu.SubContent>
		</ContextMenu.Sub>

		<!-- Project submenu -->
		{#if projects && projects.length > 0}
			<ContextMenu.Sub>
				<ContextMenu.SubTrigger>Project</ContextMenu.SubTrigger>
				<ContextMenu.SubContent class="w-48">
					<ContextMenu.Item onclick={() => updateField('project_id', null)}>No project</ContextMenu.Item>
					{#each projects as project}
						<ContextMenu.Item onclick={() => updateField('project_id', project.id)}>
							{project.name}
						</ContextMenu.Item>
					{/each}
				</ContextMenu.SubContent>
			</ContextMenu.Sub>
		{/if}

		<!-- Cycle submenu -->
		{#if cycles && cycles.length > 0}
			<ContextMenu.Sub>
				<ContextMenu.SubTrigger>Cycle</ContextMenu.SubTrigger>
				<ContextMenu.SubContent class="w-48">
					<ContextMenu.Item onclick={() => updateField('cycle_id', null)}>No cycle</ContextMenu.Item>
					{#each cycles as cycle}
						<ContextMenu.Item onclick={() => updateField('cycle_id', cycle.id)}>
							{cycle.name}
						</ContextMenu.Item>
					{/each}
				</ContextMenu.SubContent>
			</ContextMenu.Sub>
		{/if}

		<!-- Relation submenu -->
		<ContextMenu.Sub>
			<ContextMenu.SubTrigger>Relation</ContextMenu.SubTrigger>
			<ContextMenu.SubContent class="w-48">
				<ContextMenu.Item onclick={() => onaddrelation?.('blocking')}>
					Mark as blocking...
				</ContextMenu.Item>
				<ContextMenu.Item onclick={() => onaddrelation?.('blocked_by')}>
					Mark as blocked by...
				</ContextMenu.Item>
				<ContextMenu.Item onclick={() => onaddrelation?.('related')}>
					Related issue...
				</ContextMenu.Item>
				<ContextMenu.Item onclick={() => onaddrelation?.('duplicate')}>
					Duplicate of...
				</ContextMenu.Item>
			</ContextMenu.SubContent>
		</ContextMenu.Sub>

		<ContextMenu.Separator />

		<ContextMenu.Item onclick={handleSetParent}>
			Set parent...
		</ContextMenu.Item>
		{#if issue.parent_id}
			<ContextMenu.Item onclick={handleRemoveParent}>
				Remove parent
			</ContextMenu.Item>
		{/if}

		<ContextMenu.Separator />

		<ContextMenu.Item onclick={() => copyToClipboard(issue.identifier)}>
			Copy identifier
		</ContextMenu.Item>
		<ContextMenu.Item onclick={() => copyToClipboard(`${window.location.origin}/${slug}/issue/${issue.identifier}`)}>
			Copy link
		</ContextMenu.Item>
		<ContextMenu.Item onclick={() => window.open(`/${slug}/issue/${issue.identifier}`, '_blank')}>
			Open in new tab
		</ContextMenu.Item>
		<ContextMenu.Item onclick={handleDuplicate}>
			Duplicate issue
		</ContextMenu.Item>

		<ContextMenu.Separator />

		<ContextMenu.Item class="text-red-500 focus:text-red-500" onclick={handleDelete}>
			Delete
		</ContextMenu.Item>
	</ContextMenu.Content>
</ContextMenu.Root>
