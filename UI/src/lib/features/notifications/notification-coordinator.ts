// Every open tab of a workspace receives the same `notification.created`
// event. Without coordination each of them would play the sound, and a hidden
// tab would raise a system notification while the user looks at another one.
// Here the tabs elect exactly one of them to announce each event.
//
// Protocol, over a BroadcastChannel: a tab that receives an event announces a
// claim, collects its peers' claims for a short window, then every tab picks
// the same winner from the same claims. The winner announces that it is done,
// so a tab whose copy of the event arrives late stays quiet. The election and
// the storage helpers are pure; the browser objects are injected.

import type { NotificationEventPayload } from './notification-policy.ts';

export const COORDINATION_CHANNEL = 'kuayle-notifications';
export const COORDINATION_WINDOW_MS = 150;
/** How long a finished event is remembered, for copies that arrive late. */
const EVENT_MEMORY_MS = 60_000;
const SOUND_STORAGE_PREFIX = 'kuayle-notification-sound:';

export interface TabClaim {
	tabId: string;
	/** The tab is on screen. */
	visible: boolean;
	/** The tab may start audio, i.e. it has had a user gesture. */
	audible: boolean;
}

/**
 * The same value in every tab for one published event: the backend's
 * `event_id`, else a key derived from what the event is about.
 */
export function notificationEventId(payload: NotificationEventPayload): string {
	if (typeof payload.event_id === 'string' && payload.event_id) return payload.event_id;
	return `${payload.type}:${payload.issue_id ?? ''}:${payload.title ?? ''}`;
}

/**
 * The tab that announces an event: a visible tab before a hidden one, then one
 * that can play audio, then the lowest tab id so every tab agrees.
 */
export function electWinner(claims: readonly TabClaim[]): string | null {
	let winner: TabClaim | null = null;
	for (const claim of claims) {
		if (
			!winner ||
			(claim.visible !== winner.visible
				? claim.visible
				: claim.audible !== winner.audible
					? claim.audible
					: claim.tabId < winner.tabId)
		) {
			winner = claim;
		}
	}
	return winner?.tabId ?? null;
}

/** Whether the user is looking at any of the tabs that received the event. */
export function anyTabVisible(claims: readonly TabClaim[]): boolean {
	return claims.some((claim) => claim.visible);
}

export interface CoordinationResult {
	/** This tab announces the event; no other tab does. */
	act: boolean;
	/** Some tab that received the event is on screen. */
	visible: boolean;
}

type CoordinationMessage =
	| ({ kind: 'claim'; eventId: string } & TabClaim)
	| { kind: 'done'; eventId: string; tabId: string };

export interface CoordinationChannel {
	postMessage(message: unknown): void;
	onmessage: ((event: { data: unknown }) => void) | null;
	close?(): void;
}

export interface NotificationCoordinatorOptions {
	tabId: string;
	/** Null when BroadcastChannel is unavailable: the tab then acts alone. */
	channel: CoordinationChannel | null;
	isVisible: () => boolean;
	isAudible?: () => boolean;
	windowMs?: number;
	wait?: (ms: number) => Promise<void>;
	now?: () => number;
}

interface EventState {
	claims: Map<string, TabClaim>;
	/** A tab already announced this event. */
	done: boolean;
	/** This tab's own election, so a duplicate delivery joins it. */
	pending?: Promise<CoordinationResult>;
	seenAt: number;
}

function isMessage(data: unknown): data is CoordinationMessage {
	if (!data || typeof data !== 'object') return false;
	const message = data as Record<string, unknown>;
	if (typeof message.eventId !== 'string' || typeof message.tabId !== 'string') return false;
	return message.kind === 'done' || message.kind === 'claim';
}

