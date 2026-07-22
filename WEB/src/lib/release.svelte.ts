/**
 * Latest Kuayle release, resolved at runtime.
 *
 * Mirrors the app's mechanism in UI/src/lib/release.ts: the releases
 * manifest is served from the repository's main branch and the first
 * non-prerelease entry by semantic version wins.
 *
 * The prerendered static HTML always contains FALLBACK_VERSION (good for
 * SEO and no-JS clients); after hydration the manifest is fetched once and
 * every consumer updates reactively if a newer release exists.
 */

export const FALLBACK_VERSION = 'v0.1.12';
export const RELEASES_PAGE_URL = 'https://github.com/carbogninalberto/kuayle/releases';

const RELEASES_MANIFEST_URL =
	'https://raw.githubusercontent.com/carbogninalberto/kuayle/main/UI/static/releases.json';

interface GitHubRelease {
	tag_name: string;
	html_url?: string;
	prerelease?: boolean;
}

function normalizeVersion(version: string): number[] {
	return version
		.replace(/^v/i, '')
		.split(/[-+]/)[0]
		.split('.')
		.map((part) => Number.parseInt(part, 10) || 0);
}

function compareVersions(left: string, right: string): number {
	const a = normalizeVersion(left);
	const b = normalizeVersion(right);
	const length = Math.max(a.length, b.length);
	for (let i = 0; i < length; i += 1) {
		const delta = (a[i] ?? 0) - (b[i] ?? 0);
		if (delta !== 0) return delta;
	}
	return 0;
}

function parseManifest(manifest: unknown): GitHubRelease[] {
	if (Array.isArray(manifest)) return manifest as GitHubRelease[];
	if (manifest && typeof manifest === 'object') {
		const releases = (manifest as { releases?: unknown }).releases;
		if (Array.isArray(releases)) return releases as GitHubRelease[];
	}
	return [];
}

let version = $state(FALLBACK_VERSION);
let releaseUrl = $state(`${RELEASES_PAGE_URL}/tag/${FALLBACK_VERSION}`);
let requested = false;

async function load() {
	try {
		const response = await fetch(RELEASES_MANIFEST_URL, { cache: 'no-store' });
		if (!response.ok) return;
		const releases = parseManifest(await response.json());
		const latest = releases
			.filter((release) => !release.prerelease && release.tag_name)
			.sort((a, b) => compareVersions(b.tag_name, a.tag_name))[0];
		if (latest) {
			version = latest.tag_name;
			releaseUrl = latest.html_url || `${RELEASES_PAGE_URL}/tag/${latest.tag_name}`;
		}
	} catch {
		// Network or parse failure: keep the fallback version.
	}
}

/**
 * Reactive latest-release state. Call once per component; the manifest is
 * fetched at most once per page load, in the browser only.
 */
export function useLatestRelease() {
	if (typeof window !== 'undefined' && !requested) {
		requested = true;
		void load();
	}
	return {
		get version() {
			return version;
		},
		get releaseUrl() {
			return releaseUrl;
		}
	};
}
