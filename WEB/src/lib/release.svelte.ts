/**
 * Latest Kuayle release.
 *
 * The build resolves the current releases from the manifest on main
 * (vite.config.ts → __KUAYLE_RELEASES__), so prerendered HTML already shows
 * the current version and recent releases with no manual bump. After
 * hydration the manifest is fetched again with bounded retries, so a page
 * built before a newer release still updates in the browser.
 */
import {
	LAST_KNOWN_VERSION,
	RELEASES_MANIFEST_URL,
	RELEASES_PAGE_URL,
	stableReleases,
	type RecentRelease
} from './release-manifest.ts';

export { RELEASES_PAGE_URL, type RecentRelease };

const BUILD_RELEASES: RecentRelease[] =
	typeof __KUAYLE_RELEASES__ !== 'undefined' && Array.isArray(__KUAYLE_RELEASES__) ? __KUAYLE_RELEASES__ : [];

/** Current release at build time; LAST_KNOWN_VERSION only if the build could not reach the manifest. */
export const FALLBACK_VERSION = BUILD_RELEASES[0]?.version ?? LAST_KNOWN_VERSION;

const RELEASE_RETRY_DELAYS_MS = [0, 250, 1000];

let version = $state(FALLBACK_VERSION);
let releaseUrl = $state(BUILD_RELEASES[0]?.url ?? `${RELEASES_PAGE_URL}/tag/${FALLBACK_VERSION}`);
let recent = $state<RecentRelease[]>(BUILD_RELEASES);
let loaded = false;
let request: Promise<void> | null = null;

async function loadOnce(): Promise<boolean> {
	try {
		const response = await fetch(RELEASES_MANIFEST_URL, { cache: 'no-store' });
		if (!response.ok) return false;
		const releases = stableReleases(await response.json());
		const latest = releases[0];
		if (!latest) return false;
		recent = releases;
		version = latest.version;
		releaseUrl = latest.url;
		return true;
	} catch {
		return false;
	}
}

async function loadWithRetry() {
	for (const delay of RELEASE_RETRY_DELAYS_MS) {
		if (delay > 0) await new Promise((resolve) => setTimeout(resolve, delay));
		if (await loadOnce()) {
			loaded = true;
			return;
		}
	}
}

/**
 * Reactive latest-release state. Consumers share one browser request at a
 * time; exhausted requests may be retried by a later component mount.
 */
export function useLatestRelease() {
	if (typeof window !== 'undefined' && !loaded && !request) {
		request = loadWithRetry().finally(() => {
			request = null;
		});
	}
	return {
		get version() {
			return version;
		},
		/** Latest stable releases: from the build, refreshed after hydration. */
		get recent() {
			return recent;
		},
		get releaseUrl() {
			return releaseUrl;
		}
	};
}
