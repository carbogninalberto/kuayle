<script lang="ts">
	import { onMount } from 'svelte';
	import type { Issue, IssueRelation, RelationType } from '$lib/types/issue';
	import type { PaginatedResponse } from '$lib/types/common';
	import { listRelations, createRelation, deleteRelation } from '$lib/api/issue-relations';
	import { listIssues } from '$lib/api/issues';
	import { Badge } from '$lib/components/ui/badge';
	import { Button } from '$lib/components/ui/button';
	import { Separator } from '$lib/components/ui/separator';
	import * as Command from '$lib/components/ui/command';
	import { Link, X, Plus, Ban, Copy, ArrowRight } from 'lucide-svelte';

	let { slug, identifier }: { slug: string; identifier: string } = $props();

	let relations = $state<IssueRelation[]>([]);
	let dialogOpen = $state(false);
	let selectedType = $state<RelationType>('related');
	let searchQuery = $state('');
	let searchResults = $state<Issue[]>([]);
	let searching = $state(false);

	const RELATION_TYPES: { value: RelationType; label: string; icon: any }[] = [
		{ value: 'related', label: 'Related', icon: Link },
		{ value: 'blocked_by', label: 'Blocked By', icon: Ban },
		{ value: 'blocking', label: 'Blocking', icon: ArrowRight },
		{ value: 'duplicate', label: 'Duplicate', icon: Copy }
	];

	const RELATION_LABELS: Record<RelationType, string> = {
		related: 'Related',
		blocked_by: 'Blocked By',
		blocking: 'Blocking',
		duplicate: 'Duplicate'
	};

	let groupedRelations = $derived(
		RELATION_TYPES.map((type) => ({
			...type,
			items: relations.filter((r) => r.type === type.value)
		})).filter((group) => group.items.length > 0)
	);

	onMount(async () => {
		await loadRelations();
	});

	async function loadRelations() {
		relations = await listRelations(slug, identifier);
	}

	async function handleSearch(query: string) {
		searchQuery = query;
		if (!query.trim()) {
			searchResults = [];
			return;
		}
		searching = true;
		try {
			const response: PaginatedResponse<Issue> = await listIssues(slug, {
				search: query,
				limit: '10'
			});
			searchResults = response.data.filter(
				(issue) => issue.identifier !== identifier
			);
		} catch {
			searchResults = [];
		} finally {
			searching = false;
		}
	}

	async function handleAddRelation(relatedIdentifier: string) {
		await createRelation(slug, identifier, {
			related_identifier: relatedIdentifier,
			type: selectedType
		});
		await loadRelations();
		dialogOpen = false;
		searchQuery = '';
		searchResults = [];
	}

	async function handleRemoveRelation(relationId: string) {
		await deleteRelation(slug, identifier, relationId);
		relations = relations.filter((r) => r.id !== relationId);
	}
</script>

<div class="space-y-3">
	<div class="flex items-center justify-between">
		<h3 class="text-sm font-medium text-[var(--color-text-primary)]">Relations</h3>
		<Button variant="ghost" size="sm" onclick={() => (dialogOpen = true)}>
			<Plus size={14} />
			<span class="ml-1">Add relation</span>
		</Button>
	</div>

	{#if groupedRelations.length === 0}
		<p class="text-xs text-[var(--color-text-tertiary)]">No relations</p>
	{:else}
		{#each groupedRelations as group, i}
			{#if i > 0}
				<Separator />
			{/if}
			<div class="space-y-1.5">
				<div class="flex items-center gap-1.5">
					<svelte:component this={group.icon} size={12} class="text-[var(--color-text-tertiary)]" />
					<span class="text-xs font-medium text-[var(--color-text-tertiary)]">{group.label}</span>
				</div>
				{#each group.items as relation}
					<div
						class="flex items-center justify-between rounded-md px-2 py-1.5 hover:bg-[var(--color-bg-hover)]"
					>
						<div class="flex items-center gap-2 min-w-0">
							<Badge variant="outline" class="shrink-0 text-xs">
								{relation.related_issue?.identifier ?? relation.related_issue_id.slice(0, 8)}
							</Badge>
							<span class="truncate text-sm text-[var(--color-text-secondary)]">
								{relation.related_issue?.title ?? ''}
							</span>
						</div>
						<button
							onclick={() => handleRemoveRelation(relation.id)}
							class="shrink-0 ml-2 text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)]"
						>
							<X size={14} />
						</button>
					</div>
				{/each}
			</div>
		{/each}
	{/if}
</div>

<Command.CommandDialog bind:open={dialogOpen} title="Add relation" description="Search for an issue to relate">
	{#snippet children()}
		<div class="flex gap-1 border-b border-[var(--app-border)] px-3 py-2">
			{#each RELATION_TYPES as type}
				<button
					onclick={() => (selectedType = type.value)}
					class="rounded-md px-2 py-1 text-xs transition-colors {selectedType === type.value
						? 'bg-[var(--app-accent)] text-white'
						: 'text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)]'}"
				>
					{type.label}
				</button>
			{/each}
		</div>
		<Command.CommandInput
			placeholder="Search issues by identifier or title..."
			value={searchQuery}
			oninput={(e) => handleSearch((e.target as HTMLInputElement).value)}
		/>
		<Command.CommandList>
			{#if searching}
				<Command.CommandLoading>Searching...</Command.CommandLoading>
			{/if}
			<Command.CommandEmpty>No issues found.</Command.CommandEmpty>
			<Command.CommandGroup>
				{#each searchResults as issue}
					<Command.CommandItem
						value={issue.identifier}
						onSelect={() => handleAddRelation(issue.identifier)}
					>
						<span class="mr-2 text-xs text-[var(--color-text-tertiary)]">{issue.identifier}</span>
						<span class="truncate">{issue.title}</span>
					</Command.CommandItem>
				{/each}
			</Command.CommandGroup>
		</Command.CommandList>
	{/snippet}
</Command.CommandDialog>
