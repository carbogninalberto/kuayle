import type { Conversation } from '$lib/api/conversations';

// AssistantChat is keyed by workspace, conversation and user, so it remounts on
// every conversation switch. This in-memory cache keeps the sidebar's Recents
// populated while the next conversation's list loads; the server list still
// replaces it on every mount.
const MAX_KEYS = 20;
const cache = new Map<string, Conversation[]>();

export function recentsKey(userId: string | undefined, slug: string, filter: string): string | null {
	return userId ? `${userId}:${slug}:${filter}` : null;
}

export function readRecents(key: string | null): Conversation[] | undefined {
	if (!key) return undefined;
	const items = cache.get(key);
	return items ? [...items] : undefined;
}

export function writeRecents(key: string | null, items: Conversation[]): void {
	if (!key) return;
	// Re-inserting keeps the most recently written key at the end of the Map.
	cache.delete(key);
	cache.set(key, [...items]);
	if (cache.size > MAX_KEYS) {
		const oldest = cache.keys().next().value;
		if (oldest !== undefined) cache.delete(oldest);
	}
}

export function clearRecents(): void {
	cache.clear();
}
