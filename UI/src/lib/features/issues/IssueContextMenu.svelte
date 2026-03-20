<script lang="ts">
	import type { Issue, IssueStatus, IssuePriority } from '$lib/types/issue';
	import { STATUS_LABELS, PRIORITY_LABELS, STATUS_ORDER } from '$lib/types/issue';
	import type { WorkspaceMember } from '$lib/types/workspace';
	import type { Label } from '$lib/types/label';
	import * as ContextMenu from '$lib/components/ui/context-menu';
	import IssueStatusIcon from './IssueStatusIcon.svelte';
	import IssuePriorityIcon from './IssuePriorityIcon.svelte';
	import { issuesState } from './issues.state.svelte';
	import { toast } from 'svelte-sonner';
	import type { Snippet } from 'svelte';

	let {
		issue,
		slug,
		members = [],
		labels = [],
		children
	}: {
		issue: Issue;
		slug: string;
		members?: WorkspaceMember[];
		labels?: Label[];
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

	async function handleDelete() {
		try {
			await issuesState.remove(slug, issue.identifier);
			toast.success('Issue deleted');
		} catch {
			toast.error('Failed to delete issue');
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
					<IssueStatusIcon status={issue.status} size={14} />
					Status
				</span>
			</ContextMenu.SubTrigger>
			<ContextMenu.SubContent class="w-44">
				{#each STATUS_ORDER as value}
					<ContextMenu.Item onclick={() => updateField('status', value)}>
						<span class="flex items-center gap-2">
							<IssueStatusIcon status={value} size={14} />
							{STATUS_LABELS[value]}
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

		<!-- Assignee submenu -->
		{#if members.length > 0}
			<ContextMenu.Sub>
				<ContextMenu.SubTrigger>Assignee</ContextMenu.SubTrigger>
				<ContextMenu.SubContent class="w-48">
					<ContextMenu.Item onclick={() => updateField('assignee_id', null)}>
						Unassigned
					</ContextMenu.Item>
					{#each members as member}
						<ContextMenu.Item onclick={() => updateField('assignee_id', member.user_id)}>
							{member.name || member.email}
						</ContextMenu.Item>
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

		<ContextMenu.Separator />

		<ContextMenu.Item onclick={() => copyToClipboard(issue.identifier)}>
			Copy identifier
		</ContextMenu.Item>
		<ContextMenu.Item onclick={() => copyToClipboard(`${window.location.origin}/${slug}/issue/${issue.identifier}`)}>
			Copy link
		</ContextMenu.Item>

		<ContextMenu.Separator />

		<ContextMenu.Item class="text-red-500 focus:text-red-500" onclick={handleDelete}>
			Delete
		</ContextMenu.Item>
	</ContextMenu.Content>
</ContextMenu.Root>
