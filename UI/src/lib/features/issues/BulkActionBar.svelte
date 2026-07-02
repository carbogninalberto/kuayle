<script lang="ts">
	import type { IssuePriority } from '$lib/types/issue';
	import { teamStatusesState } from './team-statuses.state.svelte';
	import { issuesState } from './issues.state.svelte';
	import * as AlertDialog from '$lib/components/ui/alert-dialog';
	import { StatusSelector, PrioritySelector } from './selectors';
	import { toast } from 'svelte-sonner';
	import { X, Trash2 } from 'lucide-svelte';
	import * as issueApi from '$lib/api/issues';
	import { onMount } from 'svelte';

	let { slug }: { slug: string } = $props();

	let statusOpen = $state(false);
	let priorityOpen = $state(false);
	let deleteOpen = $state(false);

	onMount(() => {
		const onRequestDelete = () => {
			if (issuesState.selectionCount > 0) {
				deleteOpen = true;
			}
		};
		window.addEventListener('issues:bulk-delete-request', onRequestDelete);
		return () => window.removeEventListener('issues:bulk-delete-request', onRequestDelete);
	});

	async function bulkSetStatus(statusId: string) {
		const count = issuesState.selectionCount;
		try {
			await issuesState.bulkUpdate(slug, { status_id: statusId } as any);
			toast.success(`Updated ${count} issue${count > 1 ? 's' : ''}`);
		} catch {
			toast.error('Bulk update failed');
		}
		statusOpen = false;
	}

	async function bulkSetPriority(priority: IssuePriority) {
		const count = issuesState.selectionCount;
		try {
			await issuesState.bulkUpdate(slug, { priority });
			toast.success(`Updated ${count} issue${count > 1 ? 's' : ''}`);
		} catch {
			toast.error('Bulk update failed');
		}
		priorityOpen = false;
	}

	async function bulkDelete() {
		const ids = Array.from(issuesState.selectedIds);
		if (ids.length === 0) return;
		const idsToDelete = new Set(ids);
		try {
			await issueApi.bulkDeleteIssues(slug, { issue_ids: ids });
			issuesState.issues = issuesState.issues.filter((i) => !idsToDelete.has(i.id));
			issuesState.totalCount -= ids.length;
			issuesState.clearSelection();
			toast.success(`Deleted ${ids.length} issue${ids.length > 1 ? 's' : ''}`);
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to delete issues');
		}
		deleteOpen = false;
	}
</script>

{#if issuesState.selectionCount > 0}
	<div class="fixed bottom-6 left-1/2 z-40 flex -translate-x-1/2 items-center gap-2 rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-3 py-1.5 shadow-lg">
		<span class="text-xs font-medium text-[var(--color-text-primary)]">
			{issuesState.selectionCount} selected
		</span>

		<div class="flex items-center gap-1">
			<StatusSelector
				bind:open={statusOpen}
				statuses={teamStatusesState.statusOrder}
				value={undefined}
				onchange={(id) => bulkSetStatus(id)}
			>
				{#snippet trigger()}
					<button class="rounded-md border border-[var(--app-border)] px-2.5 py-1 text-xs text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]">
						Status
					</button>
				{/snippet}
			</StatusSelector>

			<PrioritySelector
				bind:open={priorityOpen}
				value={0 as import('$lib/types/issue').IssuePriority}
				onchange={(p) => bulkSetPriority(p)}
			>
				{#snippet trigger()}
					<button class="rounded-md border border-[var(--app-border)] px-2.5 py-1 text-xs text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]">
						Priority
					</button>
				{/snippet}
			</PrioritySelector>
		</div>

		<button
			onclick={() => (deleteOpen = true)}
			class="rounded-md border border-red-500/30 px-2.5 py-1 text-xs text-red-500 hover:bg-red-500/10"
			title="Delete selected issues"
		>
			<Trash2 size={12} />
		</button>

		<button
			onclick={() => issuesState.clearSelection()}
			class="ml-auto rounded p-1 text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]"
		>
			<X size={16} />
		</button>
	</div>

	<AlertDialog.Root bind:open={deleteOpen}>
		<AlertDialog.Content>
			<AlertDialog.Header>
				<AlertDialog.Title>Delete {issuesState.selectionCount} issue{issuesState.selectionCount > 1 ? 's' : ''}?</AlertDialog.Title>
				<AlertDialog.Description>This action cannot be undone.</AlertDialog.Description>
			</AlertDialog.Header>
			<AlertDialog.Footer>
				<AlertDialog.Cancel
					variant="outline"
					class="border-[var(--app-border)] text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]"
				>
					Cancel
				</AlertDialog.Cancel>
				<AlertDialog.Action
					variant="destructive"
					class="bg-red-600 text-white hover:bg-red-700"
					onclick={bulkDelete}
				>
					Delete
				</AlertDialog.Action>
			</AlertDialog.Footer>
		</AlertDialog.Content>
	</AlertDialog.Root>
{/if}
