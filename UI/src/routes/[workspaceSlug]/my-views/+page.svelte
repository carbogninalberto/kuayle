<script lang="ts">
	import { Button } from '$lib/components/ui/button/index.js';
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { listViews, deleteView } from '$lib/api/views';
	import type { View } from '$lib/types/view';
	import { isPersonalView } from '$lib/types/view';
	import EmptyState from '$lib/components/shared/EmptyState.svelte';
	import SidebarToggle from '$lib/components/layout/SidebarToggle.svelte';
	import * as AlertDialog from '$lib/components/ui/alert-dialog';
	import { appToast } from '$lib/features/toast/toast';
	import { Bookmark, CircleUser, Trash2 } from '@lucide/svelte';
	import { m } from '$lib/paraglide/messages.js';
	import { getLocale, setLocale } from '$lib/paraglide/runtime.js';

	const slug = $derived(page.params.workspaceSlug ?? '');

	let views = $state<View[]>([]);
	let loading = $state(true);
	let pendingDeleteView = $state<View | null>(null);
	let deleteOpen = $state(false);

	async function loadViews() {
		if (!slug) return;
		loading = true;
		try {
			const all = await listViews(slug);
			views = all.filter(isPersonalView);
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
			views = views.filter((item) => item.id !== view.id);
			appToast.success(m['views.toast.deleted']());
		} catch {
			appToast.error(m['views.toast.failed_delete']());
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
				<span class="flex items-center gap-1.5 font-medium text-[var(--color-text-primary)]">
					<Bookmark size={14} class="shrink-0" />
					{m['views.my_views']()}
				</span>
			</nav>
		</div>
	</div>

	{#if !loading && views.length === 0}
		<EmptyState
			title={m['views.no_personal_views']()}
			description={m['views.no_personal_views_desc']()}
		/>
	{:else}
		<div class="divide-y divide-[var(--app-border)]">
			{#each views as view}
				<div class="flex items-center gap-4 px-6 py-3 hover:bg-[var(--color-bg-hover)]">
					<a href="/{slug}/views/{view.id}" class="flex min-w-0 flex-1 items-center gap-3">
						<CircleUser size={14} class="shrink-0 text-[var(--color-text-tertiary)]" />
						<div class="min-w-0">
							<div class="flex items-center gap-2">
								<span class="truncate text-sm font-medium text-[var(--color-text-primary)]">{view.name}</span>
							</div>
							{#if view.description}
								<p class="mt-0.5 truncate text-xs text-[var(--color-text-tertiary)]">{view.description}</p>
							{/if}
						</div>
					</a>
					<Button variant="plain" onclick={() => requestDelete(view)}
						class="shrink-0 rounded p-1 text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-tertiary)] hover:text-red-500"
						title={m['views.delete_view']()}
					>
						<Trash2 size={14} />
					</Button>
				</div>
			{/each}
		</div>
	{/if}
</div>

<AlertDialog.Root bind:open={deleteOpen}>
	<AlertDialog.Content>
		<AlertDialog.Header>
			<AlertDialog.Title>{m['views.delete_title']()}</AlertDialog.Title>
			<AlertDialog.Description>
				{m['views.delete_desc']({ name: pendingDeleteView?.name ?? '' })}
			</AlertDialog.Description>
		</AlertDialog.Header>
		<AlertDialog.Footer>
			<AlertDialog.Cancel variant="outline" onclick={() => (pendingDeleteView = null)}>{m['common.cancel']()}</AlertDialog.Cancel>
			<AlertDialog.Action variant="destructive" onclick={handleDelete}>{m['views.delete_view']()}</AlertDialog.Action>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>
