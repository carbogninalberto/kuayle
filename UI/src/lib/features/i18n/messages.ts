/**
 * App-facing i18n entry point. Import `m`, `getLocale`, `setLocale` and `locales`
 * from here, not from `$lib/paraglide/*`.
 *
 * Paraglide compiles only the full locales, so every message function stays small.
 * Community locales are plain JSON catalogs loaded on startup for the selected locale
 * only; `m` serves their strings and falls back to the compiled English message.
 */
import { m as compiled } from '$lib/paraglide/messages.js';
import {
	getLocale as paraglideGetLocale,
	setLocale as paraglideSetLocale,
	baseLocale,
	isLocale
} from '$lib/paraglide/runtime.js';
import { COMMUNITY_LOCALE_KEY, LOCALES, interpolate, isCommunityLocale } from './locales';

type Catalog = Record<string, string>;

const CATALOG_DIR = '../../../../messages/community/';
const loaders = import.meta.glob<Catalog>('../../../../messages/community/*.json', { import: 'default' });

let community: string | null = null;
let catalog: Catalog | null = null;

export const locales: string[] = Object.keys(LOCALES);

function storedCommunityLocale(): string | null {
	try {
		const code = localStorage.getItem(COMMUNITY_LOCALE_KEY);
		return code && isCommunityLocale(code) ? code : null;
	} catch {
		return null;
	}
}

/** Load the selected community catalog, if any. Runs once before the app starts. */
export async function initLocale(): Promise<void> {
	const code = storedCommunityLocale();
	const load = code ? loaders[`${CATALOG_DIR}${code}.json`] : undefined;
	if (code && load) {
		try {
			catalog = await load();
			community = code;
		} catch {
			// Network or parse failure: keep the compiled English messages.
			catalog = null;
			community = null;
		}
	}
	if (typeof document !== 'undefined') document.documentElement.lang = getLocale();
}

export function getLocale(): string {
	return community ?? paraglideGetLocale();
}

/** Persist the locale and reload so every view renders in it. */
export function setLocale(code: string): void {
	try {
		if (isCommunityLocale(code)) {
			localStorage.setItem(COMMUNITY_LOCALE_KEY, code);
			paraglideSetLocale(baseLocale, { reload: false });
		} else if (isLocale(code)) {
			localStorage.removeItem(COMMUNITY_LOCALE_KEY);
			paraglideSetLocale(code, { reload: false });
		} else {
			return;
		}
	} catch {
		return;
	}
	window.location.reload();
}

// The target is an empty object: bundled module namespaces are frozen, and a Proxy may
// not return a different value for a frozen property.
export const m = new Proxy({} as typeof compiled, {
	has: (_, key) => key in compiled,
	get(_, key) {
		const message = Reflect.get(compiled, key);
		if (!catalog || typeof key !== 'string' || typeof message !== 'function') return message;
		const template = catalog[key];
		if (template === undefined) return message;
		return (params?: Record<string, unknown>) => interpolate(template, params);
	}
});
