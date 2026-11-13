// Forge links are admin-configured, so only plain http(s) URLs are rendered.
export function safeForgeUrl(value: unknown): string | null {
	if (typeof value !== 'string' || value.trim() !== value || value === '') return null;
	try {
		const url = new URL(value);
		if (
			(url.protocol !== 'https:' && url.protocol !== 'http:') ||
			url.hostname === '' ||
			url.username !== '' ||
			url.password !== ''
		) {
			return null;
		}
		return url.href;
	} catch {
		return null;
	}
}
