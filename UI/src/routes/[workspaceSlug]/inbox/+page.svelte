<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { page } from '$app/state';
	import { listNotifications, markAllRead, markNotificationRead, archiveNotification, snoozeNotification } from '$lib/api/notifications';
	import type { Notification } from '$lib/types/notification';
	import { formatRelativeTime } from '$lib/utils/format';
	import EmptyState from '$lib/components/shared/EmptyState.svelte';
	import * as Tabs from '$lib/components/ui/tabs';
	import * as Popover from '$lib/components/ui/popover';
	import { Button } from '$lib/components/ui/button';
	import { Badge } from '$lib/components/ui/badge';
	import { toast } from 'svelte-sonner';
	import { Inbox, Clock, Archive, Eye, AlarmClock, Trash2, ExternalLink } from 'lucide-svelte';
	import SidebarToggle from '$lib/components/layout/SidebarToggle.svelte';

	type TabValue = 'inbox' | 'snoozed' | 'archived';

	const slug = $derived(page.params.workspaceSlug ?? '');

	const NOTIFICATION_TYPE_LABELS: Record<string, string> = {
		status_changed: 'Status changed',
		assigned: 'Assigned to you',
		commented: 'New comment',
		mentioned: 'You were mentioned',
		priority_changed: 'Priority changed',
		issue_created: 'New issue created',
		issue_updated: 'Issue updated',
		due_date_changed: 'Due date changed',
		label_added: 'Label added',
		cycle_changed: 'Cycle changed'
	};

	function getNotificationTypeLabel(type: string): string {
		return NOTIFICATION_TYPE_LABELS[type] || type.replace(/_/g, ' ');
	}

	let notifications = $state<Notification[]>([]);
	let unreadCount = $state(0);
	let loading = $state(true);
	let activeTab = $state<TabValue>('inbox');
	let selectedIndex = $state(-1);

	onMount(async () => {
		await loadNotifications();

		// Reload when a new notification arrives via WebSocket
		const onWsNotification = () => loadNotifications();
		window.addEventListener('ws:notification', onWsNotification);
		return () => window.removeEventListener('ws:notification', onWsNotification);
	});

	async function loadNotifications() {
		loading = true;
		try {
			const tab = activeTab === 'inbox' ? undefined : activeTab;
			const res = await listNotifications(tab);
			notifications = res.notifications;
			unreadCount = res.unread_count;
			selectedIndex = notifications.length > 0 ? 0 : -1;
		} finally {
			loading = false;
		}
	}

	async function handleTabChange(tab: string) {
		activeTab = tab as TabValue;
		await loadNotifications();
	}

	async function handleMarkAllRead() {
		try {
			await markAllRead();
			notifications = notifications.map((n) => ({ ...n, read_at: new Date().toISOString() }));
			unreadCount = 0;
			toast.success('All marked as read');
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to mark as read');
		}
	}

	async function handleMarkRead(id: string) {
		try {
			await markNotificationRead(id);
			notifications = notifications.map((n) =>
				n.id === id ? { ...n, read_at: new Date().toISOString() } : n
			);
			unreadCount = Math.max(0, unreadCount - 1);
		} catch {
			// Silent
		}
	}

	async function handleToggleRead(id: string) {
		const n = notifications.find((n) => n.id === id);
		if (!n) return;
		if (n.read_at) {
			// Mark unread — not supported by backend, just skip
			return;
		}
		await handleMarkRead(id);
	}

	async function handleArchive(id: string) {
		try {
			await archiveNotification(id);
			notifications = notifications.filter((n) => n.id !== id);
			toast.success('Archived');
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to archive');
		}
	}

	async function handleSnooze(id: string, hours: number) {
		const until = new Date(Date.now() + hours * 60 * 60 * 1000).toISOString();
		try {
			await snoozeNotification(id, until);
			notifications = notifications.filter((n) => n.id !== id);
			toast.success(`Snoozed for ${hours}h`);
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to snooze');
		}
	}

	// Keyboard navigation
	function handleKeydown(e: KeyboardEvent) {
		const target = e.target as HTMLElement;
		if (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA') return;

		switch (e.key.toLowerCase()) {
			case 'j':
				e.preventDefault();
				selectedIndex = Math.min(selectedIndex + 1, notifications.length - 1);
				break;
			case 'k':
				e.preventDefault();
				selectedIndex = Math.max(selectedIndex - 1, 0);
				break;
			case 'u':
				e.preventDefault();
				if (selectedIndex >= 0 && notifications[selectedIndex]) {
					handleToggleRead(notifications[selectedIndex].id);
				}
				break;
			case 'e':
				e.preventDefault();
				if (selectedIndex >= 0 && notifications[selectedIndex]) {
					handleArchive(notifications[selectedIndex].id);
				}
				break;
			case 'h':
				e.preventDefault();
				if (selectedIndex >= 0 && notifications[selectedIndex]) {
					handleSnooze(notifications[selectedIndex].id, 3);
				}
				break;
		}
	}

	onMount(() => {
		document.addEventListener('keydown', handleKeydown);
		return () => document.removeEventListener('keydown', handleKeydown);
	});

	let snoozeOpenId = $state<string | null>(null);
</script>

<div class="flex h-full flex-col">
	<div class="flex h-[49px] items-center justify-between border-b border-[var(--app-border)] px-6">
		<div class="flex items-center gap-2">
			<SidebarToggle />
			<h1 class="text-sm font-medium text-[var(--color-text-primary)]">Inbox</h1>
			{#if unreadCount > 0}
				<Badge variant="default" class="text-[10px]">{unreadCount}</Badge>
			{/if}
		</div>
		{#if activeTab === 'inbox' && notifications.length > 0}
			<button
				onclick={handleMarkAllRead}
				class="text-xs text-[var(--color-text-tertiary)] hover:text-[var(--color-text-secondary)]"
			>
				Mark all read
			</button>
		{/if}
	</div>

	<!-- Tabs -->
	<Tabs.Root value={activeTab} onValueChange={handleTabChange}>
		<Tabs.List class="w-full justify-start gap-1.5 rounded-none border-none bg-transparent px-2 pt-4 pb-2">
			<Tabs.Trigger value="inbox" class="flex-none h-auto rounded-full border border-[var(--app-border)] px-2.5 py-1 text-xs text-[var(--color-text-tertiary)] shadow-none data-[state=active]:border-[var(--app-accent)]/30 data-[state=active]:bg-[var(--app-accent)]/10 data-[state=active]:text-[var(--app-accent-light)] data-[state=active]:shadow-none">
				<Inbox size={13} class="mr-1" />
				Inbox
			</Tabs.Trigger>
			<Tabs.Trigger value="snoozed" class="flex-none h-auto rounded-full border border-[var(--app-border)] px-2.5 py-1 text-xs text-[var(--color-text-tertiary)] shadow-none data-[state=active]:border-[var(--app-accent)]/30 data-[state=active]:bg-[var(--app-accent)]/10 data-[state=active]:text-[var(--app-accent-light)] data-[state=active]:shadow-none">
				<Clock size={13} class="mr-1" />
				Snoozed
			</Tabs.Trigger>
			<Tabs.Trigger value="archived" class="flex-none h-auto rounded-full border border-[var(--app-border)] px-2.5 py-1 text-xs text-[var(--color-text-tertiary)] shadow-none data-[state=active]:border-[var(--app-accent)]/30 data-[state=active]:bg-[var(--app-accent)]/10 data-[state=active]:text-[var(--app-accent-light)] data-[state=active]:shadow-none">
				<Archive size={13} class="mr-1" />
				Archived
			</Tabs.Trigger>
		</Tabs.List>

		<Tabs.Content value={activeTab} class="flex-1 overflow-y-auto mt-0">
			{#if loading}
				<div class="flex h-64 items-center justify-center">
					<p class="text-sm text-[var(--color-text-secondary)]">Loading...</p>
				</div>
			{:else if notifications.length === 0}
				<EmptyState
					title={activeTab === 'inbox' ? 'No notifications' : activeTab === 'snoozed' ? 'No snoozed notifications' : 'No archived notifications'}
					description={activeTab === 'inbox' ? "You're all caught up!" : ''}
				/>
			{:else}
				<div class="divide-y divide-[var(--app-border)]">
					{#each notifications as notification, i}
						<div
							class="group flex items-center gap-3 px-6 py-3 {notification.read_at ? 'opacity-60' : ''} {selectedIndex === i ? 'bg-[var(--color-bg-hover)]' : ''}"
							role="button"
							tabindex="-1"
							onclick={() => { selectedIndex = i; if (!notification.read_at) handleMarkRead(notification.id); }}
							onkeydown={() => {}}
						>
							{#if !notification.read_at && activeTab === 'inbox'}
								<div class="h-2 w-2 shrink-0 rounded-full bg-[var(--app-accent)]"></div>
							{:else}
								<div class="h-2 w-2 shrink-0"></div>
							{/if}
							<div class="flex-1 min-w-0">
								<p class="text-sm text-[var(--color-text-primary)]">{notification.title}</p>
								<div class="mt-0.5 flex items-center gap-2">
									{#if notification.type}
										<span class="rounded bg-[var(--color-bg-tertiary)] px-1.5 py-0.5 text-[10px] text-[var(--color-text-tertiary)]">
											{getNotificationTypeLabel(notification.type)}
										</span>
									{/if}
									<p class="text-xs text-[var(--color-text-tertiary)]">
										{formatRelativeTime(notification.created_at)}
										{#if notification.snoozed_until && activeTab === 'snoozed'}
											· Snoozed until {new Date(notification.snoozed_until).toLocaleDateString('en-US', { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' })}
										{/if}
									</p>
								</div>
							</div>
							{#if notification.issue_id}
								<a
									href="/{slug}/issue/{notification.issue_id}"
									onclick={(e) => e.stopPropagation()}
									class="shrink-0 text-[var(--color-text-tertiary)] hover:text-[var(--app-accent)]"
									title="Go to issue"
								>
									<ExternalLink size={13} />
								</a>
							{/if}
							{#if activeTab === 'inbox'}
								<div class="flex items-center gap-1 opacity-0 group-hover:opacity-100">
									<!-- Snooze -->
									<Popover.Root open={snoozeOpenId === notification.id} onOpenChange={(open) => { snoozeOpenId = open ? notification.id : null; }}>
										<Popover.Trigger>
											<Button variant="ghost" size="icon-sm" class="h-7 w-7" title="Snooze">
												<AlarmClock size={13} />
											</Button>
										</Popover.Trigger>
										<Popover.Content class="w-36 p-1" align="end">
											<button onclick={() => { snoozeOpenId = null; handleSnooze(notification.id, 1); }} class="flex w-full rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)]">1 hour</button>
											<button onclick={() => { snoozeOpenId = null; handleSnooze(notification.id, 3); }} class="flex w-full rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)]">3 hours</button>
											<button onclick={() => { snoozeOpenId = null; handleSnooze(notification.id, 24); }} class="flex w-full rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)]">Tomorrow</button>
											<button onclick={() => { snoozeOpenId = null; handleSnooze(notification.id, 72); }} class="flex w-full rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)]">3 days</button>
										</Popover.Content>
									</Popover.Root>
									<!-- Archive -->
									<Button variant="ghost" size="icon-sm" class="h-7 w-7" title="Archive" onclick={() => handleArchive(notification.id)}>
										<Archive size={13} />
									</Button>
									<!-- Mark read -->
									{#if !notification.read_at}
										<Button variant="ghost" size="icon-sm" class="h-7 w-7" title="Mark read" onclick={() => handleMarkRead(notification.id)}>
											<Eye size={13} />
										</Button>
									{/if}
								</div>
							{/if}
						</div>
					{/each}
				</div>
			{/if}
		</Tabs.Content>
	</Tabs.Root>
</div>
