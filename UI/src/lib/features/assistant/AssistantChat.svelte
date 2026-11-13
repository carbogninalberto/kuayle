<script lang="ts">
	import { assistantDraftKey } from './chat-draft';
	import PageTitle from '$lib/components/shared/PageTitle.svelte';
	import { onMount, tick, untrack } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import {
		ArrowUp,
		Square,
		Plus,
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
		Workflow as WorkflowIcon,
		X
	} from '@lucide/svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import * as Message from '$lib/components/ui/message/index.js';
	import * as Bubble from '$lib/components/ui/bubble/index.js';
	import * as MessageScroller from '$lib/components/ui/message-scroller/index.js';
	import * as InputGroup from '$lib/components/ui/input-group/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import * as Sheet from '$lib/components/ui/sheet/index.js';
	import * as Resizable from '$lib/components/ui/resizable/index.js';
	import { conversations, type Conversation, type AssistantStatus, type MessageStatus } from '$lib/api/conversations';
	import { agentTasks, type AgentQueueItem } from '$lib/api/agent-tasks';
	import { ConversationState } from './conversation-state.svelte';
	import { conversationSubject, onScreenSubjects } from '$lib/features/notifications/on-screen-subjects';
	import { TaskBoard } from './task-board.svelte';
	import { ComposerAttachments } from './composer-attachments.svelte';
	import ComposerAttachmentList from './ComposerAttachments.svelte';
	import ComposerVoice from './ComposerVoice.svelte';
	import { appendTranscript } from '$lib/features/voice/voice-rules';
	import MessageAttachments from './MessageAttachments.svelte';
	import { ATTACHMENT_TYPES } from './attachment-rules';
	import ToolCalls from './ToolCalls.svelte';
	import AgentChatMessage from './AgentChatMessage.svelte';
	import CopyMessageButton from './CopyMessageButton.svelte';
	import { markdownToPlainText } from './message-text';
	import ToolSources from './ToolSources.svelte';
	import Markdown from './Markdown.svelte';
	import LiveStatus from './LiveStatus.svelte';
	import ScrollerHandle from './ScrollerHandle.svelte';
	import TaskComposer from './TaskComposer.svelte';
	import MentionList from '$lib/components/shared/mention/MentionList.svelte';
	import type { MentionItem } from '$lib/components/shared/mention/mention.extension';
	import WikiReferenceChip from '$lib/features/wiki/WikiReferenceChip.svelte';
	import { searchWikiReferences } from '$lib/api/wiki';
	import { wikiReferences } from '$lib/features/wiki/wiki-reference-store';
	import {
		chatReferenceParts,
		chatReferenceToken,
		isWikiPageDrag,
		mentionQueryAt,
		mentionQueryMayMatch,
		wikiPageDragData,
		type WikiReference
	} from '$lib/features/wiki/wiki-references';
	import TaskStep from './TaskStep.svelte';
	import AgentThread from './AgentThread.svelte';
	import AgentComposer from './AgentComposer.svelte';
	import { indicatorDot } from './task-tone';
	import { taskLabel } from './task-labels';
	import AssistantSidebar from './AssistantSidebar.svelte';
	import SidePanelHost from './panels/SidePanelHost.svelte';
	import OverviewPanel from './panels/OverviewPanel.svelte';
	import AgentPanel from './panels/AgentPanel.svelte';
	import DiffSidePanel from './panels/DiffSidePanel.svelte';
	import WorkflowPanel from './panels/WorkflowPanel.svelte';
	import { PanelHost, type PanelContext, type PanelDefinition } from './panels/panel-host.svelte';
	import {
		conversationIndicator,
		isMessageableTask,
		placeByTime,
		taskIndicator,
		visibleHistory,
		type TaskIndicator
	} from './shell-rules';
	import { listProjects } from '$lib/api/projects';
	import type { Project } from '$lib/types/project';
	import { listQuery, newChatProject, resolveProjectFilter, type ProjectFilter } from './project-filter';
	import { MediaQuery } from 'svelte/reactivity';
	import { authState } from '$lib/features/auth/auth.state.svelte';
	import { agentInfo } from '$lib/features/agents/agents';
	import { appToast, getErrorMessage } from '$lib/features/toast/toast';
	import { useTerminalDock } from '$lib/features/dev-machines/terminal-dock-context.svelte';
	import type { AgentLiveChatMessage, AgentTask } from '$lib/api/agent-tasks';
	import { m } from '$lib/features/i18n/messages';

	// The assistant page: a three-column shell. Left, the assistant's own
	// sidebar (places, search, Recents with folders); centre, the conversation
	// with the orchestrator assistant, its tool steps, approvals and coding
	// task steps where they happened; right, a resizable side panel host
	// (details, the coding agents' threads, a diff, and future panels). Below
	// the breakpoints the sidebars become drawers.
	let { slug, conversationId }: { slug: string; conversationId: string | null } = $props();
	// This component is keyed by workspace, conversation and authenticated user.
	const chat = untrack(() => new ConversationState(slug, conversationId ?? ''));
	// While this conversation is displayed, its own approval requests are in
	// view: the notifier stays quiet for them (see on-screen-subjects.ts).
	$effect(() => (conversationId ? onScreenSubjects.show(conversationSubject(conversationId)) : undefined));
	const userKey = untrack(() => `${authState.user?.id}:${slug}`);
	const draftKey = untrack(() => assistantDraftKey(authState.user?.id, slug, conversationId));
	let history = $state<Conversation[]>([]);
	let status = $state<AssistantStatus>({ configured: false });
	let initialized = $state(false);
	let text = $state('');
	// The list's project filter is remembered per person and workspace; a new
	// chat started under a project filter belongs to that project.
	const filterKey = untrack(() => `assistant-project-filter:${userKey}`);
	let projects = $state<Project[]>([]);
	let projectFilter = $state<ProjectFilter>(
		untrack(() => {
			try {
				return (localStorage.getItem(filterKey) as ProjectFilter | null) ?? 'all';
			} catch {
				return 'all';
			}
		})
	);
	const projectOptions = $derived([
		{ value: 'all', label: m['assistant.all_projects']() },
		{ value: 'none', label: m['assistant.no_project']() },
		...projects.map((project) => ({ value: project.id, label: project.name }))
	]);
	function loadHistory(filter: ProjectFilter) {
		return conversations.list(slug, listQuery(filter)).then((items) => {
			if (!disposed && projectFilter === filter) history = items;
		});
	}
	function chooseProject(next: string) {
		if (next === projectFilter) return;
		projectFilter = next;
		try {
			if (next === 'all') localStorage.removeItem(filterKey);
			else localStorage.setItem(filterKey, next);
		} catch {
			/* The filter still applies without storage. */
		}
		void loadHistory(next).catch((error) => appToast.apiError(error, m['assistant.error']()));
	}

	// ── Layout: fixed sidebar from lg, inline side panel from xl, drawers below.
	const large = new MediaQuery('min-width: 1024px');
	const wide = new MediaQuery('min-width: 1280px');
	const sidebarKey = untrack(() => `assistant-sidebar:${userKey}`);
	let sidebarCollapsed = $state(
		untrack(() => {
			try {
				return localStorage.getItem(sidebarKey) === 'collapsed';
			} catch {
				return false;
			}
		})
	);
	let sidebarDrawer = $state(false);
	const sidebarInline = $derived(large.current && !sidebarCollapsed);
	function setSidebarCollapsed(value: boolean) {
		sidebarCollapsed = value;
		try {
			localStorage.setItem(sidebarKey, value ? 'collapsed' : 'open');
		} catch {
			/* Collapsing still works without storage. */
		}
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
		{ id: 'workflow', label: m['assistant.panel_workflow'], icon: WorkflowIcon, component: WorkflowPanel, available: () => !!chat.issue }
	];
	const availablePanels = $derived(panels.filter((panel) => panel.available?.() ?? true));
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
		if (target && !wide.current) untrack(() => (host.active = null));
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
	const agentParam = $derived(page.url.searchParams.get('agent'));
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
	const agentView = $derived(!!agentParam && !!conversationId);
	const assistantHref = $derived(
		conversationId ? `/${slug}/assistant?conversation=${encodeURIComponent(conversationId)}` : `/${slug}/assistant`
	);
	function openAgent(task: AgentTask) {
		const url = new URL(page.url);
		url.searchParams.set('agent', task.id);
		url.searchParams.delete('task');
		void goto(`${url.pathname}${url.search}`, { keepFocus: true, noScroll: true });
	}
	function closeAgent() {
		void goto(assistantHref, { keepFocus: true, noScroll: true });
	}
	// An agent's view has its own composer, which writes to that agent
	// (AgentComposer); the page's composer belongs to the assistant's chat.
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
		for (const conversation of history) result[conversation.id] = conversationIndicator(conversation.id, queueItems);
		if (conversationId && chat.busy && !result[conversationId]) result[conversationId] = 'running';
		return result;
	});

	let sending = $state(false);
	let error = $state('');
	let notice = $state('');
	let textarea = $state<HTMLTextAreaElement | null>(null);
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
	const messageable = $derived(
		(board?.tasks ?? []).filter((task) => task.kind !== 'review' && isMessageableTask(task.state))
	);
	const targetTask = $derived(chat.composerTarget ? chat.liveTasks[chat.composerTarget] : undefined);
	const toAgent = $derived(!!targetTask && isMessageableTask(targetTask.state));
	$effect(() => {
		// An agent that finished can no longer be written to: fall back.
		if (chat.composerTarget && !toAgent) untrack(() => (chat.composerTarget = null));
	});
	const agentName = (task: AgentTask) => agentInfo(task.provider).name;
	const targetLabel = $derived(
		toAgent && targetTask
			? m['assistant.target_agent']({ identifier: chat.issue?.identifier ?? '', agent: agentName(targetTask) })
			: m['assistant.target_assistant']()
	);
	$effect(() => {
		void chat.composerFocus;
		if (!chat.composerFocus) return;
		untrack(() => {
			// On narrow screens the panel is a drawer over the composer.
			if (!wide.current) host.close();
			void tick().then(() => textarea?.focus());
		});
	});

	function followLatest() {
		scroller?.scrollToEnd({ behavior: 'auto' });
	}
	let draftLoaded = $state(false);
	let pending: { id: string; content: string; attachments: string[]; conversation?: string } | null = null;
	// A new chat is created when its first image is attached, so the image can
	// be uploaded to it before the first message is sent.
	let draftConversation: string | null = null;
	let fileInput = $state<HTMLInputElement | null>(null);
	let dragging = $state(false);

	// Wiki page references. The composer is plain text, so a page chosen with
	// "@" or dragged in from a wiki tree goes in as a token naming the page;
	// sent messages show it as the usual reference chip.
	let pageMention = $state<{ from: number; query: string } | null>(null);
	let pageSuggestions = $state<WikiReference[]>([]);
	let pageIndex = $state(0);
	let pageMenu = $state({ x: 0, y: 0, height: 0 });
	const pageItems = $derived<MentionItem[]>(
		pageSuggestions.map((page) => ({ kind: 'wiki', id: page.id, title: page.title ?? '', scope: page.scope_name ?? '' }))
	);
	function trackPageMention() {
		const found = textarea ? mentionQueryAt(textarea.value, textarea.selectionStart) : null;
		if (found && textarea && !pageMention) {
			const rect = textarea.getBoundingClientRect();
			pageMenu = { x: rect.left, y: rect.bottom + 4, height: rect.height };
		}
		if (found?.query !== pageMention?.query) pageIndex = 0;
		pageMention = found;
	}
	// Queries of the "@" being typed that found no page: text that continues
	// one of them is ordinary prose after an "@", not a search.
	let emptyPageQueries: { from: number; queries: string[] } | null = null;
	$effect(() => {
		const mention = pageMention;
		if (!mention) {
			pageSuggestions = [];
			emptyPageQueries = null;
			return;
		}
		if (emptyPageQueries?.from !== mention.from) emptyPageQueries = { from: mention.from, queries: [] };
		const empty = emptyPageQueries.queries;
		if (!mentionQueryMayMatch(mention.query.trim(), empty)) {
			pageSuggestions = [];
			return;
		}
		let stale = false;
		const timer = setTimeout(
			() =>
				void searchWikiReferences(slug, mention.query)
					.then((found) => {
						if (found.length === 0) empty.push(mention.query.trim());
						if (!stale) pageSuggestions = found;
					})
					.catch(() => {
						if (!stale) pageSuggestions = [];
					}),
			mention.query ? 120 : 0
		);
		return () => {
			stale = true;
			clearTimeout(timer);
		};
	});
	/** Puts a page reference in the draft, replacing [from, to) of it. */
	function insertPageReference(reference: WikiReference, from: number, to: number) {
		const token = chatReferenceToken(reference);
		const before = text.slice(0, from);
		const lead = before && !/\s$/.test(before) ? ' ' : '';
		text = `${before}${lead}${token} ${text.slice(to).replace(/^ /, '')}`;
		const caret = before.length + lead.length + token.length + 1;
		pageMention = null;
		void tick().then(() => {
			textarea?.focus();
			textarea?.setSelectionRange(caret, caret);
		});
	}
	function choosePage(item: MentionItem | undefined) {
		const reference = pageSuggestions.find((page) => page.id === item?.id);
		if (!reference || !pageMention || !textarea) return;
		insertPageReference(reference, pageMention.from, textarea.selectionStart);
	}
	async function dropPage(pageId: string) {
		const reference = await wikiReferences.resolve(slug, pageId);
		if (!reference.accessible) return;
		const at = textarea?.selectionStart ?? text.length;
		insertPageReference(reference, at, at);
	}

	async function ensureConversation(fallbackTitle = m['assistant.image_title']()) {
		const existing = conversationId ?? pending?.conversation ?? draftConversation;
		if (existing) return existing;
		draftConversation = (
			await conversations.create(slug, text.trim().slice(0, 100) || fallbackTitle, newChatProject(projectFilter))
		).id;
		return draftConversation;
	}
	const attachments = untrack(() => new ComposerAttachments(slug, () => ensureConversation()));
	const sameIds = (a: string[], b: string[]) => a.length === b.length && a.every((id, i) => id === b[i]);
	function imageFiles(list: FileList | null | undefined) {
		return Array.from(list ?? []).filter((file) => file.type.startsWith('image/'));
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
	const agentTooLong = $derived(toAgent && text.length > 4000);
	const canSend = $derived(
		toAgent
			? text.trim().length > 0 && !agentTooLong && !sending && !chat.unavailable
			: initialized &&
					status.configured &&
					(text.trim().length > 0 || attachments.ids.length > 0) &&
					new TextEncoder().encode(text).length <= 16384 &&
					!attachments.uploading &&
					!attachments.failed &&
					!sending &&
					!chat.busy &&
					!chat.unavailable &&
					!chat.loading &&
					!chat.failed
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
		try {
			text = localStorage.getItem(draftKey) ?? '';
			const saved = JSON.parse(localStorage.getItem(`${draftKey}:pending`) ?? 'null');
			// Images are not restored after a reload, so only a text-only send resumes.
			if (saved && typeof saved.id === 'string' && saved.content === text && !saved.attachments?.length)
				pending = { ...saved, attachments: [] };
		} catch {
			/* Storage can be disabled. */
		}
		draftLoaded = true;
		const saved = projectFilter;
		void Promise.all([
			conversations.list(slug, listQuery(saved)),
			conversations.status(slug),
			listProjects(slug).catch(() => [])
		])
			.then(([items, provider, visible]) => {
				if (disposed) return;
				history = items;
				status = provider;
				projects = visible;
				initialized = true;
				// A remembered project that is no longer visible falls back to all.
				const resolved = resolveProjectFilter(saved, visible);
				if (resolved !== saved) chooseProject(resolved);
			})
			.catch(() => {
				if (!disposed) error = m['assistant.error']();
			});
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
	// A view can ask for a message, such as a review request from an agent's
	// view; the user still reads, edits and sends it — to the assistant. The
	// composer takes focus once it is on screen again.
	let draftFocus = $state(false);
	$effect(() => {
		const draft = chat.composerDraft;
		if (draft === null) return;
		untrack(() => {
			chat.composerDraft = null;
			chat.composerTarget = null;
			text = draft;
			if (!wide.current) host.close();
			draftFocus = true;
		});
	});
	$effect(() => {
		if (!draftFocus || !textarea?.isConnected) return;
		const target = textarea;
		untrack(() => (draftFocus = false));
		void tick().then(() => target.focus());
	});
	$effect(() => {
		if (!draftLoaded) return;
		try {
			if (text) localStorage.setItem(draftKey, text);
			else localStorage.removeItem(draftKey);
		} catch {
			/* Private browsing may deny storage. */
		}
	});

	async function sendToAgent(task: AgentTask) {
		const body = text.trim();
		sending = true;
		error = '';
		notice = '';
		try {
			await agentTasks.sendLiveMessage(slug, task.id, body);
			if (disposed) return;
			if (text.trim() === body) text = '';
			// The agent's thread reloads and shows the message with its delivery.
			chat.taskLiveCursor = { ...chat.taskLiveCursor, [task.id]: -Date.now() };
			notice = m['assistant.sent_to_agent']({ identifier: chat.issue?.identifier ?? '', agent: agentName(task) });
			await tick();
			textarea?.focus();
		} catch (e) {
			if (!disposed) error = getErrorMessage(e, m['tasks.live_send_error']());
		} finally {
			sending = false;
		}
	}

	async function send(event?: SubmitEvent) {
		event?.preventDefault();
		if (!canSend) return;
		if (toAgent && targetTask) return sendToAgent(targetTask);
		sending = true;
		error = '';
		notice = '';
		followLatest();
		const ids = attachments.ids;
		if (!pending || pending.content !== text || !sameIds(pending.attachments, ids))
			pending = {
				id: crypto.randomUUID(),
				content: text,
				attachments: ids,
				conversation: pending?.conversation ?? draftConversation ?? undefined
			};
		try {
			const id =
				conversationId ??
				pending.conversation ??
				(
					await conversations.create(
						slug,
						text.trim().slice(0, 100) || m['assistant.image_title'](),
						newChatProject(projectFilter)
					)
				).id;
			pending.conversation = id;
			try {
				localStorage.setItem(`${draftKey}:pending`, JSON.stringify(pending));
			} catch {
				/* Sending still works without storage. */
			}
			const submitted = pending.content;
			const accepted = await conversations.send(slug, id, pending.id, submitted, pending.attachments);
			if (disposed) return;
			chat.accept(accepted);
			if (text === submitted) text = '';
			if (sameIds(attachments.ids, pending.attachments)) attachments.clear();
			pending = null;
			try {
				if (!text) localStorage.removeItem(draftKey);
				else if (!conversationId) localStorage.setItem(draftKey.replace(/:new$/, `:${id}`), text);
				localStorage.removeItem(`${draftKey}:pending`);
			} catch {
				/* Storage is optional. */
			}
			if (!conversationId) await goto(`/${slug}/assistant?conversation=${encodeURIComponent(id)}`);
			else await chat.load();
			await tick();
			textarea?.focus();
		} catch {
			if (!disposed) error = m['assistant.send_error']();
		} finally {
			sending = false;
		}
	}
	async function stop() {
		if (!conversationId) return;
		try {
			await conversations.cancel(slug, conversationId);
			await chat.load();
		} catch {
			error = m['assistant.error']();
		}
	}
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
			history = history.map((c) => (c.id === updated.id ? { ...c, title: updated.title } : c));
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
		draft: (value) => (chat.composerDraft = value)
	};
	const sidebarProps = $derived({
		slug,
		conversationId,
		projects,
		projectFilter,
		projectOptions,
		indicators,
		onproject: chooseProject,
		onrenamed: renamed
	});
</script>

<PageTitle parts={[m['assistant.title']()]} />

{#snippet topBar()}
	<header class="flex h-12 shrink-0 items-center gap-1 border-b px-2 md:px-3">
		{#if !sidebarInline}<Button
				variant="ghost"
				size="icon-sm"
				aria-label={m['assistant.show_sidebar']()}
				title={m['assistant.show_sidebar']()}
				aria-expanded={sidebarDrawer}
				onclick={showSidebar}><PanelLeft /></Button
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
			{#each availablePanels.filter((panel) => panel.id !== 'diff') as panel (panel.id)}
				<Button
					variant="ghost"
					size="icon-sm"
					aria-label={panel.label()}
					title={panel.label()}
					aria-pressed={activePanel?.id === panel.id}
					class={activePanel?.id === panel.id ? 'bg-muted text-foreground' : ''}
					onclick={() => (activePanel?.id === panel.id ? closePanel() : host.open(panel.id))}
					><panel.icon /></Button
				>
			{/each}
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
					<Markdown source={message.content} />
				</Bubble.Content>
				{#if message.content}<CopyMessageButton
						class="top-0 end-0"
						text={() => markdownToPlainText(message.content)}
					/>{/if}
			</Bubble.Root>
			<!-- Approval requests always close the message, after its text. -->
			<ToolCalls
				calls={chat.tools[message.id] ?? []}
				slug={chat.slug}
				approvals={chat.approvals}
				part="approvals"
				onresolve={(approval, decision, choice) => chat.resolveApproval(approval, decision, choice)}
			/>
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

{#snippet composer()}
	<div class="shrink-0 px-3 pt-1 pb-3 md:px-6 md:pb-4">
		<div class="mx-auto w-full max-w-3xl">
			{#if error || chat.failed}<p role="alert" class="mb-2 text-sm text-destructive">
					{error || m['assistant.error']()}
				</p>{/if}
			{#if initialized && !status.configured && !toAgent}<p class="mb-3 text-sm text-muted-foreground">
					{m['assistant.configure']()} <a class="underline" href="/{slug}/settings/ai">{m['assistant.settings']()}</a>
				</p>{/if}
			{#if attachments.notice}<p role="alert" class="mb-2 text-sm text-destructive">{attachments.notice}</p>{/if}
			{#if toAgent && targetTask}
				<div
					class="mb-1.5 flex min-w-0 items-center gap-2 rounded-lg border border-[var(--status-running)]/40 bg-[var(--status-running)]/5 px-3 py-1.5 text-xs"
					role="status"
				>
					<Bot size={14} class="shrink-0 text-[var(--status-running)]" aria-hidden="true" />
					<span class="min-w-0 flex-1 truncate"
						>{m['assistant.target_agent_notice']({
							identifier: chat.issue?.identifier ?? '',
							agent: agentName(targetTask)
						})}</span
					>
					<Button
						variant="ghost"
						size="sm"
						class="h-6 shrink-0 px-2 text-xs"
						onclick={() => (chat.composerTarget = null)}><X size={12} />{m['assistant.target_back']()}</Button
					>
				</div>
			{/if}
			<form
				onsubmit={send}
				aria-label={m['assistant.composer']()}
				ondragover={(event) => {
					if (isWikiPageDrag(event.dataTransfer) && !chat.unavailable) {
						event.preventDefault();
						dragging = true;
						return;
					}
					if (toAgent || !status.configured || chat.unavailable || !event.dataTransfer?.types.includes('Files')) return;
					event.preventDefault();
					dragging = true;
				}}
				ondragleave={(event) => {
					if (!(event.currentTarget as HTMLElement).contains(event.relatedTarget as Node | null)) dragging = false;
				}}
				ondrop={(event) => {
					dragging = false;
					const pageId = wikiPageDragData(event.dataTransfer);
					if (pageId) {
						event.preventDefault();
						if (!chat.unavailable) void dropPage(pageId);
						return;
					}
					if (toAgent) return;
					const files = imageFiles(event.dataTransfer?.files);
					if (!files.length) return;
					event.preventDefault();
					attachments.add(files);
				}}
			>
				<input
					bind:this={fileInput}
					type="file"
					accept={ATTACHMENT_TYPES.join(',')}
					multiple
					class="sr-only"
					tabindex="-1"
					aria-hidden="true"
					onchange={(event) => {
						const input = event.currentTarget as HTMLInputElement;
						attachments.add(Array.from(input.files ?? []));
						input.value = '';
					}}
				/>
				<InputGroup.Root
					class="rounded-2xl bg-background shadow-sm {dragging ? 'ring-2 ring-ring' : ''} {toAgent
						? 'border-[var(--status-running)]/50'
						: ''}"
				>
					{#if attachments.items.length && !toAgent}<InputGroup.Addon align="block-start"
							><ComposerAttachmentList {attachments} /></InputGroup.Addon
						>{/if}
					<InputGroup.Textarea
						bind:ref={textarea}
						bind:value={text}
						aria-label={toAgent ? m['tasks.live_composer']() : m['assistant.message']()}
						aria-invalid={agentTooLong || undefined}
						placeholder={toAgent && targetTask
							? m['assistant.placeholder_agent']({
									identifier: chat.issue?.identifier ?? '',
									agent: agentName(targetTask)
								})
							: m['assistant.placeholder']()}
						maxlength={16000}
						disabled={!toAgent && (!initialized || !status.configured || chat.unavailable)}
						class="max-h-48 min-h-11"
						oninput={trackPageMention}
						onclick={trackPageMention}
						onkeyup={(event) => {
							if (event.key.startsWith('Arrow') && !(pageMention && pageItems.length)) trackPageMention();
						}}
						onblur={() => (pageMention = null)}
						onkeydown={(event) => {
							// The page menu takes the keys the rich editor's mention menu takes.
							if (pageMention && pageItems.length && !event.isComposing) {
								if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
									event.preventDefault();
									pageIndex = (pageIndex + (event.key === 'ArrowDown' ? 1 : pageItems.length - 1)) % pageItems.length;
									return;
								}
								if (event.key === 'Enter' || event.key === 'Tab') {
									event.preventDefault();
									choosePage(pageItems[pageIndex]);
									return;
								}
								if (event.key === 'Escape') {
									event.preventDefault();
									pageMention = null;
									return;
								}
							}
							if (event.key === 'Enter' && !event.shiftKey && !event.isComposing) {
								event.preventDefault();
								void send();
							}
						}}
						onpaste={(event) => {
							if (toAgent) return;
							const files = imageFiles(event.clipboardData?.files);
							if (!files.length) return;
							event.preventDefault();
							attachments.add(files);
						}}
					/>
					<InputGroup.Addon align="block-end" class="justify-between gap-2">
						<div class="flex min-w-0 items-center gap-1">
							<InputGroup.Button
								size="icon-sm"
								aria-label={m['assistant.attach']()}
								title={m['assistant.attach']()}
								disabled={toAgent || !initialized || !status.configured || chat.unavailable}
								onclick={() => fileInput?.click()}><Plus size={16} /></InputGroup.Button
							>
							{#if messageable.length}
								<DropdownMenu.Root>
									<DropdownMenu.Trigger>
										{#snippet child({ props })}
											<InputGroup.Button
												{...props}
												size="sm"
												variant={toAgent ? 'secondary' : 'outline'}
												class="h-7 max-w-56 min-w-0 gap-1 rounded-full px-2.5 text-xs"
												aria-label={m['assistant.target_label']({ target: targetLabel })}
												>{#if toAgent}<Bot size={13} />{:else}<MessageCircle size={13} />{/if}<span class="truncate"
													>{targetLabel}</span
												><ChevronDown size={12} class="shrink-0" /></InputGroup.Button
											>
										{/snippet}
									</DropdownMenu.Trigger>
									<DropdownMenu.Content align="start" class="w-64">
										<DropdownMenu.Label class="text-xs font-normal text-muted-foreground"
											>{m['assistant.target_heading']()}</DropdownMenu.Label
										>
										<DropdownMenu.CheckboxItem checked={!toAgent} onCheckedChange={() => (chat.composerTarget = null)}
											>{m['assistant.target_assistant']()}</DropdownMenu.CheckboxItem
										>
										{#each messageable as task (task.id)}<DropdownMenu.CheckboxItem
												checked={chat.composerTarget === task.id}
												onCheckedChange={() => (chat.composerTarget = task.id)}
												><span class="truncate"
													>{m['assistant.target_agent']({
														identifier: chat.issue?.identifier ?? '',
														agent: agentName(task)
													})}</span
												></DropdownMenu.CheckboxItem
											>{/each}
									</DropdownMenu.Content>
								</DropdownMenu.Root>
							{:else}
								<span class="flex h-7 items-center gap-1 rounded-full border px-2.5 text-xs text-muted-foreground"
									><MessageCircle size={13} aria-hidden="true" />{m['assistant.target_assistant']()}</span
								>
							{/if}
						</div>
						<div class="flex shrink-0 items-center gap-1">
							<ComposerVoice
								{slug}
								ensureConversation={() => ensureConversation(m['voice.conversation_title']())}
								disabled={!initialized || !status.configured || chat.unavailable}
								ontranscript={(transcript) => {
									text = appendTranscript(text, transcript);
									textarea?.focus();
								}}
							/>
							{#if chat.busy && !toAgent}<InputGroup.Button
									size="icon-sm"
									aria-label={m['assistant.stop']()}
									onclick={() => void stop()}><Square size={14} /></InputGroup.Button
								>
							{:else}<InputGroup.Button
									type="submit"
									variant="default"
									size="icon-sm"
									class="rounded-full"
									disabled={!canSend}
									aria-label={toAgent ? m['tasks.live_send']() : m['assistant.send']()}
									><ArrowUp size={16} /></InputGroup.Button
								>{/if}
						</div>
					</InputGroup.Addon>
				</InputGroup.Root>
				<p class="mt-1.5 flex min-w-0 justify-center gap-1 text-center text-[11px] text-muted-foreground">
					{#if notice}<span role="status" class="truncate">{notice}</span>{:else}
						<span class="truncate">{toAgent ? '' : `${status.model ?? m['assistant.no_provider']()} · `}{sending
								? m['assistant.sending']()
								: attachments.uploading
									? m['assistant.attachment_wait']()
									: dragging
										? m['assistant.attachment_drop']()
										: toAgent
											? targetTask?.state === 'running'
												? m['tasks.live_hint_running']()
												: m['tasks.live_hint_next']()
											: m['assistant.hint']()}</span
						>{/if}
				</p>
			</form>
			{#if pageMention && pageItems.length}
				<MentionList
					items={pageItems}
					selectedIndex={pageIndex}
					position={pageMenu}
					anchorHeight={pageMenu.height}
					onselect={choosePage}
					onclose={() => (pageMention = null)}
				/>
			{/if}
		</div>
	</div>
{/snippet}

{#snippet center()}
	<section class="flex h-full min-h-0 min-w-0 flex-1 flex-col" aria-label={m['assistant.conversation']()}>
		{@render topBar()}
		<div class="min-h-0 flex-1">
			<MessageScroller.Provider autoScroll>
				<ScrollerHandle bind:scroller />
				<MessageScroller.Root>
					<MessageScroller.Viewport aria-label={m['assistant.messages']()}>
						{#if agentView}<MessageScroller.Content class="mx-auto w-full max-w-3xl gap-6 px-4 py-6 md:px-6">
								<section aria-label={m['assistant.agent_view_label']({ identifier: chat.issue?.identifier ?? '' })}>
									{#if agentTask}{#key agentTask.id}<AgentThread
												{chat}
												task={agentTask}
												backHref={assistantHref}
												onback={closeAgent}
												allowedAgents={board?.options?.providers.map((provider) => provider.id) ?? []}
											/>{/key}{:else if firstLoad || (chat.issue && !chat.unavailable && (!board || board.loading))}<p
											class="text-sm text-muted-foreground"
											role="status"
										>
											{m['assistant.loading']()}
										</p>{:else}<div class="space-y-3">
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
							{:else if chat.messages.length === 0 && !Object.keys(chat.liveTasks).length}
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
													onclick={() => {
														chat.composerTarget = null;
														text = suggestion;
														void send();
													}}><span class="truncate">{suggestion}</span></Button
												>
											{/each}
										</div>
									{/if}
								</div>
							{/if}
							{#each shown.shown as message (message.id)}
								{#each timeline.before[message.id] ?? [] as item (item.id)}{@render timelineItem(item)}{/each}
								<MessageScroller.Item messageId={message.id}>
									{#if message.role === 'user'}{@render userMessage(message)}{:else}{@render assistantMessage(
											message
										)}{/if}
								</MessageScroller.Item>
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
		{#if !agentView}{@render composer()}{:else if agentTask}{#key agentTask.id}<AgentComposer
					{chat}
					task={agentTask}
				/>{/key}{/if}
	</section>
{/snippet}

<div class="flex h-full min-h-0 min-w-0 flex-1 bg-background text-foreground" data-testid="assistant-shell">
	{#if sidebarInline}
		<div class="w-[272px] shrink-0 border-r" data-testid="assistant-sidebar">
			<AssistantSidebar
				{...sidebarProps}
				bind:history
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
					bind:history
					collapseLabel={m['assistant.hide_sidebar']()}
					oncollapse={() => (sidebarDrawer = false)}
				/>
			</Sheet.Content>
		</Sheet.Root>
	{/if}
	<div class="flex min-h-0 min-w-0 flex-1">
		{#if panelInline && activePanel}
			<Resizable.PaneGroup direction="horizontal" autoSaveId="kuayle-assistant-panel" class="min-h-0 flex-1">
				<Resizable.Pane minSize={40} class="flex min-w-0">{@render center()}</Resizable.Pane>
				<Resizable.Handle withHandle aria-label={m['assistant.resize_panel']()} />
				<Resizable.Pane defaultSize={32} minSize={22} maxSize={60} class="min-w-0 border-l"
					><SidePanelHost panels={availablePanels} active={activePanel} context={panelContext} /></Resizable.Pane
				>
			</Resizable.PaneGroup>
		{:else}
			{@render center()}
			{#if !wide.current}
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
