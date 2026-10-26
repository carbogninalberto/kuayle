import {
	conversations,
	type Conversation,
	type ConversationMessage,
	type ConversationEvent
} from '$lib/api/conversations';

export class ConversationState {
	conversation = $state<Conversation | null>(null);
	messages = $state<ConversationMessage[]>([]);
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
			this.messages = history.messages;
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
			this.conversation = null;
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
		if (event.type === 'message.created') {
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
