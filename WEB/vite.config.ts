import adapter from '@sveltejs/adapter-static';
import { sveltekit } from '@sveltejs/kit/vite';
import tailwindcss from '@tailwindcss/vite';
import { defineConfig } from 'vite';
import { RELEASES_MANIFEST_URL, stableReleases, type RecentRelease } from './src/lib/release-manifest';

/**
 * Resolve the current releases once per build so prerendered pages show the real
 * version and recent releases without a manual bump. Offline builds fall back to an
 * empty list (LAST_KNOWN_VERSION in the app) instead of failing.
 */
async function buildReleases(): Promise<RecentRelease[]> {
	try {
		const response = await fetch(RELEASES_MANIFEST_URL, { signal: AbortSignal.timeout(8000) });
		if (!response.ok) throw new Error(`HTTP ${response.status}`);
		return stableReleases(await response.json());
	} catch (error) {
		console.warn(`[web] release manifest unavailable at build time (${(error as Error).message}); using LAST_KNOWN_VERSION`);
		return [];
	}
}

export default defineConfig(async () => ({
	define: {
		__KUAYLE_RELEASES__: JSON.stringify(await buildReleases())
	},
	plugins: [
		tailwindcss(),
		sveltekit({
			compilerOptions: {
				// Force runes mode for the project, except for libraries. Can be removed in svelte 6.
				runes: ({ filename }) =>
					filename.split(/[/\\]/).includes('node_modules') ? undefined : true
			},
			adapter: adapter({
				fallback: undefined,
				pages: 'build',
				assets: 'build',
				precompress: false,
				strict: true
			})
		})
	]
}));
