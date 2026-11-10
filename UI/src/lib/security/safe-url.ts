// Link targets read back from stored rich text. Free of DOM and $lib imports
// for the Node unit tests.

/**
 * The URL when it is a web address or one of this app's own API paths (where
 * uploaded files are served), otherwise null. Anything else, such as
 * `javascript:` or `data:`, must not become a link the user can click.
 */
export function safeFileUrl(value: string | null | undefined): string | null {
	const url = value ?? '';
	if (/^https?:\/\//i.test(url) || url.startsWith('/api/')) return url;
	return null;
}
