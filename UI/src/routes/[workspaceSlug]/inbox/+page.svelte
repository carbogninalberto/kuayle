<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import {
		listNotifications,
		markAllRead,
		markNotificationRead,
		markNotificationUnread,
		archiveNotification,
		unarchiveNotification,
		snoozeNotification,
		unsnoozeNotification
	} from '$lib/api/notifications';
	import { getIssue } from '$lib/api/issues';
	import type { Notification } from '$lib/types/notification';
	import type { Issue } from '$lib/types/issue';
	import { formatRelativeTime } from '$lib/utils/format';
	import EmptyState from '$lib/components/shared/EmptyState.svelte';
	import FullPageIssueView from '$lib/features/issues/FullPageIssueView.svelte';
	import DueDatePickerPanel from '$lib/components/shared/DueDatePickerPanel.svelte';
	import * as Tabs from '$lib/components/ui/tabs';
	import * as ContextMenu from '$lib/components/ui/context-menu';
	import { Badge } from '$lib/components/ui/badge';
	import { appToast } from '$lib/features/toast/toast';
	import {
		Inbox,
		Clock,
		Archive,
		AlarmClock,
		ArrowRightLeft,
		UserCheck,
		MessageSquare,
		AtSign,
		Signal,
		CirclePlus,
		Pencil,
		CalendarDays,
		Eye,
		Tag,
		RefreshCw
	} from 'lucide-svelte';
	import SidebarToggle from '$lib/components/layout/SidebarToggle.svelte';
	import { IsMobile } from '$lib/hooks/is-mobile.svelte';

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

	function normalizeNotificationType(type: string): string {
		return type.includes('.') ? (type.split('.').pop() ?? type) : type;
	}

	function getNotificationTypeLabel(type: string): string {
		const normalizedType = normalizeNotificationType(type);
		return NOTIFICATION_TYPE_LABELS[normalizedType] || normalizedType.replace(/_/g, ' ');
	}

	const NOTIFICATION_TYPE_STYLE: Record<string, { icon: any; color: string; bg: string }> = {
		status_changed: { icon: ArrowRightLeft, color: '#60a5fa', bg: 'rgba(59,130,246,0.18)' },
		assigned: { icon: UserCheck, color: '#4ade80', bg: 'rgba(34,197,94,0.18)' },
		commented: { icon: MessageSquare, color: '#c084fc', bg: 'rgba(168,85,247,0.18)' },
		mentioned: { icon: AtSign, color: '#fb923c', bg: 'rgba(249,115,22,0.18)' },
		priority_changed: { icon: Signal, color: '#f87171', bg: 'rgba(239,68,68,0.18)' },
		issue_created: { icon: CirclePlus, color: '#4ade80', bg: 'rgba(34,197,94,0.18)' },
		issue_updated: { icon: Pencil, color: '#60a5fa', bg: 'rgba(59,130,246,0.18)' },
		due_date_changed: { icon: CalendarDays, color: '#fbbf24', bg: 'rgba(245,158,11,0.18)' },
		label_added: { icon: Tag, color: '#f472b6', bg: 'rgba(236,72,153,0.18)' },
		cycle_changed: { icon: RefreshCw, color: '#2dd4bf', bg: 'rgba(20,184,166,0.18)' }
	};

	function getTypeStyle(type: string) {
		return (
			NOTIFICATION_TYPE_STYLE[normalizeNotificationType(type)] ?? {
				icon: Inbox,
				color: '#6b7280',
				bg: 'rgba(107,114,128,0.12)'
			}
		);
	}

	let notifications = $state<Notification[]>([]);
	let unreadCount = $state(0);
	let loading = $state(true);
	let activeTab = $state<TabValue>('inbox');
	let selectedId = $state<string | null>(null);
	let selectedIssue = $state<Issue | null>(null);
	let issueLoading = $state(false);
	let snoozeDateNotification = $state<Notification | null>(null);
	const isMobile = new IsMobile();

	const selectedNotification = $derived(notifications.find((n) => n.id === selectedId) ?? null);

	onMount(() => {
		loadNotifications();
		const onWsNotification = () => loadNotifications();
		window.addEventListener('ws:notification', onWsNotification);
		return () => window.removeEventListener('ws:notification', onWsNotification);
	});

	async function loadNotifications() {
		loading = true;
		try {
			const tab = activeTab === 'inbox' ? undefined : activeTab;
			const res = await listNotifications(tab);
			notifications = res.notifications ?? [];
			unreadCount = res.unread_count;
			// Auto-select first if nothing selected
			if (!isMobile.current && !selectedId && notifications.length > 0) {
				selectNotification(notifications[0]);
			}
		} finally {
			loading = false;
		}
	}

	async function selectNotification(n: Notification) {
		selectedId = n.id;
		if (!n.read_at && activeTab === 'inbox') {
			handleMarkRead(n.id);
		}
		if (isMobile.current && n.issue_identifier) {
			goto(`/${slug}/issue/${n.issue_identifier}`);
			return;
		}
		// Load issue if available
		if (n.issue_identifier) {
			issueLoading = true;
			try {
				selectedIssue = await getIssue(slug, n.issue_identifier);
			} catch {
				selectedIssue = null;
			} finally {
				issueLoading = false;
			}
		} else {
			selectedIssue = null;
		}
	}

	async function handleTabChange(tab: string) {
		activeTab = tab as TabValue;
		selectedId = null;
		selectedIssue = null;
		await loadNotifications();
	}

	async function handleMarkAllRead() {
		try {
			await markAllRead();
			notifications = notifications.map((n) => ({ ...n, read_at: new Date().toISOString() }));
			unreadCount = 0;
			appToast.success('All marked as read');
		} catch (err: any) {
			appToast.apiError(err, 'Failed to mark as read');
		}
	}

	async function handleMarkRead(id: string) {
		try {
			const existing = notifications.find((n) => n.id === id);
			const updated = await markNotificationRead(id);
			notifications = notifications.map((n) => (n.id === id ? { ...n, read_at: new Date().toISOString() } : n));
			if (existing && !existing.read_at) unreadCount = Math.max(0, unreadCount - 1);
			return updated;
		} catch {
			/* silent */
		}
	}

	async function handleMarkUnread(id: string) {
		try {
			const existing = notifications.find((n) => n.id === id);
			await markNotificationUnread(id);
			notifications = notifications.map((n) => (n.id === id ? { ...n, read_at: null } : n));
			if (activeTab === 'inbox' && existing?.read_at) unreadCount++;
			appToast.success('Marked as unread');
		} catch (err: any) {
			appToast.apiError(err, 'Failed to mark as unread');
		}
	}

	function removeNotificationFromList(id: string) {
		notifications = notifications.filter((n) => n.id !== id);
		if (selectedId === id) {
			selectedId = notifications[0]?.id ?? null;
			if (selectedId) selectNotification(notifications[0]);
			else selectedIssue = null;
		}
	}

	async function handleArchive(id: string) {
		try {
			await archiveNotification(id);
			removeNotificationFromList(id);
			appToast.success('Archived');
		} catch (err: any) {
			appToast.apiError(err, 'Failed to archive');
		}
	}

	async function handleUnarchive(id: string) {
		try {
			await unarchiveNotification(id);
			if (activeTab === 'archived') removeNotificationFromList(id);
			else await loadNotifications();
			appToast.success('Removed from archive');
		} catch (err: any) {
			appToast.apiError(err, 'Failed to unarchive');
		}
	}

	async function handleSnooze(id: string, hours: number) {
		const until = new Date(Date.now() + hours * 60 * 60 * 1000).toISOString();
		await snoozeUntil(id, until, `Snoozed for ${hours}h`);
	}

	async function handleSnoozeDate(id: string, date: string | null) {
		if (!date) return;
		const until = snoozeDateToTimestamp(date).toISOString();
		await snoozeUntil(id, until, `Snoozed until ${formatSnoozeDate(date)}`);
	}

	async function snoozeUntil(id: string, until: string, successMessage: string) {
		try {
			await snoozeNotification(id, until);
			removeNotificationFromList(id);
			appToast.success(successMessage);
		} catch (err: any) {
			appToast.apiError(err, 'Failed to snooze');
		}
	}

	async function handleUnsnooze(id: string) {
		try {
			await unsnoozeNotification(id);
			if (activeTab === 'snoozed') removeNotificationFromList(id);
			else await loadNotifications();
			appToast.success('Unsnoozed');
		} catch (err: any) {
			appToast.apiError(err, 'Failed to unsnooze');
		}
	}

	function openSnoozeDatePicker(notification: Notification) {
		snoozeDateNotification = notification;
	}

	function closeSnoozeDatePicker() {
		snoozeDateNotification = null;
	}

	function snoozeDateToTimestamp(date: string) {
		const [year, month, day] = date.split('-').map(Number);
		const selected = new Date(year, month - 1, day, 9, 0, 0, 0);
		const now = new Date();
		if (selected.toDateString() === now.toDateString()) {
			selected.setHours(18, 0, 0, 0);
			if (selected <= now) selected.setTime(now.getTime() + 60 * 60 * 1000);
		}
		return selected;
	}

	function formatSnoozeDate(date: string) {
		return new Date(`${date}T00:00:00`).toLocaleDateString('en-US', {
			month: 'short',
			day: 'numeric',
			year: 'numeric'
		});
	}

	// Keyboard navigation
	function handleKeydown(e: KeyboardEvent) {
		const target = e.target as HTMLElement;
		if (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.isContentEditable) return;

		const idx = notifications.findIndex((n) => n.id === selectedId);
		switch (e.key.toLowerCase()) {
			case 'j':
				e.preventDefault();
				if (idx < notifications.length - 1) selectNotification(notifications[idx + 1]);
				break;
			case 'k':
				e.preventDefault();
				if (idx > 0) selectNotification(notifications[idx - 1]);
				break;
			case 'e':
				e.preventDefault();
				if (selectedId) handleArchive(selectedId);
				break;
			case 'h':
				e.preventDefault();
				if (selectedId) handleSnooze(selectedId, 3);
				break;
		}
	}

	onMount(() => {
		document.addEventListener('keydown', handleKeydown);
		return () => document.removeEventListener('keydown', handleKeydown);
	});

