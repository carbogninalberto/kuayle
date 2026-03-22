<script lang="ts">
	import type { Issue, RelationType } from '$lib/types/issue';
	import type { PaginatedResponse } from '$lib/types/common';
	import { createRelation } from '$lib/api/issue-relations';
	import { listIssues } from '$lib/api/issues';
	import IssueStatusIcon from './IssueStatusIcon.svelte';
	import * as Dialog from '$lib/components/ui/dialog';
	import { toast } from 'svelte-sonner';

	let {
		open = $bindable(false),
		slug,
		identifier,
		defaultType = 'related',
		oncreated
	}: {
		open: boolean;
		slug: string;
		identifier: string;
		defaultType?: RelationType;
		oncreated?: () => void;
	} = $props();

	const RELATION_LABELS: Record<RelationType, string> = {
		related: 'Related to',
		blocked_by: 'Blocked by',
		blocking: 'Blocking',
		duplicate: 'Duplicate of'
	};

	const RELATION_TYPES: RelationType[] = ['related', 'blocked_by', 'blocking', 'duplicate'];

	// svelte-ignore state_referenced_locally
	let selectedType = $state<RelationType>(defaultType);
	let searchQuery = $state('');
	let searchResults = $state<Issue[]>([]);
	let searching = $state(false);
	let searchTimer: ReturnType<typeof setTimeout> | undefined;

	$effect(() => {
		if (open) {
			selectedType = defaultType;
			searchQuery = '';
			searchResults = [];
		}
	});

	function handleSearch(query: string) {
		searchQuery = query;
		clearTimeout(searchTimer);
		if (!query.trim()) {
			searchResults = [];
			return;
		}
		searchTimer = setTimeout(async () => {
			searching = true;
			try {
				const response: PaginatedResponse<Issue> = await listIssues(slug, { search: query, per_page: '10' });
				searchResults = response.data.filter((issue) => issue.identifier !== identifier);
			} catch {
				searchResults = [];
			} finally {
				searching = false;
			}
		}, 200);
	}

	async function handleAddRelation(relatedIdentifier: string) {
		try {
			await createRelation(slug, identifier, { related_identifier: relatedIdentifier, type: selectedType });
			toast.success('Relation added');
			open = false;
			oncreated?.();
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to add relation');
		}
	}
</script>

<Dialog.Root bind:open>
	<Dialog.Content class="sm:max-w-md">
		<Dialog.Header>
			<Dialog.Title>Add Relation</Dialog.Title>
			<Dialog.Description>Select a relation type and search for an issue.</Dialog.Description>
		</Dialog.Header>

		<div class="flex flex-col gap-3 py-4">
			<!-- Relation type pills -->
			<div class="flex gap-1">
				{#each RELATION_TYPES as type}
					<button
						onclick={() => (selectedType = type)}
						class="rounded px-2 py-0.5 text-[11px] transition-colors {selectedType === type
							? 'bg-[var(--app-accent)] text-[var(--app-accent-foreground)]'
							: 'text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)] bg-[var(--color-bg-secondary)]'}"
					>
						{RELATION_LABELS[type]}
					</button>
				{/each}
			</div>

			<!-- Search input -->
			<input
				type="text"
				placeholder="Search issues..."
				value={searchQuery}
				oninput={(e) => handleSearch((e.target as HTMLInputElement).value)}
				class="w-full rounded border border-[var(--app-border)] bg-[var(--color-bg)] px-3 py-2 text-sm text-[var(--color-text-primary)] outline-none focus:border-[var(--app-accent)] transition-colors"
			/>

			<!-- Search results -->
			{#if searchResults.length > 0}
				<div class="max-h-48 overflow-y-auto rounded border border-[var(--app-border)]">
					{#each searchResults as result}
						<button
							onclick={() => handleAddRelation(result.identifier)}
							class="flex w-full items-center gap-2 px-3 py-2 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] transition-colors"
						>
							<IssueStatusIcon status={result.status} size={13} />
							<span class="text-xs text-[var(--color-text-tertiary)]">{result.identifier}</span>
							<span class="truncate">{result.title}</span>
						</button>
					{/each}
				</div>
			{:else if searchQuery.trim() && !searching}
				<p class="text-xs text-[var(--color-text-tertiary)] text-center py-2">No issues found</p>
			{/if}
		</div>
	</Dialog.Content>
</Dialog.Root>
