import type { AgentLiveChatMessage, AgentTask, TaskActivity } from '$lib/api/agent-tasks';
import type { TaskBar } from './task-actions';
import {
	conversations,
	type Conversation,
	type ConversationIssue,
	type ConversationMessage,
	type ConversationEvent,
	type ConversationToolCall,
	type ConversationApproval
} from '$lib/api/conversations';

export class ConversationState {
	conversation = $state<Conversation | null>(null);
	issue = $state<ConversationIssue | null>(null);
	messages = $state<ConversationMessage[]>([]);
	// Tool calls by assistant message, ordered by their recorded sequence.
	tools = $state<Record<string, ConversationToolCall[]>>({});
	// Approvals by tool call. The backend is the authority; this is display state.
	approvals = $state<Record<string, ConversationApproval>>({});
	liveTasks = $state<Record<string, AgentTask>>({});
	// The attempt whose diff is open beside the chat.
	diffView = $state<{ taskId: string; runId: string; title: string } | null>(null);
	// The newest task's compact header, pinned while its row is scrolled away.
	taskBar = $state<TaskBar | null>(null);
	taskActivity = $state<Record<string, TaskActivity>>({});
	taskValidationCursor = $state<Record<string, number>>({});
	taskPublicationCursor = $state<Record<string, number>>({});
	taskSignalCursor = $state<Record<string, number>>({});
	// Bumped when a review of the task changes; the task card reloads its sessions.
	taskReviewCursor = $state<Record<string, number>>({});
	// What the conversation's coding agents sent it, oldest first. Agent text
	// is untrusted: it is only ever shown as plain text.
	agentMessages = $state<AgentLiveChatMessage[]>([]);
	// Bumped when a task's live channel changes; its thread on the card reloads.
	taskLiveCursor = $state<Record<string, number>>({});
	// Told about an agent question that arrives live, to notify the reader.
	onAgentQuestion: ((message: AgentLiveChatMessage) => void) | null = null;
	// Text a task card asks the chat composer to show, e.g. a review request.
	composerDraft = $state<string | null>(null);
	cursor = 0;
	hasMore = $state(false);
	loading = $state(true);
	loadingOlder = $state(false);
	unavailable = $state(false);
	failed = $state(false);
	connected = $state(false);
	busy = $derived(this.messages.some((m) => m.status === 'queued' || m.status === 'streaming'));
	private socket: WebSocket | null = null;
	private timer: ReturnType<typeof setTimeout> | undefined;
	private disposed = false;
	private retries = 0;
	private loadVersion = 0;
	constructor(
		readonly slug: string,
		readonly id: string
	) {}

