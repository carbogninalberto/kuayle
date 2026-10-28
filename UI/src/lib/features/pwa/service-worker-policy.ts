// What the service worker may cache. It holds only the built app shell and
// public static files; API data, sockets, authenticated responses and page
// HTML always go to the network. This module has no browser or SvelteKit
// imports so it is shared by the worker, the page and unit tests.

export const OFFLINE_PAGE = '/offline.html';
export const SHELL_CACHE_PREFIX = 'kuayle-shell-';

// Public static files worth keeping offline. Release data and the upgrade
// page change with each deployment and are always fetched fresh.
const STATIC_SHELL = /^\/(favicon\.svg|manifest\.webmanifest|offline\.html|icons\/[\w.-]+\.png)$/;

export function precacheList(build: readonly string[], files: readonly string[]): string[] {
	return [...build.filter((path) => path.startsWith('/_app/immutable/')), ...files.filter((path) => STATIC_SHELL.test(path))];
}

export type RequestStrategy = 'cache-first' | 'network-offline-fallback' | 'bypass';

export interface RequestLike {
	url: string;
	method: string;
	mode: string;
	headers: Headers;
}

export function requestStrategy(request: RequestLike, origin: string, shell: ReadonlySet<string>): RequestStrategy {
	const url = new URL(request.url);
	if (request.method !== 'GET' || url.origin !== origin) return 'bypass';
	if (url.pathname === '/api' || url.pathname.startsWith('/api/')) return 'bypass';
	if (request.headers.has('upgrade') || request.headers.has('authorization')) return 'bypass';
	if (request.mode === 'navigate') return 'network-offline-fallback';
	return shell.has(url.pathname) ? 'cache-first' : 'bypass';
}

export function shouldRegisterServiceWorker(environment: {
	dev: boolean;
	supported: boolean;
	webdriver: boolean;
	optIn: boolean;
}): boolean {
	if (environment.dev || !environment.supported) return false;
	// Browser tests mock the API with request routing, which a worker would
	// bypass; they opt in explicitly to test the worker itself.
	return !environment.webdriver || environment.optIn;
}
