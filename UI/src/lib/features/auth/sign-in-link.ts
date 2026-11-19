const signInParam = 'sign_in';

export function withoutSignInToken(url: URL): URL {
	const clean = new URL(url);
	if (clean.searchParams.has(signInParam)) clean.searchParams.delete(signInParam);
	return clean;
}

export async function consumeSignInLink(
	url: URL,
	workspaceSlug: string | undefined,
	replace: (url: URL) => void,
	exchange: (slug: string, token: string) => Promise<void>
): Promise<void> {
	const token = url.searchParams.get(signInParam);
	if (token === null) return;
	replace(withoutSignInToken(url));
	if (token && workspaceSlug) await exchange(workspaceSlug, token);
}

function relativePath(url: URL): string {
	return url.pathname + url.search + url.hash;
}

export function loginRedirectUrl(url: URL): string {
	const next = relativePath(withoutSignInToken(url));
	return next === '/' ? '/login' : '/login?' + new URLSearchParams({ next });
}

export function safeLoginNext(raw: string | null, origin: string): string | null {
	if (!raw?.startsWith('/') || raw.startsWith('//') || /[\\\s]/.test(raw)) return null;
	try {
		const url = new URL(raw, origin);
		const path = decodeURIComponent(url.pathname);
		if (url.origin !== origin || path.startsWith('//') || /^\/login\/?$/.test(path)) return null;
		return relativePath(withoutSignInToken(url));
	} catch {
		return null;
	}
}
