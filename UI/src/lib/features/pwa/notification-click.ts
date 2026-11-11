// Where a clicked notification takes the user. Pure, so the service worker's
// `notificationclick` handler stays a thin wrapper and this is unit tested.

/** Absolute target for a notification's `data.url`, or null unless it is on our origin. */
export function notificationTargetUrl(raw: unknown, origin: string): string | null {
	if (typeof raw !== 'string' || !raw) return null;
	try {
		const url = new URL(raw, origin);
		return url.origin === origin ? url.href : null;
	} catch {
		return null;
	}
}

interface WindowClientLike {
	url: string;
	focused?: boolean;
}

function sameOrigin(url: string, origin: string): boolean {
	try {
		return new URL(url).origin === origin;
	} catch {
		return false;
	}
}

/**
 * The window to reuse: one already on the target, else a same-origin window to
 * navigate (the focused one first). Null means a new window has to be opened.
 */
export function chooseWindowClient<T extends WindowClientLike>(
	clients: readonly T[],
	target: string,
	origin: string
): { client: T; navigate: boolean } | null {
	const onTarget = clients.find((client) => client.url === target);
	if (onTarget) return { client: onTarget, navigate: false };
	const ours = clients.filter((client) => sameOrigin(client.url, origin));
	const client = ours.find((candidate) => candidate.focused) ?? ours[0];
	return client ? { client, navigate: true } : null;
}

/** Asks an open page to route to the target itself instead of reloading. */
export const NOTIFICATION_NAVIGATE = 'NOTIFICATION_NAVIGATE';
export const NOTIFICATION_NAVIGATE_ACK = 'NOTIFICATION_NAVIGATE_ACK';
/** How long the worker waits for a page to confirm before reloading it. */
export const NAVIGATE_ACK_TIMEOUT_MS = 1_000;

/**
 * The in-app path a page should route to for a worker's navigate message, or
 * null when the message is something else or points off our origin.
 */
export function notificationNavigatePath(data: unknown, origin: string): string | null {
	if (!data || typeof data !== 'object') return null;
	const message = data as { type?: unknown; url?: unknown };
	if (message.type !== NOTIFICATION_NAVIGATE) return null;
	const target = notificationTargetUrl(message.url, origin);
	if (!target) return null;
	const url = new URL(target);
	return url.pathname + url.search + url.hash;
}

interface MessagePortLike {
	onmessage: ((event: { data: unknown }) => void) | null;
	close?(): void;
}

export interface NavigationRequestEnv {
	/** A fresh channel: port1 stays with the worker, port2 goes to the page. */
	createChannel: () => { port1: MessagePortLike; port2: unknown };
	setTimeout: (callback: () => void, ms: number) => unknown;
	clearTimeout?: (handle: unknown) => void;
	timeoutMs?: number;
}

interface MessageTarget {
	postMessage(message: unknown, transfer: unknown[]): void;
}

/**
 * Sends the navigate message and resolves to whether the page confirmed it
 * within the timeout. A page that is still loading, or runs a version without
 * the listener, never answers; neither does one that refuses the target.
 */
export function requestClientNavigation(client: MessageTarget, target: string, env: NavigationRequestEnv): Promise<boolean> {
	return new Promise((resolve) => {
		let settled = false;
		let timer: unknown;
		let port: MessagePortLike | undefined;
		const settle = (acknowledged: boolean) => {
			if (settled) return;
			settled = true;
			env.clearTimeout?.(timer);
			try {
				port?.close?.();
			} catch {
				// Already closed.
			}
			resolve(acknowledged);
		};
		try {
			const channel = env.createChannel();
			port = channel.port1;
			port.onmessage = (event) => {
				if ((event.data as { type?: unknown } | null)?.type === NOTIFICATION_NAVIGATE_ACK) settle(true);
			};
			timer = env.setTimeout(() => settle(false), env.timeoutMs ?? NAVIGATE_ACK_TIMEOUT_MS);
			client.postMessage({ type: NOTIFICATION_NAVIGATE, url: target }, [channel.port2]);
		} catch {
			settle(false);
		}
	});
}

export interface NotificationWindowClient extends WindowClientLike, MessageTarget {
	focus(): Promise<unknown>;
	navigate(url: string): Promise<{ focus(): Promise<unknown> } | null>;
}

export type NotificationOpenOutcome = 'focused' | 'routed' | 'navigated' | 'opened' | 'none';

/**
 * Brings the user to a clicked notification's target, cheapest way first:
 * focus a window already there; ask an open window to route in-app (keeping
 * its state); reload that window onto the target; open a new window.
 */
export async function openNotificationTarget<T extends NotificationWindowClient>(input: {
	clients: readonly T[];
	target: string;
	origin: string;
	requestNavigation: (client: T, target: string) => Promise<boolean>;
	openWindow: (target: string) => Promise<unknown>;
}): Promise<NotificationOpenOutcome> {
	const { clients, target, origin } = input;
	const choice = chooseWindowClient(clients, target, origin);
	if (choice) {
		// Focus first, while the click still allows it; a refusal is not final,
		// the fallbacks below focus again.
		const focused = await choice.client.focus().then(
			() => true,
			() => false
		);
		if (!choice.navigate) {
			if (focused) return 'focused';
		} else {
			if (focused && (await input.requestNavigation(choice.client, target).catch(() => false))) return 'routed';
			try {
				// navigate() rejects for windows this worker does not control.
				const navigated = await choice.client.navigate(target);
				if (navigated) {
					await navigated.focus();
					return 'navigated';
				}
			} catch {
				// Fall through to a new window.
			}
		}
	}
	try {
		await input.openWindow(target);
		return 'opened';
	} catch {
		return 'none';
	}
}
