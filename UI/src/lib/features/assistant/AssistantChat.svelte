<script lang="ts">
	import ChatComposer from './ChatComposer.svelte';
	import PageTitle from '$lib/components/shared/PageTitle.svelte';
	import { onMount, tick, untrack } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import {
		RefreshCw,
		MessageCircle,
		ArrowDown,
		Bot,
		ChevronDown,
		ChevronsDownUp,
		ChevronsUpDown,
		PanelLeft,
		PanelRight,
		Pencil,
		SquarePen,
		FileDiff,
		ListChecks,
		Workflow as WorkflowIcon,
		NotebookText,
		ClipboardCheck,
		FolderTree,
		EllipsisVertical
	} from '@lucide/svelte';
	import { cn } from '$lib/utils.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import * as Message from '$lib/components/ui/message/index.js';
	import * as Bubble from '$lib/components/ui/bubble/index.js';
	import * as MessageScroller from '$lib/components/ui/message-scroller/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import * as Sheet from '$lib/components/ui/sheet/index.js';
	import * as Resizable from '$lib/components/ui/resizable/index.js';
	import { conversations, type Conversation, type AssistantStatus, type MessageStatus } from '$lib/api/conversations';
	import { agentTasks, type AgentQueueItem } from '$lib/api/agent-tasks';
	import { ConversationState } from './conversation-state.svelte';
	import { conversationSubject, onScreenSubjects } from '$lib/features/notifications/on-screen-subjects';
	import { TaskBoard } from './task-board.svelte';
	import MessageAttachments from './MessageAttachments.svelte';
	import ToolCalls from './ToolCalls.svelte';
	import AgentChatMessage from './AgentChatMessage.svelte';
	import CopyMessageButton from './CopyMessageButton.svelte';
	import { markdownToPlainText } from './message-text';
	import ToolSources from './ToolSources.svelte';
	import Markdown from './Markdown.svelte';
	import LiveStatus from './LiveStatus.svelte';
	import ScrollerHandle from './ScrollerHandle.svelte';
	import TaskComposer from './TaskComposer.svelte';
	import WikiReferenceChip from '$lib/features/wiki/WikiReferenceChip.svelte';
	import { chatReferenceParts } from '$lib/features/wiki/wiki-references';
	import TaskStep from './TaskStep.svelte';
	import AgentThread from './AgentThread.svelte';
	import { sendOptions } from './agent-conversation';
	import { indicatorDot } from './task-tone';
	import { taskLabel } from './task-labels';
	import AssistantSidebar from './AssistantSidebar.svelte';
	import AssistantRail from './AssistantRail.svelte';
	import TaskColumn from './TaskColumn.svelte';
	import SidePanelHost from './panels/SidePanelHost.svelte';
	import OverviewPanel from './panels/OverviewPanel.svelte';
	import AgentPanel from './panels/AgentPanel.svelte';
	import DiffSidePanel from './panels/DiffSidePanel.svelte';
	import WorkflowPanel from './panels/WorkflowPanel.svelte';
	import FilesPanel from './panels/FilesPanel.svelte';
	import NotebookPanel from './panels/NotebookPanel.svelte';
	import ChecksPanel from './panels/ChecksPanel.svelte';
	import { PanelHost, type PanelContext, type PanelDefinition } from './panels/panel-host.svelte';
	import {
		conversationIndicator,
		placeByTime,
		taskIndicator,
		visibleHistory,
		type TaskIndicator
	} from './shell-rules';
	import { readRecents, recentsKey, writeRecents } from './recents-cache';
	import { keepChoice, readChoice } from './layout-choice';
	import { MediaQuery } from 'svelte/reactivity';
	import { authState } from '$lib/features/auth/auth.state.svelte';
	import { agentInfo } from '$lib/features/agents/agents';
	import { appToast } from '$lib/features/toast/toast';
	import { useTerminalDock } from '$lib/features/dev-machines/terminal-dock-context.svelte';
	import type { AgentLiveChatMessage, AgentTask } from '$lib/api/agent-tasks';
	import { m } from '$lib/features/i18n/messages';

	// The assistant page: a three-column shell. Left, the assistant's own
	// sidebar (places, search, folders and recents); centre, the conversation
	// with the orchestrator assistant, tool and task steps, and pending approvals
	// above the composer; right, a resizable side panel host
	// (details, the coding agents' threads, a diff, and future panels). While a
	// task is open (the agent view) the sidebar folds to a rail and the task's
	// column (phases, members, budget) sits left of its room. Below the
	// breakpoints the sidebars and the task column become drawers.
	let { slug, conversationId }: { slug: string; conversationId: string | null } = $props();
	// This component is keyed by workspace, conversation and authenticated user.
	const chat = untrack(() => new ConversationState(slug, conversationId ?? ''));
	// While this conversation is displayed, its own approval requests are in
	// view: the notifier stays quiet for them (see on-screen-subjects.ts).
	$effect(() => (conversationId ? onScreenSubjects.show(conversationSubject(conversationId)) : undefined));
	const userKey = untrack(() => `${authState.user?.id}:${slug}`);
	let status = $state<AssistantStatus>({ configured: false });
	let initialized = $state(false);
	const pageSize = 30;
	const cacheKey = untrack(() => recentsKey(authState.user?.id, slug));
	let history = $state<Conversation[]>(untrack(() => readRecents(cacheKey) ?? []));
	let matches = $state<Conversation[]>([]);
	let search = $state('');
	let folder = $state<string | null>(null);
	const listQuery = $derived({ search, folder });
	const filtered = $derived(!!search || !!folder);
	const listed = $derived(filtered ? matches : history);
	let loadingList = $state(true);
	let listFailed = $state(false);
	let nextPage = $state<Conversation>();
	function updateHistory(update: (items: Conversation[]) => Conversation[]) {
		history = update(history);
		matches = update(matches).filter((c) =>
			(!folder || c.folder_id === folder) && c.title.toLocaleLowerCase().includes(search.toLocaleLowerCase())
		);
		writeRecents(cacheKey, history.slice(0, pageSize));
	}

	async function loadList(query: typeof listQuery, before?: Conversation) {
		loadingList = true;
		listFailed = false;
		try {
			const items = await conversations.list(slug, pageSize + 1, query.search, query.folder, before);
			if (disposed || query !== listQuery) return;
			const page = items.slice(0, pageSize);
			let itemsToShow = page;
			if (before) {
				const seen = new Set(listed.map((item) => item.id));
				itemsToShow = [...listed, ...page.filter((item) => !seen.has(item.id))];
			}
			if (filtered) matches = itemsToShow;
			else {
				history = itemsToShow;
				writeRecents(cacheKey, history.slice(0, pageSize));
			}
			nextPage = items.length > pageSize ? page.at(-1) : undefined;
		} catch {
			if (!disposed && query === listQuery) listFailed = true;
		} finally {
			if (!disposed && query === listQuery) loadingList = false;
		}
	}

	$effect(() => {
		const query = listQuery;
		nextPage = undefined;
		matches = [];
		loadingList = true;
		listFailed = false;
		const timer = setTimeout(() => void loadList(query), query.search ? 150 : 0);
		return () => clearTimeout(timer);
	});

	// ── Layout: fixed sidebar from lg, inline side panel from xl, drawers below.
	// The URL names the task the agent view shows (?agent=, see below).
	const agentParam = $derived(page.url.searchParams.get('agent'));
	const agentView = $derived(!!agentParam && !!conversationId);
	const large = new MediaQuery('min-width: 1024px');
	const wide = new MediaQuery('min-width: 1280px');
	const sidebarKey = untrack(() => `assistant-sidebar:${userKey}`);
	let sidebarCollapsed = $state(untrack(() => readChoice(sidebarKey) === 'collapsed'));
	// While a task is open the sidebar folds to its rail unless the person
	// opens it there; that choice is kept apart from the chat's (KUA-281).
	const taskSidebarKey = untrack(() => `assistant-sidebar-task:${userKey}`);
	let taskSidebarOpen = $state(untrack(() => readChoice(taskSidebarKey) === 'open'));
	let sidebarDrawer = $state(false);
	const sidebarRail = $derived(large.current && agentView && !taskSidebarOpen);
	const sidebarInline = $derived(large.current && (agentView ? taskSidebarOpen : !sidebarCollapsed));
	function setSidebarCollapsed(value: boolean) {
		if (agentView) {
			taskSidebarOpen = !value;
			keepChoice(taskSidebarKey, value ? 'rail' : 'open');
			return;
		}
		sidebarCollapsed = value;
		keepChoice(sidebarKey, value ? 'collapsed' : 'open');
	}
	function showSidebar() {
		if (large.current) setSidebarCollapsed(false);
		else sidebarDrawer = true;
	}
	// The side panel opens by itself on wide screens ('auto' picks the agents
	// of an issue chat, otherwise the details); the person's choice is kept.
	const host = untrack(() => new PanelHost(`assistant-panel:${userKey}`, wide.current ? 'auto' : null));
	let board = $state<TaskBoard | null>(null);
	const panels: PanelDefinition[] = [
		{ id: 'details', label: m['assistant.panel_details'], icon: PanelRight, component: OverviewPanel },
		{ id: 'agent', label: m['assistant.panel_agents'], icon: Bot, component: AgentPanel, available: () => !!chat.issue },
		{ id: 'diff', label: m['assistant.panel_diff'], icon: FileDiff, component: DiffSidePanel, available: () => !!chat.diffView },
		// The task's workflow (KUA-95): its graph, current step and path.
		{ id: 'workflow', label: m['assistant.panel_workflow'], icon: WorkflowIcon, component: WorkflowPanel, available: () => !!chat.issue },
		// The task's files at its head, with a preview and its diff (KUA-285).
		{ id: 'files', label: m['tasks.files_panel'], icon: FolderTree, component: FilesPanel, available: () => !!chat.issue, scrolls: true },
		// The open task's notebook, for its creator, and its pipeline's checks (KUA-286).
		{
			id: 'notebook',
			label: m['notebook.title'],
			icon: NotebookText,
			component: NotebookPanel,
			available: () => !!agentTask && agentTask.kind !== 'review' && agentTask.creator_id === authState.user?.id
		},
		{ id: 'checks', label: m['assistant.panel_checks'], icon: ClipboardCheck, component: ChecksPanel, available: () => !!agentTask?.pipeline }
	];
	const availablePanels = $derived(panels.filter((panel) => panel.available?.() ?? true));
	// The top bar's panel buttons (a diff opens from a file, not from the bar).
	// More than two do not fit beside the title in a narrow bar: there it shows
	// the open panel and a "More panels" menu (KUA-296).
	const panelToggles = $derived(availablePanels.filter((panel) => panel.id !== 'diff'));
	const panelsOverflow = $derived(panelToggles.length > 2);
	const activePanel = $derived.by(() => {
		if (!host.active) return null;
		const id = host.active === 'auto' ? (chat.issue ? 'agent' : 'details') : host.active;
		return availablePanels.find((panel) => panel.id === id) ?? availablePanels[0];
	});
	const panelInline = $derived(wide.current && !!activePanel);
	function closePanel() {
		if (activePanel?.id === 'diff') chat.diffView = null;
		host.close();
	}
	// Narrowing the window never pops a drawer open: the inline panel closes
	// (without forgetting the person's choice for wide screens).
	$effect(() => {
		if (!wide.current) untrack(() => (host.active = null));
	});
	// A step of a task's path opens in the agent view, which a drawer would
	// cover: on narrow screens following the link closes it.
	let pathTarget = untrack(() => `${page.url.searchParams.get('agent') ?? ''}${page.url.hash}`);
	$effect(() => {
		const target = `${page.url.searchParams.get('agent') ?? ''}${page.url.hash}`;
		if (target === pathTarget) return;
		pathTarget = target;
		if (target && !wide.current)
			untrack(() => {
				host.active = null;
				taskSheet = false;
			});
	});
	// A diff asked for from a task, a step or the overview opens in the panel.
	$effect(() => {
		if (chat.diffView) untrack(() => host.open('diff'));
	});
	// A task asked for (step, background row, ?task=) opens the agents panel.
	$effect(() => {
		if (chat.focusedTask && chat.issue) untrack(() => host.open('agent'));
	});
	// ── Agent view: the centre column shows one coding task's agent thread.
	// The URL names it (?agent=<task id or issue identifier>), so reloads and
	// links open the same view; leaving it returns to the assistant chat.
	const agentTasksList = $derived(
		board?.tasks ??
			Object.values(chat.liveTasks).sort(
				(a, b) => Date.parse(b.created_at) - Date.parse(a.created_at) || b.id.localeCompare(a.id)
			)
	);
	// A review a workflow step started is a task of its own that the
	// conversation does not list: its step links to it, so it is read by id
	// (the server decides who may) and again whenever a review changes.
	let reviewTask = $state<AgentTask | undefined>();
	$effect(() => {
		const id = agentParam;
		void chat.taskReviewCursor;
		if (!id || !conversationId || chat.loading || chat.liveTasks[id] || id.toLowerCase() === chat.issue?.identifier.toLowerCase()) {
			untrack(() => (reviewTask = undefined));
			return;
		}
		let stale = false;
		agentTasks
			.get(slug, id)
			.then((task) => {
				if (!stale && task.id === id && task.kind === 'review' && task.conversation_id === conversationId)
					reviewTask = task;
			})
			.catch(() => {});
		return () => (stale = true);
	});
	const agentTask = $derived.by(() => {
		if (!agentParam) return undefined;
		const byId = chat.liveTasks[agentParam] ?? (reviewTask?.id === agentParam ? reviewTask : undefined);
		if (byId) return byId;
		return chat.issue && agentParam.toLowerCase() === chat.issue.identifier.toLowerCase()
			? agentTasksList[0]
			: undefined;
	});
	const assistantHref = $derived(
		conversationId ? `/${slug}/assistant?conversation=${encodeURIComponent(conversationId)}` : `/${slug}/assistant`
	);
	function openAgent(task: AgentTask) {
		const url = new URL(page.url.href);
		url.searchParams.set('agent', task.id);
		url.searchParams.delete('task');
		void goto(`${url.pathname}${url.search}`, { reset: false });
	}
	function closeAgent() {
		void goto(assistantHref, { reset: false });
	}
	// ── Task column (KUA-281): left of the task's room on wide screens, a
	// drawer below. Whether it shows is the person's choice and is kept;
	// narrowing the window never pops the drawer open.
	const taskColumnKey = untrack(() => `assistant-task-column:${userKey}`);
	let taskColumnOpen = $state(untrack(() => readChoice(taskColumnKey) !== 'closed'));
	let taskSheet = $state(false);
	const taskColumnInline = $derived(wide.current && taskColumnOpen && !!agentTask);
	function toggleTaskColumn() {
		if (!wide.current) {
			taskSheet = !taskSheet;
			return;
		}
		taskColumnOpen = !taskColumnOpen;
		keepChoice(taskColumnKey, taskColumnOpen ? 'open' : 'closed');
	}
	// Closed from its own header, the column takes the focus with it: the
	// top bar's toggle, which opens it again, gets it.
	let taskToggle = $state<HTMLElement | null>(null);
	function hideTaskColumn() {
		if (taskColumnOpen) toggleTaskColumn();
		void tick().then(() => taskToggle?.focus());
	}
	$effect(() => {
		if (wide.current || !agentView) untrack(() => (taskSheet = false));
	});
	// The room takes the width while no side panel sits beside it.
	const roomWide = $derived(agentView && !panelInline);
	const requestedTask = untrack(() => page.url.searchParams.get('task'));
	$effect(() => {
		const wanted = page.url.searchParams.get('task') ?? requestedTask;
		if (wanted && chat.liveTasks[wanted]) untrack(() => (chat.focusedTask = wanted));
	});

	// ── Agent work across the person's conversations, for the details panel
	// and the sidebar's row indicators.
	const dock = useTerminalDock();
	let queue = $state<AgentQueueItem[]>([]);
	async function loadQueue() {
		if (typeof document !== 'undefined' && document.hidden) return;
		try {
			const next = await agentTasks.queue(slug);
			if (!disposed) queue = Array.isArray(next) ? next : [];
			if (queue.length) dock?.trackAgents();
		} catch {
			// The queue is a convenience; the conversations remain authoritative.
		}
	}
	// This conversation's own tasks are shown at once, before the next poll.
	const queueItems = $derived.by(() => {
		const known = new Set(queue.map((item) => item.id));
		const own: AgentQueueItem[] = Object.values(chat.liveTasks)
			.filter((task) => !known.has(task.id))
			.map((task) => ({
				id: task.id,
				conversation_id: task.conversation_id ?? chat.id,
				state: task.state,
				reason: task.reason,
				issue_identifier: chat.issue?.identifier ?? '',
				issue_title: chat.issue?.title ?? '',
				provider: task.provider,
				updated_at: task.updated_at
			}));
		return [...own, ...queue];
	});
	const indicators = $derived.by(() => {
		const result: Record<string, TaskIndicator | undefined> = {};
		for (const conversation of listed) result[conversation.id] = conversationIndicator(conversation.id, queueItems);
		if (conversationId && chat.busy && !result[conversationId]) result[conversationId] = 'running';
		return result;
	});

	let composer = $state<ChatComposer>();
	let sending = $state(false);
	let error = $state('');
	let scroller = $state<ReturnType<typeof MessageScroller.useMessageScroller>>();
	// Reloads after sending keep the transcript, header and task on screen, so
	// the reader keeps their place; only the first load shows the loading state.
	const firstLoad = $derived(chat.loading && !chat.messages.length);

	// ── Earlier history folds away behind one row; the choice is remembered.
	const collapseKey = untrack(() => `assistant-collapse-history:${userKey}`);
	let historyCollapsed = $state(
		untrack(() => {
			try {
				return localStorage.getItem(collapseKey) === 'collapsed';
			} catch {
				return false;
			}
		})
	);
	function setHistoryCollapsed(value: boolean) {
		historyCollapsed = value;
		try {
			localStorage.setItem(collapseKey, value ? 'collapsed' : 'expanded');
		} catch {
			/* Folding still works without storage. */
		}
	}
	const KEEP = 6;
	const shown = $derived(visibleHistory(chat.messages, historyCollapsed, KEEP));

	// Agent messages and coding tasks sit between the conversation's messages
	// by time; they are never conversation messages themselves.
	type TimelineItem =
		| { kind: 'agent'; id: string; created_at: string; message: AgentLiveChatMessage }
		| { kind: 'task'; id: string; created_at: string; task: AgentTask };
	const timeline = $derived(
		placeByTime<(typeof chat.messages)[number], TimelineItem>(chat.messages, [
			...chat.agentMessages.map((message) => ({
				kind: 'agent' as const,
				id: message.id,
				created_at: message.created_at,
				message
			})),
			...Object.values(chat.liveTasks).map((task) => ({
				kind: 'task' as const,
				id: `task:${task.id}`,
				created_at: task.created_at,
				task
			}))
		])
	);

	// ── Composer target: the assistant, or one of this chat's coding agents.
	const messageable = $derived((board?.tasks ?? []).filter(task => sendOptions(task, authState.user?.id).open));
	const targetTask = $derived(agentView ? agentTask : chat.composerTarget ? chat.liveTasks[chat.composerTarget] : undefined);
	function chooseTarget(id: string | null) {
		chat.composerTarget = id;
		if (agentView && id !== agentTask?.id) {
			const task = id && chat.liveTasks[id];
			if (task) openAgent(task);
			else closeAgent();
		}
	}
	const agentName = (task: AgentTask) => agentInfo(task.provider).name;

	function followLatest() {
		scroller?.scrollToEnd({ behavior: 'auto' });
	}
	let retryRequest: { message: string; id: string } | null = null;
	let disposed = false;
	const suggestions = $derived(
		chat.issue
			? [
					m['assistant.suggest_issue_summary']({ identifier: chat.issue.identifier }),
					m['assistant.suggest_issue_next']({ identifier: chat.issue.identifier }),
					m['assistant.suggest_issue_delegate']({ identifier: chat.issue.identifier })
				]
			: [m['assistant.suggest_review'](), m['assistant.suggest_create'](), m['assistant.suggest_teams']()]
	);
	// An issue chat's coding tasks load once its issue is known.
	$effect(() => {
		if (!chat.issue || board || chat.unavailable) return;
		untrack(() => {
			board = new TaskBoard(chat);
			board.start();
		});
	});
	$effect(() => {
		if (chat.unavailable && board) untrack(() => (board = null));
	});

	onMount(() => {
		// An agent's question needs the reader: say so even when it is scrolled away.
		chat.onAgentQuestion = (question) =>
			appToast.warning(m['assistant.agent_question_toast']({ identifier: question.identifier }), {
				description: question.body.length > 140 ? `${question.body.slice(0, 140)}…` : question.body,
				duration: 10000
			});
		void conversations.status(slug)
			.then((provider) => {
				if (!disposed) {
					status = provider;
					initialized = true;
				}
			})
			.catch(() => { if (!disposed) error = m['assistant.error'](); });
		if (conversationId) void chat.load();
		else chat.loading = false;
		void loadQueue();
		const queueTimer = setInterval(() => void loadQueue(), 20_000);
		return () => {
			disposed = true;
			chat.dispose();
			board?.dispose();
			clearInterval(queueTimer);
		};
	});
	async function retry(message: string) {
		if (!conversationId || sending) return;
		sending = true;
		error = '';
		if (retryRequest?.message !== message) retryRequest = { message, id: crypto.randomUUID() };
		followLatest();
		try {
			const accepted = await conversations.retry(slug, conversationId, message, retryRequest.id);
			chat.accept(accepted);
			retryRequest = null;
			await chat.load();
		} catch {
			error = m['assistant.error']();
		} finally {
			sending = false;
		}
	}
	function statusLabel(value: MessageStatus) {
		switch (value) {
			case 'queued':
				return m['assistant.queued']();
			case 'streaming':
				return m['assistant.streaming']();
			case 'completed':
				return m['assistant.completed']();
			case 'failed':
				return m['assistant.failed']();
			case 'interrupted':
				return m['assistant.interrupted']();
			case 'cancelled':
				return m['assistant.cancelled']();
		}
	}

	// ── Title: rename in place, or switch to another recent conversation.
	const title = $derived(chat.issue?.title ?? chat.conversation?.title ?? m['assistant.new']());
	let renaming = $state<string | null>(null);
	let savingTitle = false;
	function renamed(updated: Conversation) {
		if (chat.conversation?.id === updated.id) chat.conversation = { ...chat.conversation, title: updated.title };
	}
	async function saveTitle() {
		const next = renaming?.trim();
		if (renaming === null || savingTitle) return;
		if (!next || !conversationId || next === chat.conversation?.title) {
			renaming = null;
			return;
		}
		savingTitle = true;
		try {
			const updated = await conversations.rename(slug, conversationId, next);
			updateHistory((items) => items.map((c) => (c.id === updated.id ? { ...c, title: updated.title } : c)));
			renamed(updated);
			renaming = null;
		} catch (e) {
			appToast.apiError(e, m['assistant.rename_failed']());
		} finally {
			savingTitle = false;
		}
	}
	const focusInput = (node: HTMLInputElement) => {
		node.focus();
		node.select();
	};
	const recent = $derived(history.filter((c) => c.id !== conversationId).slice(0, 8));

	const panelContext: PanelContext = {
		slug: untrack(() => slug),
		chat,
		get board() {
			return board;
		},
		get queue() {
			return queueItems;
		},
		close: closePanel,
		open: (id) => host.open(id),
		draft: (value) => (chat.composerDraft = value),
		get agentTask() {
			return agentTask;
		}
	};
	const sidebarProps = $derived({
		slug,
		conversationId,
		history: listed,
		onchange: updateHistory,
		loading: loadingList,
		failed: listFailed,
		hasMore: !!nextPage,
		onmore: () => { if (!loadingList) void loadList(listQuery, nextPage); },
		indicators,
		onrenamed: renamed
	});
