<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { page } from '$app/state';
	import { listIssues, triageAccept, triageDecline } from '$lib/api/issues';
	import type { Issue } from '$lib/types/issue';
	import type { PaginatedResponse } from '$lib/types/common';
	import IssueStatusIcon from '$lib/features/issues/IssueStatusIcon.svelte';
	import IssuePriorityIcon from '$lib/features/issues/IssuePriorityIcon.svelte';
	import EmptyState from '$lib/components/shared/EmptyState.svelte';
	import { Badge } from '$lib/components/ui/badge';
	import { Button } from '$lib/components/ui/button';
	import { Kbd } from '$lib/components/ui/kbd';
	import { toast } from 'svelte-sonner';
	import { formatRelativeTime } from '$lib/utils/format';
	import { CheckCircle2, XCircle, ArrowLeft } from 'lucide-svelte';
	import SidebarToggle from '$lib/components/layout/SidebarToggle.svelte';

	const slug = $derived(page.params.workspaceSlug ?? '');
	const teamId = $derived(page.params.teamId ?? '');

	let issues = $state<Issue[]>([]);
	let loading = $state(true);
	let selectedIndex = $state(0);

	let selectedIssue = $derived(issues.length > 0 && selectedIndex >= 0 ? issues[selectedIndex] : null);

	onMount(async () => {
		await loadIssues();
	});

	async function loadIssues() {
		loading = true;
		try {
			const res: PaginatedResponse<Issue> = await listIssues(slug, {
				team: teamId,
				triaged: 'false',
				per_page: '100'
			});
			issues = res.data;
			selectedIndex = 0;
		} finally {
			loading = false;
		}
	}

	async function handleAccept(identifier: string) {
		try {
			await triageAccept(slug, identifier);
			issues = issues.filter((i) => i.identifier !== identifier);
			selectedIndex = Math.min(selectedIndex, issues.length - 1);
			toast.success('Issue accepted');
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to accept');
		}
	}

	async function handleDecline(identifier: string) {
		try {
			await triageDecline(slug, identifier);
			issues = issues.filter((i) => i.identifier !== identifier);
			selectedIndex = Math.min(selectedIndex, issues.length - 1);
			toast.success('Issue declined');
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to decline');
		}
	}

	function handleKeydown(e: KeyboardEvent) {
		const target = e.target as HTMLElement;
		if (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA') return;

		switch (e.key) {
			case 'j':
				e.preventDefault();
				selectedIndex = Math.min(selectedIndex + 1, issues.length - 1);
				break;
			case 'k':
				e.preventDefault();
				selectedIndex = Math.max(selectedIndex - 1, 0);
				break;
			case '1':
				e.preventDefault();
				if (selectedIssue) handleAccept(selectedIssue.identifier);
				break;
			case '3':
				e.preventDefault();
				if (selectedIssue) handleDecline(selectedIssue.identifier);
				break;
		}
	}

	onMount(() => {
		document.addEventListener('keydown', handleKeydown);
		return () => document.removeEventListener('keydown', handleKeydown);
	});
</script>

<div class="flex h-full">
	<!-- Issue list -->
	<div class="flex w-80 shrink-0 flex-col border-r border-[var(--app-border)]">
		<div class="flex h-[49px] items-center gap-3 border-b border-[var(--app-border)] px-4">
			<SidebarToggle />
			<a
				href="/{slug}/teams/{teamId}"
				class="text-[var(--color-text-tertiary)] hover:text-[var(--color-text-secondary)]"
			>
				<ArrowLeft size={16} />
			</a>
			<h1 class="text-sm font-medium text-[var(--color-text-primary)]">Triage</h1>
			<Badge variant="outline" class="text-[10px]">{issues.length}</Badge>
		</div>

		<div class="flex-1 overflow-y-auto">
			{#if !loading && issues.length === 0}
				<EmptyState
					title="All triaged"
					description="No issues waiting for triage"
				/>
			{:else}
				{#each issues as issue, i}
					<button
						onclick={() => (selectedIndex = i)}
						class="flex w-full flex-col gap-1 border-b border-[var(--app-border)] px-4 py-3 text-left {selectedIndex === i ? 'bg-[var(--color-bg-hover)]' : 'hover:bg-[var(--color-bg-hover)]'}"
					>
						<div class="flex items-center gap-2">
							<IssuePriorityIcon priority={issue.priority} size={13} />
							<span class="text-xs text-[var(--color-text-tertiary)]">{issue.identifier}</span>
							<span class="ml-auto text-xs text-[var(--color-text-tertiary)]">{formatRelativeTime(issue.created_at)}</span>
						</div>
						<p class="line-clamp-2 text-sm text-[var(--color-text-primary)]">{issue.title}</p>
					</button>
				{/each}
			{/if}
		</div>
	</div>

	<!-- Detail panel -->
	<div class="flex-1">
		{#if selectedIssue}
			<div class="flex h-[49px] items-center justify-between border-b border-[var(--app-border)] px-6">
				<div class="flex items-center gap-2">
					<span class="text-sm text-[var(--color-text-tertiary)]">{selectedIssue.identifier}</span>
					<IssueStatusIcon status={selectedIssue.status} />
				</div>
				<div class="flex items-center gap-2">
					<Button size="sm" onclick={() => handleAccept(selectedIssue!.identifier)} class="gap-1.5">
						<CheckCircle2 size={14} />
						Accept
						<Kbd class="ml-1">1</Kbd>
					</Button>
					<Button variant="outline" size="sm" onclick={() => handleDecline(selectedIssue!.identifier)} class="gap-1.5">
						<XCircle size={14} />
						Decline
						<Kbd class="ml-1">3</Kbd>
					</Button>
				</div>
			</div>

			<div class="p-6">
				<h2 class="text-lg font-semibold text-[var(--color-text-primary)]">{selectedIssue.title}</h2>
				{#if selectedIssue.description}
					<p class="mt-3 whitespace-pre-wrap text-sm text-[var(--color-text-secondary)]">
						{selectedIssue.description}
					</p>
				{/if}

				<div class="mt-6 grid grid-cols-2 gap-3 text-sm">
					<div class="flex items-center gap-2">
						<span class="w-20 text-[var(--color-text-tertiary)]">Status</span>
						<div class="flex items-center gap-1.5 text-[var(--color-text-secondary)]">
							<IssueStatusIcon status={selectedIssue.status} />
							{selectedIssue.status.replace('_', ' ')}
						</div>
					</div>
					<div class="flex items-center gap-2">
						<span class="w-20 text-[var(--color-text-tertiary)]">Priority</span>
						<div class="flex items-center gap-1.5 text-[var(--color-text-secondary)]">
							<IssuePriorityIcon priority={selectedIssue.priority} />
							{selectedIssue.priority}
						</div>
					</div>
				</div>

				<!-- Keyboard hint -->
				<div class="mt-8 flex items-center gap-4 rounded-md border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-4 py-3">
					<span class="text-xs text-[var(--color-text-tertiary)]">Keyboard shortcuts:</span>
					<div class="flex items-center gap-1 text-xs text-[var(--color-text-secondary)]">
						<Kbd>1</Kbd> Accept
					</div>
					<div class="flex items-center gap-1 text-xs text-[var(--color-text-secondary)]">
						<Kbd>3</Kbd> Decline
					</div>
					<div class="flex items-center gap-1 text-xs text-[var(--color-text-secondary)]">
						<Kbd>J</Kbd><Kbd>K</Kbd> Navigate
					</div>
				</div>
			</div>
		{:else if !loading && issues.length === 0}
			<div class="flex h-full items-center justify-center">
				<div class="text-center">
					<CheckCircle2 size={40} class="mx-auto text-[var(--color-success)]" />
					<p class="mt-3 text-sm font-medium text-[var(--color-text-primary)]">All caught up!</p>
					<p class="mt-1 text-xs text-[var(--color-text-tertiary)]">No issues waiting for triage</p>
				</div>
			</div>
		{/if}
	</div>
</div>
