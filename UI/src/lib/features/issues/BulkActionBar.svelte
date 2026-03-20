<script lang="ts">
	import type { IssueStatus, IssuePriority } from '$lib/types/issue';
	import { STATUS_LABELS, PRIORITY_LABELS, STATUS_ORDER } from '$lib/types/issue';
	import IssueStatusIcon from './IssueStatusIcon.svelte';
	import IssuePriorityIcon from './IssuePriorityIcon.svelte';
	import { issuesState } from './issues.state.svelte';
	import * as Popover from '$lib/components/ui/popover';
	import { toast } from 'svelte-sonner';
	import { X } from 'lucide-svelte';

	let { slug }: { slug: string } = $props();

	let statusOpen = $state(false);
	let priorityOpen = $state(false);

	const priorityValues: IssuePriority[] = [0, 1, 2, 3, 4];

	async function bulkSetStatus(status: IssueStatus) {
		try {
			await issuesState.bulkUpdate(slug, { status });
			toast.success(`Updated ${issuesState.selectionCount} issues`);
		} catch {
			toast.error('Bulk update failed');
		}
		statusOpen = false;
	}

	async function bulkSetPriority(priority: IssuePriority) {
		try {
			await issuesState.bulkUpdate(slug, { priority });
			toast.success(`Updated ${issuesState.selectionCount} issues`);
		} catch {
			toast.error('Bulk update failed');
		}
		priorityOpen = false;
	}
</script>

{#if issuesState.selectionCount > 0}
	<div class="sticky bottom-4 mx-4 flex items-center gap-3 rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-4 py-2.5 shadow-lg">
		<span class="text-sm font-medium text-[var(--color-text-primary)]">
			{issuesState.selectionCount} selected
		</span>

		<div class="flex items-center gap-1">
			<Popover.Root bind:open={statusOpen}>
				<Popover.Trigger>
					<button class="rounded-md border border-[var(--app-border)] px-2.5 py-1 text-xs text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]">
						Status
					</button>
				</Popover.Trigger>
				<Popover.Content class="w-40 p-1" align="start">
					{#each STATUS_ORDER as value}
						<button
							onclick={() => bulkSetStatus(value)}
							class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)]"
						>
							<IssueStatusIcon status={value} size={14} />
							{STATUS_LABELS[value]}
						</button>
					{/each}
				</Popover.Content>
			</Popover.Root>

			<Popover.Root bind:open={priorityOpen}>
				<Popover.Trigger>
					<button class="rounded-md border border-[var(--app-border)] px-2.5 py-1 text-xs text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]">
						Priority
					</button>
				</Popover.Trigger>
				<Popover.Content class="w-40 p-1" align="start">
					{#each priorityValues as value}
						<button
							onclick={() => bulkSetPriority(value)}
							class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)]"
						>
							<IssuePriorityIcon priority={value} size={14} />
							{PRIORITY_LABELS[value]}
						</button>
					{/each}
				</Popover.Content>
			</Popover.Root>
		</div>

		<button
			onclick={() => issuesState.clearSelection()}
			class="ml-auto rounded p-1 text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]"
		>
			<X size={16} />
		</button>
	</div>
{/if}
