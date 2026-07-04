export function emitAppRefresh(resources: string[], slug?: string) {
	if (typeof window === 'undefined') return;
	window.dispatchEvent(
		new CustomEvent('app:refresh', { detail: { slug, resources } })
	);
}
