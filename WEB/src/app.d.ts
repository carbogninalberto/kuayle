// See https://svelte.dev/docs/kit/types#app.d.ts
// for information about these interfaces
declare global {
	namespace App {
		// interface Error {}
		// interface Locals {}
		// interface PageData {}
		// interface PageState {}
		// interface Platform {}
	}

	/** Stable releases resolved at build time by vite.config.ts (empty when the manifest was unreachable). */
	const __KUAYLE_RELEASES__: import('$lib/release-manifest').RecentRelease[];
}

export {};