</script>

<div class="flex h-full min-w-0 flex-col">
	<!-- Fixed header -->
	<div
		class="flex min-h-[49px] shrink-0 items-center justify-between gap-2 border-b border-[var(--app-border)] px-3 sm:px-4"
	>
		<div class="flex min-w-0 items-center gap-2">
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

	<!-- Main content: left list + right detail -->
	<div class="flex min-h-0 flex-1 overflow-hidden">
		<!-- Left: notification list -->
		<div class="flex w-full min-w-0 shrink-0 flex-col border-r border-[var(--app-border)] md:w-[320px]">
			<!-- Tabs -->
			<Tabs.Root value={activeTab} onValueChange={handleTabChange}>
				<Tabs.List
					class="no-scrollbar !h-auto w-full justify-start gap-1 overflow-x-auto overflow-y-hidden rounded-none border-none bg-transparent px-3 py-2 md:!h-8 md:py-0"
				>
					<Tabs.Trigger
						value="inbox"
						class="h-9 flex-none rounded-full border border-[var(--app-border)] px-3 py-1.5 text-xs text-[var(--color-text-tertiary)] shadow-none data-[state=active]:border-[var(--app-accent)]/30 data-[state=active]:bg-[var(--app-accent)]/10 data-[state=active]:text-[var(--app-accent-light)] data-[state=active]:shadow-none md:h-auto md:px-2 md:py-0.5 md:text-[11px]"
					>
						<Inbox size={12} class="mr-1" />
						Inbox
					</Tabs.Trigger>
					<Tabs.Trigger
						value="snoozed"
						class="h-9 flex-none rounded-full border border-[var(--app-border)] px-3 py-1.5 text-xs text-[var(--color-text-tertiary)] shadow-none data-[state=active]:border-[var(--app-accent)]/30 data-[state=active]:bg-[var(--app-accent)]/10 data-[state=active]:text-[var(--app-accent-light)] data-[state=active]:shadow-none md:h-auto md:px-2 md:py-0.5 md:text-[11px]"
					>
						<Clock size={12} class="mr-1" />
						Snoozed
					</Tabs.Trigger>
					<Tabs.Trigger
						value="archived"
						class="h-9 flex-none rounded-full border border-[var(--app-border)] px-3 py-1.5 text-xs text-[var(--color-text-tertiary)] shadow-none data-[state=active]:border-[var(--app-accent)]/30 data-[state=active]:bg-[var(--app-accent)]/10 data-[state=active]:text-[var(--app-accent-light)] data-[state=active]:shadow-none md:h-auto md:px-2 md:py-0.5 md:text-[11px]"
					>
						<Archive size={12} class="mr-1" />
						Archived
					</Tabs.Trigger>
				</Tabs.List>
			</Tabs.Root>

			<!-- Scrollable list -->
			<div class="flex-1 overflow-y-auto">
				{#if loading}
					<div class="flex h-32 items-center justify-center"></div>
				{:else if notifications.length === 0}
					<div class="px-4 py-8">
						<EmptyState
							title={activeTab === 'inbox'
								? 'No notifications'
								: activeTab === 'snoozed'
									? 'No snoozed'
									: 'No archived'}
							description={activeTab === 'inbox' ? "You're all caught up!" : ''}
						/>
					</div>
				{:else}
					{#each notifications as notification (notification.id)}
						{@const style = getTypeStyle(notification.type)}
						{@const Icon = style.icon}
						<ContextMenu.Root>
							<ContextMenu.Trigger>
								<div
									role="button"
									tabindex="0"
									class="flex min-h-16 w-full cursor-pointer items-start gap-2.5 px-3 py-3 text-left transition-colors {selectedId === notification.id ? 'bg-[var(--color-bg-hover)]' : ''} {notification.read_at ? 'opacity-60' : ''} hover:bg-[var(--color-bg-hover)] md:min-h-0 md:py-2.5"
									onclick={() => selectNotification(notification)}
									onkeydown={(e) => {
										if (e.key === 'Enter' || e.key === ' ') {
											e.preventDefault();
											selectNotification(notification);
										}
									}}
								>
									<div
										class="relative mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-lg"
										style="background: {style.bg};"
									>
										<Icon size={16} color={style.color} />
										{#if !notification.read_at && activeTab === 'inbox'}
											<div
												class="absolute -top-0.5 -right-0.5 h-2 w-2 rounded-full bg-[var(--app-accent)] ring-2 ring-[var(--color-bg-primary)]"
											></div>
										{/if}
									</div>
									<div class="flex-1 min-w-0">
										<p class="text-[12px] leading-snug text-[var(--color-text-primary)] line-clamp-2">
											{notification.title}
										</p>
										<div class="mt-0.5 flex items-center justify-between">
											<span class="text-[10px] font-semibold" style="color: {style.color};">
												{getNotificationTypeLabel(notification.type)}
											</span>
											<span class="shrink-0 text-[11px] tabular-nums text-[var(--color-text-secondary)]">
												{formatRelativeTime(notification.created_at)}
											</span>
										</div>
									</div>
								</div>
							</ContextMenu.Trigger>

							<ContextMenu.Content class="w-48 p-1">
								{#if notification.read_at}
									<ContextMenu.Item onclick={() => handleMarkUnread(notification.id)}>
										<span class="flex items-center gap-2"><Eye class="h-4 w-4" />Mark as unread</span>
									</ContextMenu.Item>
								{:else}
									<ContextMenu.Item onclick={() => handleMarkRead(notification.id)}>
										<span class="flex items-center gap-2"><Eye class="h-4 w-4" />Mark as read</span>
									</ContextMenu.Item>
								{/if}

								{#if activeTab === 'inbox'}
									<ContextMenu.Sub>
										<ContextMenu.SubTrigger>
											<span class="flex items-center gap-2"><AlarmClock class="h-4 w-4" />Snooze</span>
										</ContextMenu.SubTrigger>
										<ContextMenu.SubContent class="w-40 p-1">
											<ContextMenu.Item onclick={() => handleSnooze(notification.id, 1)}>1 hour</ContextMenu.Item>
											<ContextMenu.Item onclick={() => handleSnooze(notification.id, 3)}>3 hours</ContextMenu.Item>
											<ContextMenu.Item onclick={() => handleSnooze(notification.id, 24)}>Tomorrow</ContextMenu.Item>
											<ContextMenu.Separator />
											<ContextMenu.Item onclick={() => openSnoozeDatePicker(notification)}>Pick date...</ContextMenu.Item>
										</ContextMenu.SubContent>
									</ContextMenu.Sub>
									<ContextMenu.Item onclick={() => handleArchive(notification.id)}>
										<span class="flex items-center gap-2"><Archive class="h-4 w-4" />Archive</span>
									</ContextMenu.Item>
								{:else if activeTab === 'snoozed'}
									<ContextMenu.Item onclick={() => handleUnsnooze(notification.id)}>
										<span class="flex items-center gap-2"><RefreshCw class="h-4 w-4" />Unsnooze</span>
									</ContextMenu.Item>
									<ContextMenu.Item onclick={() => handleArchive(notification.id)}>
										<span class="flex items-center gap-2"><Archive class="h-4 w-4" />Archive</span>
									</ContextMenu.Item>
								{:else if activeTab === 'archived'}
									<ContextMenu.Item onclick={() => handleUnarchive(notification.id)}>
										<span class="flex items-center gap-2"><RefreshCw class="h-4 w-4" />Remove from archive</span>
									</ContextMenu.Item>
								{/if}
							</ContextMenu.Content>
						</ContextMenu.Root>
					{/each}
				{/if}
			</div>
		</div>

		<!-- Right: issue detail -->
		<div class="hidden flex-1 overflow-hidden md:block">
			{#if issueLoading}
				<div class="flex h-full items-center justify-center"></div>
			{:else if selectedIssue}
				{#key selectedIssue.id}
					<FullPageIssueView
						issue={selectedIssue}
						{slug}
						onupdated={(updated) => {
							selectedIssue = updated;
						}}
					/>
				{/key}
			{:else if selectedNotification}
				<div class="flex h-full flex-col items-center justify-center gap-2 text-[var(--color-text-tertiary)]">
					<p class="text-sm">{selectedNotification.title}</p>
					<p class="text-xs">This notification is not linked to an issue.</p>
				</div>
			{:else}
				<div class="flex h-full items-center justify-center">
					<p class="text-sm text-[var(--color-text-tertiary)]">Select a notification to view details</p>
				</div>
			{/if}
		</div>
	</div>
</div>

{#if snoozeDateNotification}
	<div class="fixed inset-0 z-50 flex items-start justify-center px-3 pt-[12vh]" role="dialog" aria-modal="true">
		<button
			class="fixed inset-0 cursor-default bg-black/50"
			onclick={closeSnoozeDatePicker}
			tabindex={-1}
			aria-label="Close snooze date picker"
		></button>

		<div class="relative z-10 w-full max-w-[31rem] overflow-hidden rounded-xl border border-[var(--app-border)] bg-[var(--color-bg-secondary)] shadow-2xl">
			<div class="flex items-center justify-between gap-3 border-b border-[var(--app-border)] px-4 py-3">
				<div>
					<h2 class="text-sm font-medium text-[var(--color-text-primary)]">Snooze notification</h2>
					<p class="line-clamp-1 text-xs text-[var(--color-text-tertiary)]">{snoozeDateNotification.title}</p>
				</div>
				<button
					onclick={closeSnoozeDatePicker}
					class="inline-flex h-8 w-8 items-center justify-center rounded-md text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]"
					title="Close"
				>
					X
				</button>
			</div>

			<DueDatePickerPanel
				value={null}
				onchange={(date) => handleSnoozeDate(snoozeDateNotification!.id, date)}
				clearLabel="Cancel"
				close={closeSnoozeDatePicker}
			/>
		</div>
	</div>
{/if}
