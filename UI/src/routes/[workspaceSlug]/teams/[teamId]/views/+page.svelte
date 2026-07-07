<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { listViews, deleteView } from '$lib/api/views';
	import type { View } from '$lib/types/view';
	import EmptyState from '$lib/components/shared/EmptyState.svelte';
	import { Badge } from '$lib/components/ui/badge';
	import * as AlertDialog from '$lib/components/ui/alert-dialog';
	import { toast } from 'svelte-sonner';
	import { Bookmark, Trash2, SquareUser, Layers, ChevronRight } from 'lucide-svelte';
	import SidebarToggle from '$lib/components/layout/SidebarToggle.svelte';
	import { sidebarState } from '$lib/features/layout/sidebar.state.svelte';

	const slug = $derived(page.params.workspaceSlug ?? '');
	const teamId = $derived(page.params.teamId ?? '');
	let views = $state<View[]>([]);
	let loading = $state(true);
	let pendingDeleteView = $state<View | null>(null);
	let deleteOpen = $state(false);

	async function loadViews() {
		if (!slug || !teamId) return;
		loading = true;
		try {
			const all = await listViews(slug);
			views = all.filter((v) => v.filters?.team === teamId);
		} finally {
			loading = false;
		}
	}

	$effect(() => {
		loadViews();
	});

	onMount(() => {
		function handleAppRefresh(e: Event) {
			const detail = (e as CustomEvent<{ slug?: string; resources?: string[] }>).detail;
			if (detail?.slug && detail.slug !== slug) return;
			const resources = detail?.resources;
			if (!resources || resources.length === 0 || resources.includes('views')) {
				loadViews();
			}
		}

		window.addEventListener('app:refresh', handleAppRefresh);
		return () => window.removeEventListener('app:refresh', handleAppRefresh);
	});

	function requestDelete(view: View) {
		pendingDeleteView = view;
		deleteOpen = true;
	}

	async function handleDelete() {
		const view = pendingDeleteView;
		if (!view) return;
		try {
			await deleteView(slug, view.id);
			views = views.filter((v) => v.id !== view.id);
			toast.success('View deleted');
		} catch {
			toast.error('Failed to delete view');
		} finally {
			deleteOpen = false;
			pendingDeleteView = null;
		}
	}
</script>

<div class="h-full">
	<div class="flex h-[49px] items-center justify-between border-b border-[var(--app-border)] px-6">
		<div class="flex items-center gap-3">
			<SidebarToggle />
			<nav class="flex items-center gap-1.5 text-sm">
				{#if sidebarState.getTeam(teamId)}
					<a
						href="/{slug}/teams/{teamId}"
						class="flex items-center gap-1.5 text-[var(--color-text-tertiary)] hover:text-[var(--color-text-secondary)]"
					>
						<SquareUser size={14} class="shrink-0" style="color: {sidebarState.getTeamColor(teamId)}" />
						{sidebarState.getTeam(teamId)?.name}
					</a>
					<ChevronRight size={12} class="shrink-0 text-[var(--color-text-tertiary)]" />
				{/if}
				<span class="flex items-center gap-1.5 font-medium text-[var(--color-text-primary)]">
					<Layers size={14} class="shrink-0" />
					Views
				</span>
			</nav>
		</div>
	</div>

	{#if !loading && views.length === 0}
		<EmptyState
			title="No views for this team"
			description="Save filters from the issues page to create reusable views"
		/>
	{:else}
		<div class="divide-y divide-[var(--app-border)]">
			{#each views as view}
				<div class="flex items-center gap-4 px-6 py-3 hover:bg-[var(--color-bg-hover)]">
					<a href="/{slug}/views/{view.id}" class="flex flex-1 items-center gap-3 min-w-0">
						<Bookmark size={14} class="shrink-0 text-[var(--color-text-tertiary)]" />
						<div class="min-w-0">
							<div class="flex items-center gap-2">
								<span class="text-sm font-medium text-[var(--color-text-primary)]">{view.name}</span>
								{#if view.is_shared}
									<Badge variant="outline" class="text-[10px]">Shared</Badge>
								{/if}
							</div>
							{#if view.description}
								<p class="mt-0.5 truncate text-xs text-[var(--color-text-tertiary)]">{view.description}</p>
							{/if}
						</div>
					</a>
					<button
						onclick={() => requestDelete(view)}
						class="shrink-0 rounded p-1 text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-tertiary)] hover:text-red-500"
						title="Delete view"
					>
						<Trash2 size={14} />
					</button>
				</div>
			{/each}
		</div>
	{/if}
</div>

<AlertDialog.Root bind:open={deleteOpen}>
	<AlertDialog.Content>
		<AlertDialog.Header>
			<AlertDialog.Title>Delete view?</AlertDialog.Title>
			<AlertDialog.Description>
				This will permanently delete {pendingDeleteView?.name ?? 'this view'}.
			</AlertDialog.Description>
		</AlertDialog.Header>
		<AlertDialog.Footer>
			<AlertDialog.Cancel variant="outline" onclick={() => (pendingDeleteView = null)}>Cancel</AlertDialog.Cancel>
			<AlertDialog.Action variant="destructive" onclick={handleDelete}>Delete view</AlertDialog.Action>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>
