<script lang="ts">
	import { onMount } from 'svelte';
	import { listNotifications, markAllRead } from '$lib/api/notifications';
	import type { Notification } from '$lib/types/notification';
	import { formatRelativeTime } from '$lib/utils/format';
	import EmptyState from '$lib/components/shared/EmptyState.svelte';
	import { toast } from 'svelte-sonner';

	let notifications = $state<Notification[]>([]);
	let loading = $state(true);

	onMount(async () => {
		try {
			notifications = await listNotifications();
		} finally {
			loading = false;
		}
	});

	async function handleMarkAllRead() {
		try {
			await markAllRead();
			notifications = notifications.map((n) => ({ ...n, read_at: new Date().toISOString() }));
			toast.success('All notifications marked as read');
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to mark notifications as read');
		}
	}
</script>

<div class="h-full">
	<div class="flex h-[49px] items-center justify-between border-b border-[var(--app-border)] px-6">
		<h1 class="text-sm font-medium text-[var(--color-text-primary)]">Inbox</h1>
		{#if notifications.length > 0}
			<button
				onclick={handleMarkAllRead}
				class="text-xs text-[var(--color-text-tertiary)] hover:text-[var(--color-text-secondary)]"
			>
				Mark all read
			</button>
		{/if}
	</div>

	{#if loading}
		<div class="flex h-64 items-center justify-center">
			<p class="text-sm text-[var(--color-text-secondary)]">Loading...</p>
		</div>
	{:else if notifications.length === 0}
		<EmptyState title="No notifications" description="You're all caught up!" />
	{:else}
		<div class="divide-y divide-[var(--app-border)]">
			{#each notifications as notification}
				<div
					class="flex items-center gap-3 px-6 py-3 {notification.read_at ? 'opacity-60' : ''}"
				>
					{#if !notification.read_at}
						<div class="h-2 w-2 rounded-full bg-[var(--app-accent)]"></div>
					{:else}
						<div class="h-2 w-2"></div>
					{/if}
					<div class="flex-1">
						<p class="text-sm text-[var(--color-text-primary)]">{notification.title}</p>
						<p class="text-xs text-[var(--color-text-tertiary)]">
							{formatRelativeTime(notification.created_at)}
						</p>
					</div>
				</div>
			{/each}
		</div>
	{/if}
</div>
