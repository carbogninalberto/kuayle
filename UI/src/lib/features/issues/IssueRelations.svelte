<script lang="ts">
	import { onMount } from 'svelte';
	import type { IssueRelation, RelationType } from '$lib/types/issue';
	import { listRelations, deleteRelation } from '$lib/api/issue-relations';
	import IssueStatusIcon from './IssueStatusIcon.svelte';
	import AddRelationDialog from './AddRelationDialog.svelte';
	import * as Collapsible from '$lib/components/ui/collapsible';
	import { Link, X, Plus, Ban, Copy, ArrowRight, ChevronRight } from 'lucide-svelte';
	import { appToast } from '$lib/features/toast/toast';

	let {
		slug,
		identifier,
		dialogOpen = $bindable(false),
		dialogType = $bindable<RelationType>('related')
	}: {
		slug: string;
		identifier: string;
		dialogOpen?: boolean;
		dialogType?: RelationType;
	} = $props();

	let relations = $state<IssueRelation[]>([]);
	let loading = $state(true);
	let expanded = $state(true);

	const RELATION_LABELS: Record<RelationType, string> = {
		related: 'Related to',
		blocked_by: 'Blocked by',
		blocking: 'Blocking',
		duplicate: 'Duplicate of'
	};

	const RELATION_ICONS: Record<RelationType, any> = {
		related: Link,
		blocked_by: Ban,
		blocking: ArrowRight,
		duplicate: Copy
	};

	const RELATION_ICON_CLASSES: Record<RelationType, string> = {
		related: 'text-[var(--color-text-tertiary)]',
		blocked_by: 'text-amber-500',
		blocking: 'text-blue-400',
		duplicate: 'text-purple-400'
	};

	const RELATION_TYPES: RelationType[] = ['related', 'blocked_by', 'blocking', 'duplicate'];

	let relationCount = $derived(relations.length);
	let groupedRelations = $derived(
		RELATION_TYPES.map((type) => ({
			type,
			label: RELATION_LABELS[type],
			items: relations.filter((r) => r.type === type)
		})).filter((group) => group.items.length > 0)
	);

	onMount(async () => {
		await refreshRelations();
	});

	async function refreshRelations() {
		try {
			relations = await listRelations(slug, identifier);
		} catch {
			relations = [];
		} finally {
			loading = false;
		}
	}

	function openAdd(type: RelationType = 'related') {
		dialogType = type;
		dialogOpen = true;
		expanded = true;
	}

	async function handleRemoveRelation(relationId: string) {
		try {
			await deleteRelation(slug, identifier, relationId);
			relations = relations.filter((r) => r.id !== relationId);
		} catch (err: any) {
			appToast.apiError(err, 'Failed to remove relation');
		}
	}
</script>

{#if !loading}
	{#if relations.length > 0}
		<Collapsible.Root bind:open={expanded}>
			<div class="overflow-hidden rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]/60">
				<div class="flex items-center gap-2 px-3 py-1.5">
					<Collapsible.Trigger class="flex min-w-0 flex-1 items-center gap-2 text-sm text-[var(--color-text-secondary)] hover:text-[var(--color-text-primary)]">
						<ChevronRight size={14} class="transition-transform {expanded ? 'rotate-90' : ''}" />
						<Link size={14} class="shrink-0" />
						<span class="font-medium">Relations</span>
						{#if relationCount > 0}
							<span class="rounded-full bg-[var(--color-bg-tertiary)] px-2 py-0.5 text-xs text-[var(--color-text-tertiary)]">
								{relationCount}
							</span>
						{/if}
					</Collapsible.Trigger>

					<button
						onclick={(e) => { e.stopPropagation(); openAdd(); }}
						class="ml-auto flex h-6 w-6 shrink-0 items-center justify-center rounded-full text-[var(--color-text-tertiary)] transition-colors hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]"
						title="Add relation"
					>
						<Plus size={13} />
					</button>
				</div>

				<Collapsible.Content>
					<div class="border-t border-[var(--app-border)]">
						{#if groupedRelations.length > 0}
							<div class="py-1">
								{#each groupedRelations as group}
									{@const Icon = RELATION_ICONS[group.type]}
									<div class="py-1">
										<div class="flex items-center gap-1.5 px-3 py-1 text-[11px] font-medium text-[var(--color-text-tertiary)]">
											<Icon size={12} class={RELATION_ICON_CLASSES[group.type]} />
											{group.label}
										</div>
										{#each group.items as relation (relation.id)}
											<div class="group/relation flex items-center gap-1 px-3 py-0.5 transition-colors hover:bg-[var(--color-bg-hover)]">
												{#if relation.related_issue}
													<a href="/{slug}/issue/{relation.related_issue.identifier}" class="flex min-w-0 flex-1 items-center gap-2 rounded-md px-1.5 py-1 text-xs text-[var(--color-text-secondary)] transition-colors hover:bg-[var(--color-bg-tertiary)] hover:text-[var(--color-text-primary)]">
														<IssueStatusIcon status={relation.related_issue.status} category={relation.related_issue.status_info?.category} color={relation.related_issue.status_info?.color} size={13} />
														<span class="shrink-0 tabular-nums text-[var(--color-text-tertiary)]">{relation.related_issue.identifier}</span>
														<span class="min-w-0 flex-1 truncate">{relation.related_issue.title}</span>
													</a>
												{:else}
													<div class="flex min-w-0 flex-1 items-center gap-2 px-1.5 py-1 text-xs text-[var(--color-text-tertiary)]">
														<Link size={13} class="shrink-0 opacity-60" />
														<span>Unavailable issue</span>
													</div>
												{/if}
												<button
													onclick={() => handleRemoveRelation(relation.id)}
													class="ml-1 shrink-0 rounded p-1 text-[var(--color-text-tertiary)] opacity-0 transition-opacity hover:bg-[var(--color-bg-tertiary)] hover:text-[var(--color-text-primary)] group-hover/relation:opacity-100"
													title="Remove relation"
												>
													<X size={12} />
												</button>
											</div>
										{/each}
									</div>
								{/each}
							</div>
						{/if}
					</div>
				</Collapsible.Content>
			</div>
		</Collapsible.Root>
	{/if}

	<AddRelationDialog
		bind:open={dialogOpen}
		{slug}
		{identifier}
		defaultType={dialogType}
		oncreated={refreshRelations}
	/>
{/if}
