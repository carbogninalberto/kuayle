// Stored rich text may hold images from any http(s) host. Fetching one tells
// that host who is reading and when, and lets injected content send data out in
// the address. So an image the app does not serve itself is rendered without a
// src: the address moves to REMOTE_IMAGE_ATTRIBUTE and the reader loads it on
// request (see remote-images.action.ts). Stored content is never rewritten.

export const REMOTE_IMAGE_ATTRIBUTE = 'data-remote-src';

const TRUSTED_HOSTS_KEY = 'kuayle.remote-image-hosts';
// Only used to resolve relative addresses where there is no page (tests, prerender).
const NO_PAGE_ORIGIN = 'https://app.invalid';

type ImageAttributes = Pick<Element, 'getAttribute' | 'setAttribute' | 'removeAttribute'>;

/**
 * The host an image would be fetched from, resolved the way the browser does
 * for a page on `appOrigin`. Returns null for the app's own images (uploaded
 * assets, signed asset links, anything same-origin) and '' for an address
 * that is not a fetchable http(s) one.
 */
export function remoteImageHost(src: string, appOrigin: string): string | null {
	const base = appOrigin || NO_PAGE_ORIGIN;
	let url: URL;
	try {
		url = new URL(src, base);
	} catch {
		return '';
	}
	if (url.origin === new URL(base).origin) return null;
	if (url.protocol !== 'http:' && url.protocol !== 'https:') return '';
	return url.host;
}

/** Sanitizer step: a remote image keeps its address but loses its src. */
export function deferRemoteImage(image: ImageAttributes, appOrigin: string): void {
	// Only this function sets the attribute; content cannot supply its own.
	image.removeAttribute(REMOTE_IMAGE_ATTRIBUTE);
	const src = image.getAttribute('src');
	if (!src) return;
	const host = remoteImageHost(src, appOrigin);
	if (host === null) return;
	image.removeAttribute('src');
	if (host) image.setAttribute(REMOTE_IMAGE_ATTRIBUTE, src);
}

// What this reader chose: single images for the session, hosts for this browser.
const allowedSources = new Set<string>();
let trustedHosts: Set<string> | null = null;
const listeners = new Set<() => void>();

function readTrustedHosts(): Set<string> {
	if (trustedHosts) return trustedHosts;
	trustedHosts = new Set();
	try {
		const stored: unknown = JSON.parse(localStorage.getItem(TRUSTED_HOSTS_KEY) ?? '[]');
		if (Array.isArray(stored)) {
			for (const host of stored) if (typeof host === 'string' && host) trustedHosts.add(host);
		}
	} catch {
		// No storage, or an unreadable value: nothing is trusted.
	}
	return trustedHosts;
}

function notify() {
	for (const listener of [...listeners]) listener();
}

export function isRemoteImageAllowed(src: string, appOrigin: string): boolean {
	if (allowedSources.has(src)) return true;
	const host = remoteImageHost(src, appOrigin);
	return !!host && readTrustedHosts().has(host);
}

/** Load this one image wherever it appears, until the app is reloaded. */
export function allowRemoteImage(src: string): void {
	allowedSources.add(src);
	notify();
}

/** Always load images from this host, in this browser. */
export function trustRemoteImageHost(host: string): void {
	if (!host) return;
	const hosts = readTrustedHosts();
	hosts.add(host);
	try {
		localStorage.setItem(TRUSTED_HOSTS_KEY, JSON.stringify([...hosts]));
	} catch {
		// The choice still holds until the app is reloaded.
	}
	notify();
}

/** Runs when the reader allows an image or a host; returns the unsubscribe. */
export function onRemoteImageChoice(listener: () => void): () => void {
	listeners.add(listener);
	return () => void listeners.delete(listener);
}