</script>

<PageTitle parts={[m['assistant.title']()]} />

{#snippet topBar()}
	<!-- A container: the panel buttons fold into a menu below 36rem of the bar's own width
	     (a phone, or the room between the task column and an open side panel). -->
	<header class="@container/topbar flex h-12 shrink-0 items-center gap-1 border-b px-2 md:px-3">
		{#if !sidebarInline && !sidebarRail}<Button
				variant="ghost"
				size="icon-sm"
				aria-label={m['assistant.show_sidebar']()}
				title={m['assistant.show_sidebar']()}
				aria-expanded={sidebarDrawer}
				onclick={showSidebar}><PanelLeft /></Button
			>{/if}
		{#if agentView && agentTask}<Button
				bind:ref={taskToggle}
				variant="ghost"
				size="icon-sm"
				aria-label={m['assistant.task_column']()}
				title={m['assistant.task_column']()}
				aria-pressed={wide.current ? taskColumnInline : undefined}
				aria-expanded={wide.current ? undefined : taskSheet}
				class={taskColumnInline ? 'bg-muted text-foreground' : ''}
				onclick={toggleTaskColumn}><ListChecks /></Button
			>{/if}
		<div class="flex min-w-0 flex-1 items-center gap-1.5">
			{#if renaming !== null}
				<Input
					aria-label={m['assistant.conversation_name']()}
					maxlength={200}
					class="h-8 max-w-md min-w-0 flex-1 text-sm"
					bind:value={renaming}
					onkeydown={(event) => {
						if (event.key === 'Enter') {
							event.preventDefault();
							void saveTitle();
						} else if (event.key === 'Escape') {
							event.preventDefault();
							renaming = null;
						}
					}}
					onblur={() => void saveTitle()}
					{@attach focusInput}
				/>
			{:else}
				<h1 class="min-w-0">
					<DropdownMenu.Root>
						<DropdownMenu.Trigger>
							{#snippet child({ props })}
								<Button
									{...props}
									variant="ghost"
									size="sm"
									class="h-8 max-w-full min-w-0 justify-start gap-1 px-2 text-sm font-semibold"
									aria-label={m['assistant.title_menu']({ title })}
									><span class="truncate">{title}</span><ChevronDown
										class="size-3.5 shrink-0 text-muted-foreground"
									/></Button
								>
							{/snippet}
						</DropdownMenu.Trigger>
						<DropdownMenu.Content align="start" class="w-64">
							{#if conversationId && !chat.unavailable}<DropdownMenu.Item
									onSelect={() => (renaming = chat.conversation?.title ?? '')}
									><Pencil />{m['assistant.rename_conversation']()}</DropdownMenu.Item
								>{/if}
							<DropdownMenu.Item onSelect={() => void goto(`/${slug}/assistant`)}
								><SquarePen />{m['assistant.new']()}</DropdownMenu.Item
							>
							{#if recent.length}
								<DropdownMenu.Separator />
								<DropdownMenu.Label class="text-xs font-normal text-muted-foreground"
									>{m['assistant.switch_to']()}</DropdownMenu.Label
								>
								{#each recent as item (item.id)}<DropdownMenu.Item
										onSelect={() => void goto(`/${slug}/assistant?conversation=${encodeURIComponent(item.id)}`)}
										><MessageCircle /><span class="truncate">{item.title}</span></DropdownMenu.Item
									>{/each}
							{/if}
						</DropdownMenu.Content>
					</DropdownMenu.Root>
				</h1>
			{/if}
			{#if chat.issue}<a
					class="hidden shrink-0 rounded-md border px-1.5 py-0.5 text-xs text-muted-foreground hover:text-foreground sm:inline"
					href="/{slug}/issue/{encodeURIComponent(chat.issue.identifier)}">{chat.issue.identifier}</a
				>{/if}
			{#if agentTasksList.length && chat.issue}{@render agentSwitch()}{/if}
		</div>
		{#if conversationId && !firstLoad && !chat.unavailable}<LiveStatus
				connected={chat.connected}
				busy={chat.busy}
				offline={chat.failed || chat.unavailable}
			/>{/if}
		<div class="flex shrink-0 items-center gap-0.5" role="group" aria-label={m['assistant.panels']()}>
			{#each panelToggles as panel (panel.id)}
				{@const open = activePanel?.id === panel.id}
				<Button
					variant="ghost"
					size="icon-sm"
					aria-label={panel.label()}
					title={panel.label()}
					aria-pressed={open}
					class={cn(open && 'bg-muted text-foreground', panelsOverflow && !open && 'hidden @xl/topbar:inline-flex')}
					onclick={() => (open ? closePanel() : host.open(panel.id))}
					><panel.icon /></Button
				>
			{/each}
			{#if panelsOverflow}
				<DropdownMenu.Root>
					<DropdownMenu.Trigger>
						{#snippet child({ props })}
							<Button
								{...props}
								variant="ghost"
								size="icon-sm"
								class="@xl/topbar:hidden"
								aria-label={m['assistant.more_panels']()}
								title={m['assistant.more_panels']()}><EllipsisVertical /></Button
							>
						{/snippet}
					</DropdownMenu.Trigger>
					<DropdownMenu.Content align="end" class="w-48">
						<DropdownMenu.Label class="text-xs font-normal text-muted-foreground">{m['assistant.panels']()}</DropdownMenu.Label>
						{#each panelToggles as panel (panel.id)}
							<DropdownMenu.CheckboxItem
								checked={activePanel?.id === panel.id}
								onCheckedChange={(checked) => (checked ? host.open(panel.id) : closePanel())}
								><panel.icon />{panel.label()}</DropdownMenu.CheckboxItem
							>
						{/each}
					</DropdownMenu.Content>
				</DropdownMenu.Root>
			{/if}
		</div>
	</header>
{/snippet}

{#snippet statusDot(task: AgentTask)}
	<span
		role="img"
		aria-label={taskLabel(task.state)}
		class="size-2 shrink-0 rounded-full {indicatorDot[taskIndicator(task.state) ?? 'done']}"
	></span>
{/snippet}

{#snippet agentSwitch()}
	{@const label = m['assistant.agent_view_button']({ identifier: chat.issue?.identifier ?? '' })}
	{@const shownTask = agentTask ?? agentTasksList[0]}
	{#if agentTasksList.length === 1}
		<Button
			variant={agentView ? 'secondary' : 'outline'}
			size="sm"
			class="h-7 max-w-[60%] min-w-0 shrink gap-1.5 rounded-full px-2.5 text-xs"
			aria-pressed={agentView}
			onclick={() => (agentView ? closeAgent() : openAgent(agentTasksList[0]))}
			><Bot size={13} /><span class="truncate">{label}</span>{@render statusDot(shownTask)}</Button
		>
	{:else}
		<DropdownMenu.Root>
			<DropdownMenu.Trigger>
				{#snippet child({ props })}
					<Button
						{...props}
						variant={agentView ? 'secondary' : 'outline'}
						size="sm"
						class="h-7 max-w-[60%] min-w-0 shrink gap-1.5 rounded-full px-2.5 text-xs"
						><Bot size={13} /><span class="truncate">{label}</span>{@render statusDot(shownTask)}<ChevronDown
							size={12}
							class="text-muted-foreground"
						/></Button
					>
				{/snippet}
			</DropdownMenu.Trigger>
			<DropdownMenu.Content align="start" class="w-72">
				<DropdownMenu.Label class="text-xs font-normal text-muted-foreground"
					>{m['assistant.agent_view_choose']()}</DropdownMenu.Label
				>
				{#each agentTasksList as task (task.id)}<DropdownMenu.CheckboxItem
						checked={agentView && agentTask?.id === task.id}
						onCheckedChange={() => openAgent(task)}
						>{@render statusDot(task)}<span class="truncate"
							>{m['assistant.agent_view_option']({
								identifier: chat.issue?.identifier ?? '',
								agent: agentName(task),
								attempt: m['tasks.attempt']({ number: task.generation }),
								state: taskLabel(task.state)
							})}</span
						></DropdownMenu.CheckboxItem
					>{/each}
				{#if agentView}<DropdownMenu.Separator /><DropdownMenu.Item onSelect={closeAgent}
						><MessageCircle />{m['assistant.target_back']()}</DropdownMenu.Item
					>{/if}
			</DropdownMenu.Content>
		</DropdownMenu.Root>
	{/if}
{/snippet}

{#snippet userMessage(message: (typeof chat.messages)[number])}
	<Message.Root align="end" class="group">
		<Message.Content class="items-end">
			<Message.Header class="sr-only">{m['assistant.you']()}</Message.Header>
			{#if message.attachments?.length}<MessageAttachments
					{slug}
					conversationId={message.conversation_id}
					attachments={message.attachments}
				/>{/if}
			{#if message.content}
				<Bubble.Root variant="muted">
					<Bubble.Content class="rounded-2xl rounded-br-md ps-3.5 pe-9 py-2">
						<!-- Kept on one line: the paragraph preserves whitespace. -->
						<!-- prettier-ignore -->
						<p class="break-words whitespace-pre-wrap">{#each chatReferenceParts(message.content) as part, index (index)}{#if part.pageId}<WikiReferenceChip {slug} pageId={part.pageId} />{:else}{part.text}{/if}{/each}</p>
					</Bubble.Content>
					<CopyMessageButton text={message.content} />
				</Bubble.Root>
			{/if}
			<Message.Footer
				class="flex items-center gap-1 text-[11px] text-muted-foreground opacity-70 transition-opacity group-hover:opacity-100 focus-within:opacity-100"
			>
				<time datetime={message.created_at}
					>{new Date(message.created_at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}</time
				>
			</Message.Footer>
		</Message.Content>
	</Message.Root>
{/snippet}

{#snippet assistantMessage(message: (typeof chat.messages)[number])}
	<Message.Root align="start" class="group">
		<Message.Content class="w-full">
			<Message.Header class="sr-only"
				>{m['assistant.title']()}{#if message.model}{' '}· {message.model}{/if}</Message.Header
			>
			<ToolCalls calls={chat.tools[message.id] ?? []} slug={chat.slug} approvals={chat.approvals} part="steps" /><ToolSources
				calls={chat.tools[message.id] ?? []}
				slug={chat.slug}
			/>
			<Bubble.Root variant="ghost">
				<Bubble.Content data-selectable="text" class="pe-8! min-h-6">
					<Markdown source={message.content} richBlocks={message.status !== 'streaming'} />
				</Bubble.Content>
				{#if message.content}<CopyMessageButton
						class="top-0 end-0"
						text={() => markdownToPlainText(message.content)}
					/>{/if}
			</Bubble.Root>
			<ToolCalls calls={chat.tools[message.id] ?? []} slug={chat.slug} approvals={chat.approvals} part="approvals" />
			{#if message.status !== 'completed' || !(chat.tools[message.id] ?? []).some(call => chat.approvals[call.id]?.status === 'pending')}
			<Message.Footer class="flex flex-wrap items-center gap-1.5 text-[11px] text-muted-foreground">
				<time datetime={message.created_at}
					>{new Date(message.created_at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}</time
				>
				<span role={message.status === 'streaming' ? 'status' : undefined}>{statusLabel(message.status)}</span>
				{#if message.model}<span class="hidden sm:inline">· {message.model}</span>{/if}
				{#if ['failed', 'interrupted', 'cancelled'].includes(message.status) && message.id === chat.messages.at(-1)?.id}
					<Button variant="ghost" size="sm" class="h-6 px-2 text-xs" disabled={sending} onclick={() => void retry(message.id)}
						><RefreshCw size={12} />{m['assistant.retry']()}</Button
					>
				{/if}
			</Message.Footer>
			{/if}
		</Message.Content>
	</Message.Root>
{/snippet}

{#snippet timelineItem(item: TimelineItem)}
	<MessageScroller.Item messageId={item.id}>
		{#if item.kind === 'agent'}<AgentChatMessage message={item.message} {slug} />{:else}<TaskStep
				task={item.task}
				identifier={chat.issue?.identifier ?? ''}
				onopen={() => openAgent(item.task)}
				onretry={board ? () => board?.retry(item.task) : undefined}
			/>{/if}
	</MessageScroller.Item>
{/snippet}

{#snippet center()}
	<section class="flex h-full min-h-0 min-w-0 flex-1 flex-col" aria-label={m['assistant.conversation']()}>
		{@render topBar()}
		<div class="min-h-0 flex-1">
			<MessageScroller.Provider autoScroll>
				<ScrollerHandle bind:scroller />
				<MessageScroller.Root>
					<MessageScroller.Viewport aria-label={m['assistant.messages']()}>
						{#if agentView}<MessageScroller.Content
								class="mx-auto w-full {roomWide ? '' : 'max-w-3xl'} gap-6 px-4 pb-6 md:px-6"
							>
								<section aria-label={m['assistant.agent_view_label']({ identifier: chat.issue?.identifier ?? '' })}>
									{#if agentTask}{#key agentTask.id}<AgentThread
												{chat}
												task={agentTask}
												backHref={assistantHref}
												onback={closeAgent}
												allowedAgents={board?.options?.providers.map((provider) => provider.id) ?? []}
											/>{/key}{:else if firstLoad || (chat.issue && !chat.unavailable && (!board || board.loading))}<p
											class="pt-6 text-sm text-muted-foreground"
											role="status"
										>
											{m['assistant.loading']()}
										</p>{:else}<div class="space-y-3 pt-6">
											<p role="alert" class="text-sm text-muted-foreground">{m['assistant.agent_view_missing']()}</p>
											<Button variant="outline" size="sm" href={assistantHref}
												><MessageCircle />{m['assistant.target_back']()}</Button
											>
										</div>{/if}
								</section>
							</MessageScroller.Content>
						{:else}
						{#if chat.hasMore && !shown.hidden}<div class="mx-auto w-full max-w-3xl px-4 pt-3 md:px-6">
								<Button variant="ghost" class="w-full" disabled={chat.loadingOlder} onclick={() => void chat.older()}
									>{chat.loadingOlder ? m['assistant.loading']() : m['assistant.older']()}</Button
								>
							</div>{/if}
						{#if shown.hidden || chat.messages.length > KEEP}<div class="mx-auto flex w-full max-w-3xl justify-center px-4 pt-3 md:px-6">
								{#if shown.hidden}
									<Button
										variant="outline"
										size="sm"
										class="self-center rounded-full text-xs"
										onclick={() => setHistoryCollapsed(false)}
										><ChevronsUpDown size={13} />{m['assistant.show_earlier']({ count: shown.hidden })}</Button
									>
								{:else if chat.messages.length > KEEP}
									<Button
										variant="ghost"
										size="sm"
										class="self-center rounded-full text-xs text-muted-foreground"
										onclick={() => setHistoryCollapsed(true)}
										><ChevronsDownUp size={13} />{m['assistant.collapse_earlier']()}</Button
									>
								{/if}
							</div>{/if}
						<MessageScroller.Content class="mx-auto w-full max-w-3xl gap-6 px-4 py-6 md:px-6">
							{#if firstLoad}
								<p class="text-sm text-muted-foreground" role="status">{m['assistant.loading']()}</p>
							{:else if chat.unavailable}
								<p role="alert" class="text-sm text-muted-foreground">{m['assistant.unavailable']()}</p>
							{:else if chat.messages.length === 0 && chat.agentMessages.length === 0 && !Object.keys(chat.liveTasks).length}
								<div class="my-auto flex flex-col items-start gap-3 py-16">
									<div class="rounded-xl border bg-muted/40 p-3"><MessageCircle size={24} /></div>
									<h2 class="text-2xl font-semibold tracking-tight">{m['assistant.welcome']()}</h2>
									<p class="max-w-md text-sm leading-6 text-muted-foreground">{m['assistant.intro']()}</p>
									{#if status?.configured}
										<!-- Official AI Elements Suggestion composition: one click sends the prompt. -->
										<div class="flex max-w-full flex-wrap gap-2 pb-1">
											{#each suggestions as suggestion (suggestion)}
												<Button
													variant="outline"
													size="sm"
													class="max-w-full"
													disabled={sending}
													onclick={() => composer?.suggest(suggestion)}><span class="truncate">{suggestion}</span></Button
												>
											{/each}
										</div>
									{/if}
								</div>
							{/if}
							{#each shown.shown as message (message.id)}
								{#each timeline.before[message.id] ?? [] as item (item.id)}{@render timelineItem(item)}{/each}
								{@const tools = chat.tools[message.id] ?? []}
								{#if message.role === 'user' || message.content || message.status !== 'completed' || !tools.length || tools.some(call => chat.approvals[call.id]?.status !== 'pending')}
								<MessageScroller.Item messageId={message.id}>
									{#if message.role === 'user'}{@render userMessage(message)}{:else}{@render assistantMessage(
											message
										)}{/if}
								</MessageScroller.Item>
								{/if}
							{/each}
							{#each timeline.after as item (item.id)}{@render timelineItem(item)}{/each}
						</MessageScroller.Content>
						{/if}
					</MessageScroller.Viewport>
					<MessageScroller.Button aria-label={m['assistant.latest']()}
						><ArrowDown size={16} /><span class="sr-only">{m['assistant.latest']()}</span></MessageScroller.Button
					>
				</MessageScroller.Root>
			</MessageScroller.Provider>
		</div>
		{#if !agentView || (agentTask && sendOptions(agentTask, authState.user?.id).open)}
			<ChatComposer bind:this={composer} {chat} {conversationId} {status} {initialized}
				{targetTask} {messageable} {chooseTarget} {followLatest} bind:sending bind:error wide={roomWide}
				reveal={() => { if (!wide.current) host.close(); }} />
		{/if}

	</section>
{/snippet}

{#snippet taskColumn(onclose: () => void)}
	{#if agentTask}{#key agentTask.id}<TaskColumn {chat} task={agentTask} {onclose} />{/key}{/if}
{/snippet}

<div class="flex h-full min-h-0 min-w-0 flex-1 bg-background text-foreground" data-testid="assistant-shell">
	{#if sidebarRail}
		<AssistantRail {slug} onexpand={showSidebar} />
	{:else if sidebarInline}
		<div class="w-[272px] shrink-0 border-r" data-testid="assistant-sidebar">
			<AssistantSidebar
				{...sidebarProps}
				bind:search
				bind:folder
				collapseLabel={m['assistant.hide_sidebar']()}
				oncollapse={() => setSidebarCollapsed(true)}
			/>
		</div>
	{:else}
		<Sheet.Root bind:open={sidebarDrawer}>
			<Sheet.Content side="left" class="w-[min(88vw,300px)] gap-0 p-0" showCloseButton={false}>
				<Sheet.Header class="sr-only">
					<Sheet.Title>{m['assistant.sidebar']()}</Sheet.Title>
					<Sheet.Description>{m['assistant.sidebar_description']()}</Sheet.Description>
				</Sheet.Header>
				<AssistantSidebar
					{...sidebarProps}
					bind:search
					bind:folder
					collapseLabel={m['assistant.hide_sidebar']()}
					oncollapse={() => (sidebarDrawer = false)}
				/>
			</Sheet.Content>
		</Sheet.Root>
	{/if}
	<div class="flex min-h-0 min-w-0 flex-1">
		{#if wide.current}
			<!-- One pane group, so the room stays mounted while the side panes
			     open and close. Each set of panes keeps its own sizes
			     (autoSaveId); every pane needs its order for that. -->
			<Resizable.PaneGroup direction="horizontal" autoSaveId="kuayle-assistant-panel" class="min-h-0 flex-1">
				{#if taskColumnInline}
					<Resizable.Pane id="assistant-task-column" order={1} defaultSize={22} minSize={15} maxSize={35} class="min-w-0"
						>{@render taskColumn(hideTaskColumn)}</Resizable.Pane
					>
					<Resizable.Handle withHandle aria-label={m['assistant.resize_task_column']()} />
				{/if}
				<Resizable.Pane id="assistant-room" order={2} minSize={40} class="flex min-w-0">{@render center()}</Resizable.Pane>
				{#if panelInline && activePanel}
					<Resizable.Handle withHandle aria-label={m['assistant.resize_panel']()} />
					<Resizable.Pane id="assistant-side-panel" order={3} defaultSize={32} minSize={22} maxSize={60} class="min-w-0 border-l"
						><SidePanelHost panels={availablePanels} active={activePanel} context={panelContext} /></Resizable.Pane
					>
				{/if}
			</Resizable.PaneGroup>
		{:else}
			{@render center()}
			<Sheet.Root bind:open={taskSheet}>
				<Sheet.Content side="left" class="w-[min(88vw,340px)] gap-0 p-0" showCloseButton={false}>
					<Sheet.Header class="sr-only">
						<Sheet.Title>{m['assistant.task_column']()}</Sheet.Title>
						<Sheet.Description>{m['assistant.task_column_description']()}</Sheet.Description>
					</Sheet.Header>
					{@render taskColumn(() => (taskSheet = false))}
				</Sheet.Content>
			</Sheet.Root>
			<Sheet.Root open={!!activePanel} onOpenChange={(open) => !open && closePanel()}>
				<Sheet.Content side="right" class="w-full gap-0 p-0 sm:max-w-xl" showCloseButton={false}>
					<Sheet.Header class="sr-only">
						<Sheet.Title>{activePanel?.label() ?? m['assistant.panels']()}</Sheet.Title>
						<Sheet.Description>{m['assistant.panel_description']()}</Sheet.Description>
					</Sheet.Header>
					{#if activePanel}<SidePanelHost panels={availablePanels} active={activePanel} context={panelContext} />{/if}
				</Sheet.Content>
			</Sheet.Root>
		{/if}
	</div>
</div>
{#if board?.composer.open && !board.denied && !chat.unavailable}<TaskComposer
		{slug}
		conversation={chat.id}
		options={board.options}
		task={board.composer.retry}
		initialPrompt={chat.issue?.title ?? ''}
		onaccepted={(task) => chat.acceptTask(task)}
		onclose={() => board?.closeComposer()}
	/>{/if}
