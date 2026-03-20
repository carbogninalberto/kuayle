<script lang="ts">
	import { onMount } from 'svelte';
	import type { Issue, IssueRelation, RelationType } from '$lib/types/issue';
	import type { PaginatedResponse } from '$lib/types/common';
	import { listRelations, createRelation, deleteRelation } from '$lib/api/issue-relations';
	import { listIssues } from '$lib/api/issues';
	import IssueStatusIcon from './IssueStatusIcon.svelte';
	import { Link, X, Plus, Ban, Copy, ArrowRight } from 'lucide-svelte';
	import { toast } from 'svelte-sonner';

	let { slug, identifier }: { slug: string; identifier: string } = $props();

	let relations = $state<IssueRelation[]>([]);
	let showAdd = $state(false);
	let selectedType = $state<RelationType>('related');
	let searchQuery = $state('');
	let searchResults = $state<Issue[]>([]);
	let searching = $state(false);
	let searchTimer: ReturnType<typeof setTimeout> | undefined;

	const RELATION_LABELS: Record<RelationType, string> = {
		related: 'Related to',
		blocked_by: 'Blocked by',
		blocking: 'Blocking',
		duplicate: 'Duplicate of'
	};

	const RELATION_TYPES: RelationType[] = ['related', 'blocked_by', 'blocking', 'duplicate'];

	let groupedRelations = $derived(
		RELATION_TYPES.map((type) => ({
			type,
			label: RELATION_LABELS[type],
			items: relations.filter((r) => r.type === type)
		})).filter((group) => group.items.length > 0)
	);

	onMount(async () => {
		relations = await listRelations(slug, identifier);
	});

	function handleSearch(query: string) {
		searchQuery = query;
		clearTimeout(searchTimer);
		if (!query.trim()) { searchResults = []; return; }
		searchTimer = setTimeout(async () => {
			searching = true;
			try {
				const response: PaginatedResponse<Issue> = await listIssues(slug, { search: query, per_page: '10' });
				searchResults = response.data.filter((issue) => issue.identifier !== identifier);
			} catch { searchResults = []; }
			finally { searching = false; }
		}, 200);
	}

	async function handleAddRelation(relatedIdentifier: string) {
		try {
			await createRelation(slug, identifier, { related_identifier: relatedIdentifier, type: selectedType });
			relations = await listRelations(slug, identifier);
			showAdd = false;
			searchQuery = '';
			searchResults = [];
			toast.success('Relation added');
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to add relation');
		}
	}

	async function handleRemoveRelation(relationId: string) {
		try {
			await deleteRelation(slug, identifier, relationId);
			relations = relations.filter((r) => r.id !== relationId);
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to remove relation');
		}
	}
</script>

{#if relations.length > 0 || showAdd}
	<div class="space-y-2">
		{#each groupedRelations as group}
			<div class="space-y-1">
				<span class="text-[11px] font-medium text-[var(--color-text-tertiary)]">{group.label}</span>
				{#each group.items as relation}
					<div class="flex items-center justify-between group rounded px-2 py-1 hover:bg-[var(--color-bg-hover)] transition-colors">
						<a
							href="/{slug}/issue/{relation.related_issue?.identifier ?? ''}"
							class="flex items-center gap-2 min-w-0"
						>
							{#if relation.related_issue}
								<IssueStatusIcon status={relation.related_issue.status} size={13} />
								<span class="text-xs text-[var(--color-text-tertiary)]">{relation.related_issue.identifier}</span>
								<span class="truncate text-xs text-[var(--color-text-secondary)]">{relation.related_issue.title}</span>
							{:else}
								<span class="text-xs text-[var(--color-text-tertiary)]">{relation.related_issue_id.slice(0, 8)}...</span>
							{/if}
						</a>
						<button
							onclick={() => handleRemoveRelation(relation.id)}
							class="shrink-0 ml-2 opacity-0 group-hover:opacity-100 text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)] transition-opacity"
						>
							<X size={12} />
						</button>
					</div>
				{/each}
			</div>
		{/each}

		{#if !showAdd}
			<button
				onclick={() => (showAdd = true)}
				class="flex items-center gap-1 text-xs text-[var(--color-text-tertiary)] hover:text-[var(--color-text-secondary)] transition-colors"
			>
				<Plus size={12} />
				Add relation
			</button>
		{/if}
	</div>
{/if}

{#if showAdd}
	<div class="mt-2 rounded-md border border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-2 animate-in slide-in-from-top-1 duration-150">
		<div class="flex gap-1 mb-2">
			{#each RELATION_TYPES as type}
				<button
					onclick={() => (selectedType = type)}
					class="rounded px-2 py-0.5 text-[11px] transition-colors {selectedType === type
						? 'bg-[var(--app-accent)] text-white'
						: 'text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)]'}"
				>
					{RELATION_LABELS[type]}
				</button>
			{/each}
		</div>
		<input
			type="text"
			placeholder="Search issues..."
			value={searchQuery}
			oninput={(e) => handleSearch((e.target as HTMLInputElement).value)}
			class="w-full rounded border border-[var(--app-border)] bg-[var(--color-bg)] px-2 py-1 text-xs text-[var(--color-text-primary)] outline-none focus:border-[var(--app-accent)] transition-colors"
		/>
		{#if searchResults.length > 0}
			<div class="mt-1 max-h-36 overflow-y-auto">
				{#each searchResults as result}
					<button
						onclick={() => handleAddRelation(result.identifier)}
						class="flex w-full items-center gap-2 rounded px-2 py-1 text-xs text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] transition-colors"
					>
						<IssueStatusIcon status={result.status} size={12} />
						<span class="text-[var(--color-text-tertiary)]">{result.identifier}</span>
						<span class="truncate">{result.title}</span>
					</button>
				{/each}
			</div>
		{/if}
		<button
			onclick={() => { showAdd = false; searchQuery = ''; searchResults = []; }}
			class="mt-1 text-[11px] text-[var(--color-text-tertiary)] hover:text-[var(--color-text-secondary)]"
		>
			Cancel
		</button>
	</div>
{/if}
