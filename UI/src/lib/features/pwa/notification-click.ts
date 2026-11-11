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