export function createNotificationCoordinator(options: NotificationCoordinatorOptions) {
	const { tabId, isVisible } = options;
	const isAudible = options.isAudible ?? (() => true);
	const windowMs = options.windowMs ?? COORDINATION_WINDOW_MS;
	const wait = options.wait ?? ((ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms)));
	const now = options.now ?? Date.now;
	const events = new Map<string, EventState>();
	let channel = options.channel;

	function stateFor(eventId: string): EventState {
		const at = now();
		for (const [id, state] of events) {
			if (at - state.seenAt > EVENT_MEMORY_MS) events.delete(id);
		}
		let state = events.get(eventId);
		if (!state) {
			state = { claims: new Map(), done: false, seenAt: at };
			events.set(eventId, state);
		}
		return state;
	}

	function post(message: CoordinationMessage) {
		try {
			channel?.postMessage(message);
		} catch {
			// A closed or broken channel leaves this tab acting alone.
			channel = null;
		}
	}

	if (channel) {
		channel.onmessage = (event) => {
			const message = event.data;
			if (!isMessage(message) || message.tabId === tabId) return;
			const state = stateFor(message.eventId);
			if (message.kind === 'done') state.done = true;
			else state.claims.set(message.tabId, { tabId: message.tabId, visible: !!message.visible, audible: !!message.audible });
		};
	}

	async function elect(eventId: string, state: EventState): Promise<CoordinationResult> {
		const own: TabClaim = { tabId, visible: isVisible(), audible: isAudible() };
		if (!channel) return { act: true, visible: own.visible };
		state.claims.set(tabId, own);
		post({ kind: 'claim', eventId, ...own });
		await wait(windowMs);
		const claims = [...state.claims.values()];
		const visible = anyTabVisible(claims);
		if (state.done || electWinner(claims) !== tabId) return { act: false, visible };
		state.done = true;
		post({ kind: 'done', eventId, tabId });
		return { act: true, visible };
	}

	return {
		/**
		 * Resolves once the tabs that received the event agreed on who announces
		 * it. A second call for the same event never acts again.
		 */
		coordinate(eventId: string): Promise<CoordinationResult> {
			const state = stateFor(eventId);
			if (state.pending) {
				return state.pending.then((result) => ({ act: false, visible: result.visible }));
			}
			if (state.done) return Promise.resolve({ act: false, visible: isVisible() });
			state.pending = elect(eventId, state);
			return state.pending;
		},
		close() {
			try {
				channel?.close?.();
			} catch {
				// Nothing to release.
			}
			channel = null;
		}
	};
}

export type NotificationCoordinator = ReturnType<typeof createNotificationCoordinator>;

/** A BroadcastChannel for the coordinator, or null where there is none. */
export function openCoordinationChannel(name = COORDINATION_CHANNEL): CoordinationChannel | null {
	try {
		const Channel = (globalThis as { BroadcastChannel?: new (name: string) => CoordinationChannel }).BroadcastChannel;
		return Channel ? new Channel(name) : null;
	} catch {
		return null;
	}
}

export function newTabId(): string {
	try {
		return globalThis.crypto.randomUUID();
	} catch {
		return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
	}
}

/** Where the sound player keeps when each event key last played. */
export interface ThrottleStore {
	get(key: string): number | undefined;
	set(key: string, at: number): void;
	delete(key: string): void;
}

interface StorageLike {
	getItem(key: string): string | null;
	setItem(key: string, value: string): void;
	removeItem(key: string): void;
}

/**
 * Last-played times shared by every tab through localStorage, because the
 * elected tab can differ from one event to the next. Falls back to this tab's
 * memory when storage is missing or refuses.
 */
export function createSharedThrottleStore(storage: StorageLike | null | undefined): ThrottleStore {
	const local = new Map<string, number>();
	const read = (key: string): number | undefined => {
		try {
			const raw = storage?.getItem(SOUND_STORAGE_PREFIX + key);
			const value = raw === null || raw === undefined ? NaN : Number(raw);
			return Number.isFinite(value) ? value : undefined;
		} catch {
			return undefined;
		}
	};
	return {
		get(key) {
			const shared = read(key);
			const own = local.get(key);
			if (shared === undefined) return own;
			return own === undefined ? shared : Math.max(shared, own);
		},
		set(key, at) {
			local.set(key, at);
			try {
				storage?.setItem(SOUND_STORAGE_PREFIX + key, String(at));
			} catch {
				// Private mode or a full quota: this tab still throttles itself.
			}
		},
		delete(key) {
			local.delete(key);
			try {
				storage?.removeItem(SOUND_STORAGE_PREFIX + key);
			} catch {
				// See set().
			}
		}
	};
}

/** localStorage, or null where reading the property itself throws. */
export function browserStorage(): StorageLike | null {
	try {
		return (globalThis as { localStorage?: StorageLike }).localStorage ?? null;
	} catch {
		return null;
	}
}