	async load() {
		if (this.disposed || this.unavailable) return;
		const version = ++this.loadVersion;
		this.loading = true;
		this.disconnect();
		this.failed = false;
		try {
			const history = await conversations.history(this.slug, this.id);
			if (this.disposed || version !== this.loadVersion) return;
			this.conversation = history.conversation;
			this.issue = history.issue ?? null;
			this.messages = history.messages;
			this.tools = {};
			this.approvals = {};
			this.acceptTools(history.tools ?? []);
			this.acceptApprovals(history.approvals ?? []);
			this.agentMessages = [];
			this.acceptAgentMessages(history.agent_messages ?? []);
			this.cursor = history.cursor;
			this.hasMore = history.has_more;
			this.connect();
		} catch (error) {
			if (version === this.loadVersion) {
				this.handleError(error);
				if (!this.unavailable && !this.disposed) this.timer = setTimeout(() => void this.load(), 3000);
			}
		} finally {
			if (version === this.loadVersion) this.loading = false;
		}
	}
	private handleError(error: unknown) {
		if (this.disposed) return;
		const status = (error as { status?: number })?.status;
		if (status === 401 || status === 403 || status === 404) {
			this.unavailable = true;
			this.loadVersion++;
			this.loading = false;
			this.messages = [];
			this.tools = {};
			this.approvals = {};
			this.liveTasks = {};
			this.taskActivity = {};
			this.taskValidationCursor = {};
			this.taskPublicationCursor = {};
			this.taskSignalCursor = {};
			this.taskReviewCursor = {};
			this.agentMessages = [];
			this.taskLiveCursor = {};
			this.conversation = null;
			this.issue = null;
			this.disconnect();
		} else {
			this.failed = true;
		}
	}
	accept(message: ConversationMessage) {
		if (
			this.disposed ||
			this.unavailable ||
			message.conversation_id !== this.id ||
			this.messages.some((m) => m.id === message.id)
		)
			return;
		this.messages = [...this.messages, message].sort((a, b) => a.sequence - b.sequence);
	}
	apply(event: ConversationEvent) {
		if (this.disposed || this.unavailable || event.conversation_id !== this.id || event.cursor <= this.cursor) return;
		if (event.type === 'task.state_changed' || event.type === 'task.updated') {
			this.acceptTask(event.payload as AgentTask, true);
		} else if (event.type === 'task.tests_updated') {
			const data = event.payload as { task_id: string };
			this.taskValidationCursor = { ...this.taskValidationCursor, [data.task_id]: event.cursor };
		} else if (event.type === 'task.signal_recorded') {
			const data = event.payload as { task_id: string };
			this.taskSignalCursor = { ...this.taskSignalCursor, [data.task_id]: event.cursor };
		} else if (event.type === 'task.live_message') {
			const message = event.payload as AgentLiveChatMessage;
			this.taskLiveCursor = { ...this.taskLiveCursor, [message.task_id]: event.cursor };
			if (message.direction === 'from_agent' && message.state === 'received') {
				const known = this.agentMessages.some((m) => m.id === message.id);
				this.acceptAgentMessages([message]);
				if (!known && message.kind === 'question') this.onAgentQuestion?.(message);
			}
		} else if (event.type === 'task.review_updated') {
			this.reviewChanged((event.payload as { task_id: string }).task_id);
		} else if (event.type === 'task.publication_updated') {
			const data = event.payload as { task_id: string };
			this.taskPublicationCursor = { ...this.taskPublicationCursor, [data.task_id]: event.cursor };
		} else if (event.type === 'tool.started' || event.type === 'tool.completed') {
			this.acceptTools([event.payload as ConversationToolCall]);
		} else if (event.type.startsWith('approval.')) {
			this.acceptApprovals([event.payload as ConversationApproval]);
		} else if (event.type === 'task.activity') {
			const activity = event.payload as TaskActivity;
			this.taskActivity = { ...this.taskActivity, [activity.task_id]: activity };
		} else if (event.type === 'message.created') {
			const message = event.payload as ConversationMessage;
			this.accept(message);
		} else {
			const data = event.payload as { message_id: string; delta?: string; status?: ConversationMessage['status'] };
			this.messages = this.messages.map((m) =>
				m.id === data.message_id
					? { ...m, content: m.content + (data.delta ?? ''), status: data.status ?? m.status }
					: m
			);
		}
		this.cursor = event.cursor;
	}
	async resolveApproval(approval: ConversationApproval, decision: 'approve' | 'deny') {
		const result = await conversations.resolveApproval(this.slug, this.id, approval.id, decision);
		if (!this.disposed && !this.unavailable) this.acceptApprovals([result]);
	}
	acceptApprovals(approvals: ConversationApproval[]) {
		const next = { ...this.approvals };
		for (const approval of approvals) {
			if (approval.conversation_id !== this.id) continue;
			const current = next[approval.tool_call_id];
			// Ignore stale copies: a resolved decision never reverts to pending.
			if (current && Date.parse(current.updated_at) > Date.parse(approval.updated_at)) continue;
			next[approval.tool_call_id] = approval;
		}
		this.approvals = next;
	}
	acceptTools(calls: ConversationToolCall[]) {
		if (!calls.length) return;
		const next = { ...this.tools };
		for (const call of calls) {
			if (call.conversation_id !== this.id) continue;
			const existing = (next[call.message_id] ?? []).filter((c) => c.id !== call.id);
			const previous = next[call.message_id]?.find((c) => c.id === call.id);
			// A replayed start must not hide an already recorded outcome.
			const kept = previous && previous.status !== 'running' && call.status === 'running' ? previous : call;
			next[call.message_id] = [...existing, kept].sort((a, b) => a.sequence - b.sequence);
		}
		this.tools = next;
	}
	acceptAgentMessages(messages: AgentLiveChatMessage[]) {
		const fresh = messages.filter(
			(message) =>
				message.direction === 'from_agent' &&
				message.state === 'received' &&
				!this.agentMessages.some((known) => known.id === message.id)
		);
		if (!fresh.length) return;
		this.agentMessages = [...this.agentMessages, ...fresh].sort(
			(a, b) => Date.parse(a.created_at) - Date.parse(b.created_at) || a.id.localeCompare(b.id)
		);
	}
	private reviewChanged(taskId: string) {
		this.taskReviewCursor = { ...this.taskReviewCursor, [taskId]: (this.taskReviewCursor[taskId] ?? 0) + 1 };
	}
	acceptTask(task: AgentTask, fromStream = false, expectedCurrent?: AgentTask | null) {
		if (this.disposed || this.unavailable || task.conversation_id !== this.id) return;
		// A review is shown on the task it reviews, never as a task of its own.
		if (task.kind === 'review') {
			if (task.review_of_task_id) this.reviewChanged(task.review_of_task_id);
			return;
		}
		const current = this.liveTasks[task.id];
		if (
			current &&
			(current.generation > task.generation ||
				(!fromStream &&
					current !== expectedCurrent &&
					current.generation === task.generation &&
					Date.parse(current.updated_at) >= Date.parse(task.updated_at)))
		)
			return;
		this.liveTasks = { ...this.liveTasks, [task.id]: task };
	}
	private connect() {
		if (this.disposed || this.unavailable) return;
		const socket = new WebSocket(conversations.streamURL(this.slug, this.id, this.cursor));
		this.socket = socket;
		socket.onopen = () => {
			if (this.socket === socket) {
				this.connected = true;
				this.retries = 0;
				this.failed = false;
			}
		};
		socket.onmessage = (event) => {
			if (this.socket !== socket) return;
			try {
				this.apply(JSON.parse(event.data));
			} catch {
				socket.close();
			}
		};
		socket.onerror = () => socket.close();
		socket.onclose = () => {
			if (this.socket !== socket || this.disposed) return;
			this.connected = false;
			this.timer = setTimeout(() => void this.reconnect(), Math.min(500 * 2 ** this.retries++, 10000));
		};
	}
	private async reconnect() {
		if (this.disposed) return;
		const version = this.loadVersion;
		try {
			// HTTP revalidation refreshes expired session cookies and removes cached
			// private content when a proxy obscures the WebSocket policy close code.
			let events: ConversationEvent[];
			do {
				events = await conversations.events(this.slug, this.id, this.cursor);
				if (this.disposed || version !== this.loadVersion) return;
				for (const event of events) this.apply(event);
			} while (events.length === 200);
			this.connect();
		} catch (error) {
			if (this.disposed || version !== this.loadVersion) return;
			this.handleError(error);
			if (!this.unavailable && !this.disposed) this.timer = setTimeout(() => void this.reconnect(), 3000);
		}
	}
	async older() {
		if (this.loadingOlder || !this.hasMore || !this.messages.length) return;
		this.loadingOlder = true;
		const version = this.loadVersion;
		try {
			const history = await conversations.history(this.slug, this.id, this.messages[0].sequence);
			if (this.disposed || this.unavailable || version !== this.loadVersion) return;
			const known = new Set(this.messages.map((m) => m.id));
			this.messages = [...history.messages.filter((m) => !known.has(m.id)), ...this.messages];
			this.acceptTools(history.tools ?? []);
			this.acceptApprovals(history.approvals ?? []);
			this.acceptAgentMessages(history.agent_messages ?? []);
			this.hasMore = history.has_more;
		} catch (error) {
			if (version === this.loadVersion) this.handleError(error);
		} finally {
			this.loadingOlder = false;
		}
	}
	private disconnect() {
		clearTimeout(this.timer);
		const socket = this.socket;
		this.socket = null;
		socket?.close();
		this.connected = false;
	}
	dispose() {
		this.disposed = true;
		this.disconnect();
	}
}
