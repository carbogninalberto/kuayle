<script lang="ts">
	import * as Dialog from '$lib/components/ui/dialog';
	import { Button } from '$lib/components/ui/button';
	import { Switch } from '$lib/components/ui/switch';
	import { Separator } from '$lib/components/ui/separator';
	import type { SharedLink, CreateSharedLinkRequest } from '$lib/types/shared-link';
	import type { ViewFilter } from '$lib/types/view';
	import { createSharedLink, listSharedLinks, updateSharedLink, deleteSharedLink } from '$lib/api/shared-links';
	import { Copy, ExternalLink, Trash2, Link, Info } from 'lucide-svelte';
	import { toast } from 'svelte-sonner';

	let {
		open = $bindable(false),
		slug,
		scope,
		scopeId,
		filters = {}
	}: {
		open: boolean;
		slug: string;
		scope: 'workspace' | 'team' | 'project' | 'view';
		scopeId?: string;
		filters?: ViewFilter;
	} = $props();

	let links = $state<SharedLink[]>([]);
	let loading = $state(false);
	let includeDescription = $state(false);
	let showDescriptionHint = $state(false);

	async function loadLinks() {
		try {
			const all = await listSharedLinks(slug);
			links = all.filter((l) => {
				if (l.scope !== scope) return false;
				if (scopeId && l.scope_id !== scopeId) return false;
				if (!scopeId && l.scope_id) return false;
				return true;
			});
		} catch {
			// ignore
		}
	}

	$effect(() => {
		if (open) {
			showDescriptionHint = false;
			loadLinks();
		}
	});

	async function handleCreate() {
		loading = true;
		try {
			const req: CreateSharedLinkRequest = {
				scope,
				scope_id: scopeId,
				filters: filters as Record<string, string>,
				include_description: includeDescription
			};
			const link = await createSharedLink(slug, req);
			links = [link, ...links];
			toast.success('Share link created');
		} catch {
			toast.error('Failed to create share link');
		} finally {
			loading = false;
		}
	}

	async function handleToggle(link: SharedLink) {
		try {
			const updated = await updateSharedLink(slug, link.id, { is_active: !link.is_active });
			links = links.map((l) => (l.id === updated.id ? updated : l));
			toast.success(updated.is_active ? 'Link activated' : 'Link deactivated');
		} catch {
			toast.error('Failed to update link');
		}
	}

	async function handleDelete(link: SharedLink) {
		try {
			await deleteSharedLink(slug, link.id);
			links = links.filter((l) => l.id !== link.id);
			toast.success('Link deleted');
		} catch {
			toast.error('Failed to delete link');
		}
	}

	function copyUrl(url: string) {
		navigator.clipboard.writeText(url);
		toast.success('Link copied to clipboard');
	}
</script>

<Dialog.Root bind:open>
	<Dialog.Content>
		<Dialog.Header>
			<Dialog.Title>Share public link</Dialog.Title>
			<Dialog.Description>
				Create a read-only public link to share this view with anyone.
			</Dialog.Description>
		</Dialog.Header>

		<div class="space-y-4 py-2 min-w-0">
			<!-- Create new link -->
			<div class="space-y-3">
				<div class="flex items-center justify-between">
					<div class="flex items-center gap-1.5">
						<span class="text-sm text-[var(--color-text-secondary)]">Include descriptions</span>
						<button
							class="text-[var(--color-text-tertiary)] hover:text-[var(--color-text-secondary)] transition-colors"
							onclick={() => (showDescriptionHint = !showDescriptionHint)}
						>
							<Info size={13} />
						</button>
					</div>
					<Switch bind:checked={includeDescription} />
				</div>
				{#if showDescriptionHint}
					<p class="text-xs text-[var(--color-text-tertiary)] -mt-1">
						When enabled, issue descriptions will be visible to anyone with the link. Keep disabled if descriptions contain sensitive information.
					</p>
				{/if}
				<Button onclick={handleCreate} disabled={loading} size="sm" class="w-full">
					<Link size={14} class="mr-2" />
					Create new link
				</Button>
			</div>

			{#if links.length > 0}
				<Separator />

				<div class="space-y-2 min-w-0">
					<p class="text-xs font-medium text-[var(--color-text-tertiary)] uppercase tracking-wider">Existing links</p>
					{#each links as link (link.id)}
						<div class="rounded-md border border-[var(--app-border)] p-2 min-w-0 {link.is_active ? '' : 'opacity-50'}">
							<p class="truncate text-xs text-[var(--color-text-primary)] font-mono min-w-0">{link.url}</p>
							<div class="flex items-center gap-1.5 mt-1.5">
								<p class="text-[10px] text-[var(--color-text-tertiary)]">
									{link.is_active ? 'Active' : 'Inactive'}
									{#if link.expires_at}
										 &middot; Expires {new Date(link.expires_at).toLocaleDateString()}
									{/if}
									{#if link.include_description}
										 &middot; With descriptions
									{/if}
								</p>
								<div class="flex-1"></div>
								<button
									onclick={() => copyUrl(link.url)}
									class="shrink-0 rounded p-1 text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)]"
									title="Copy link"
								>
									<Copy size={14} />
								</button>
								<a
									href={link.url}
									target="_blank"
									rel="noopener noreferrer"
									class="shrink-0 rounded p-1 text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)]"
									title="Open in new tab"
								>
									<ExternalLink size={14} />
								</a>
								<button
									onclick={() => handleToggle(link)}
									class="shrink-0 rounded px-1.5 py-0.5 text-[10px] border border-[var(--app-border)] text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)]"
								>
									{link.is_active ? 'Disable' : 'Enable'}
								</button>
								<button
									onclick={() => handleDelete(link)}
									class="shrink-0 rounded p-1 text-[var(--color-text-tertiary)] hover:text-red-500 hover:bg-[var(--color-bg-hover)]"
									title="Delete link"
								>
									<Trash2 size={14} />
								</button>
							</div>
						</div>
					{/each}
				</div>
			{/if}
		</div>
	</Dialog.Content>
</Dialog.Root>
