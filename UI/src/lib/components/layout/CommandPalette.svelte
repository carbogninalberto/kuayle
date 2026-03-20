<script lang="ts">
	import { goto } from '$app/navigation';
	import type { Team } from '$lib/types/team';
	import type { Issue } from '$lib/types/issue';
	import { listIssues } from '$lib/api/issues';
	import IssueStatusIcon from '$lib/features/issues/IssueStatusIcon.svelte';
	import IssuePriorityIcon from '$lib/features/issues/IssuePriorityIcon.svelte';
	import { LoaderCircle } from 'lucide-svelte';

	let { slug, teams, onclose }: { slug: string; teams: Team[]; onclose: () => void } = $props();
	let search = $state('');
	let selectedIndex = $state(0);
	let issueResults = $state<Issue[]>([]);
	let issueLoading = $state(false);
	let debounceTimer: ReturnType<typeof setTimeout> | undefined;

	interface CommandItem {
		label: string;
		description?: string;
		action: () => void;
	}

	const commands: CommandItem[] = $derived.by(() => {
		const items: CommandItem[] = [
			{ label: 'Go to Inbox', action: () => navigate(`/${slug}/inbox`) },
			{ label: 'Go to My Issues', action: () => navigate(`/${slug}/my-issues`) },
			{ label: 'Go to Dashboard', action: () => navigate(`/${slug}/dashboard`) },
			{ label: 'Go to Projects', action: () => navigate(`/${slug}/projects`) },
			{ label: 'Go to Settings', action: () => navigate(`/${slug}/settings`) },
			...teams.map((t) => ({
				label: `Go to ${t.name}`,
				description: t.key,
				action: () => navigate(`/${slug}/teams/${t.id}`)
			}))
		];

		if (!search) return items;
		return items.filter((i) => i.label.toLowerCase().includes(search.toLowerCase()));
	});

	const totalItems = $derived(commands.length + issueResults.length);

	$effect(() => {
		if (search.length >= 2) {
			clearTimeout(debounceTimer);
			debounceTimer = setTimeout(async () => {
				issueLoading = true;
				try {
					const res = await listIssues(slug, { search, per_page: '10' });
					issueResults = res.data;
				} catch {
					issueResults = [];
				} finally {
					issueLoading = false;
				}
			}, 300);
		} else {
			issueResults = [];
			issueLoading = false;
		}
		selectedIndex = 0;
	});

	function navigate(path: string) {
		goto(path);
		onclose();
	}

	function handleKeydown(e: KeyboardEvent) {
		if (e.key === 'Escape') {
			onclose();
		} else if (e.key === 'ArrowDown') {
			e.preventDefault();
			selectedIndex = Math.min(selectedIndex + 1, totalItems - 1);
		} else if (e.key === 'ArrowUp') {
			e.preventDefault();
			selectedIndex = Math.max(selectedIndex - 1, 0);
		} else if (e.key === 'Enter') {
			e.preventDefault();
			if (selectedIndex < commands.length) {
				commands[selectedIndex]?.action();
			} else {
				const issueIdx = selectedIndex - commands.length;
				const issue = issueResults[issueIdx];
				if (issue) navigate(`/${slug}/issue/${issue.identifier}`);
			}
		}
	}
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
	class="fixed inset-0 z-50 flex items-start justify-center pt-[20vh]"
	onkeydown={handleKeydown}
>
	<!-- Backdrop -->
	<!-- svelte-ignore a11y_no_static_element_interactions a11y_click_events_have_key_events -->
	<div class="fixed inset-0 bg-black/50" onclick={onclose}></div>

	<!-- Dialog -->
	<div
		class="relative z-10 w-full max-w-lg overflow-hidden rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)] shadow-2xl"
	>
		<input
			type="text"
			bind:value={search}
			placeholder="Type a command or search..."
			autofocus
			class="w-full border-b border-[var(--app-border)] bg-transparent px-4 py-3 text-sm text-[var(--color-text-primary)] outline-none placeholder:text-[var(--color-text-tertiary)]"
		/>
		<div class="max-h-72 overflow-y-auto py-2">
			{#if commands.length > 0}
				<div class="px-3 py-1">
					<span class="text-[10px] font-medium uppercase text-[var(--color-text-tertiary)]">Commands</span>
				</div>
				{#each commands as cmd, i}
					<button
						class="flex w-full items-center gap-3 px-4 py-2 text-left text-sm {i === selectedIndex
							? 'bg-[var(--color-bg-hover)] text-[var(--color-text-primary)]'
							: 'text-[var(--color-text-secondary)]'}"
						onmouseenter={() => (selectedIndex = i)}
						onclick={() => cmd.action()}
					>
						<span>{cmd.label}</span>
						{#if cmd.description}
							<span class="text-xs text-[var(--color-text-tertiary)]">{cmd.description}</span>
						{/if}
					</button>
				{/each}
			{/if}

			{#if search.length >= 2}
				<div class="px-3 py-1 {commands.length > 0 ? 'mt-1 border-t border-[var(--app-border)] pt-2' : ''}">
					<span class="text-[10px] font-medium uppercase text-[var(--color-text-tertiary)]">Issues</span>
				</div>
				{#if issueLoading}
					<div class="flex items-center justify-center py-4">
						<LoaderCircle size={16} class="animate-spin text-[var(--color-text-tertiary)]" />
					</div>
				{:else if issueResults.length > 0}
					{#each issueResults as issue, i}
						{@const idx = commands.length + i}
						<button
							class="flex w-full items-center gap-2 px-4 py-2 text-left text-sm {idx === selectedIndex
								? 'bg-[var(--color-bg-hover)] text-[var(--color-text-primary)]'
								: 'text-[var(--color-text-secondary)]'}"
							onmouseenter={() => (selectedIndex = idx)}
							onclick={() => navigate(`/${slug}/issue/${issue.identifier}`)}
						>
							<IssuePriorityIcon priority={issue.priority} size={14} />
							<IssueStatusIcon status={issue.status} size={14} />
							<span class="shrink-0 text-xs text-[var(--color-text-tertiary)]">{issue.identifier}</span>
							<span class="truncate">{issue.title}</span>
						</button>
					{/each}
				{:else}
					<p class="px-4 py-2 text-sm text-[var(--color-text-tertiary)]">No issues found</p>
				{/if}
			{/if}

			{#if commands.length === 0 && issueResults.length === 0 && !issueLoading}
				<p class="px-4 py-2 text-sm text-[var(--color-text-tertiary)]">No results found</p>
			{/if}
		</div>
	</div>
</div>
