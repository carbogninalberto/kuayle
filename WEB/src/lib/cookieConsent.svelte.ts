/**
 * Kuayle cookie consent store.
 *
 * The marketing site is statically pre-rendered, so consent is persisted
 * client-side in localStorage and exposed as Svelte 5 reactive state.
 *
 * Categories:
 *  - necessary:   always on (security, session). Not gateable.
 *  - analytics:   optional. Gate any future analytics script on this flag.
 *
 * Storage shape is versioned so future migrations can reset stale values.
 */

export type ConsentValue = boolean | undefined;

export interface ConsentState {
	necessary: true;
	analytics: ConsentValue;
	preferences: ConsentValue;
	marketing: ConsentValue;
	timestamp: number | undefined;
	version: number;
}

const STORAGE_KEY = 'kuayle_cookie_consent';
const CURRENT_VERSION = 1;

const DEFAULTS: ConsentState = {
	necessary: true,
	analytics: undefined,
	preferences: undefined,
	marketing: undefined,
	timestamp: undefined,
	version: CURRENT_VERSION
};

function read(): ConsentState {
	if (typeof localStorage === 'undefined') return { ...DEFAULTS };
	try {
		const raw = localStorage.getItem(STORAGE_KEY);
		if (!raw) return { ...DEFAULTS };
		const parsed = JSON.parse(raw) as Partial<ConsentState>;
		if (parsed.version !== CURRENT_VERSION) return { ...DEFAULTS };
		return { ...DEFAULTS, ...parsed, necessary: true };
	} catch {
		return { ...DEFAULTS };
	}
}

function persist(state: ConsentState) {
	if (typeof localStorage === 'undefined') return;
	try {
		localStorage.setItem(STORAGE_KEY, JSON.stringify(state));
	} catch {
		/* storage unavailable / blocked — fail silently */
	}
}

let state = $state<ConsentState>(read());

function set(partial: Partial<Omit<ConsentState, 'necessary' | 'version' | 'timestamp'>>) {
	state = {
		...state,
		...partial,
		necessary: true,
		version: CURRENT_VERSION,
		timestamp: Date.now()
	};
	persist(state);
}

export const consent = {
	get value(): ConsentState {
		return state;
	},
	/** True once the user has made an explicit choice (accept or reject). */
	get decided(): boolean {
		return state.analytics !== undefined;
	},
	acceptAll() {
		set({ analytics: true, preferences: true, marketing: true });
	},
	rejectAll() {
		set({ analytics: false, preferences: false, marketing: false });
	},
	save(partial: {
		analytics?: boolean;
		preferences?: boolean;
		marketing?: boolean;
	}) {
		set(partial);
	},
	/** Forget the choice — shows the banner again. */
	reset() {
		state = { ...DEFAULTS };
		if (typeof localStorage !== 'undefined') {
			try {
				localStorage.removeItem(STORAGE_KEY);
			} catch {
				/* ignore */
			}
		}
	}
};
