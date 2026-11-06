/**
 * Pure helpers for the Kuayle releases manifest (UI/static/releases.json on main).
 * Shared by the build (vite.config.ts resolves the current release at build time)
 * and the browser (release.svelte.ts refreshes it after hydration).
 */
import { releaseSummary } from './release-summary.ts';

/** Used only when the manifest could not be fetched at build time. */
export const LAST_KNOWN_VERSION = 'v0.2.16';

export const RELEASES_PAGE_URL = 'https://github.com/carbogninalberto/kuayle/releases';

export const RELEASES_MANIFEST_URL =
	'https://raw.githubusercontent.com/carbogninalberto/kuayle/main/UI/static/releases.json';
const RELEASE_VERSION = /^v?\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$/;

interface GitHubRelease {
	tag_name: string;
	html_url?: string;
	prerelease?: boolean;
	published_at?: string;
	summary?: string;
}

export interface RecentRelease {
	version: string;
	url: string;
	publishedAt: string | null;
	summary: string | null;
}


function normalizeVersion(version: string): number[] {
	return version
		.replace(/^v/i, '')
		.split(/[-+]/)[0]
		.split('.')
		.map((part) => Number.parseInt(part, 10) || 0);
}

export function compareVersions(left: string, right: string): number {
	const a = normalizeVersion(left);
	const b = normalizeVersion(right);
	const length = Math.max(a.length, b.length);
	for (let i = 0; i < length; i += 1) {
		const delta = (a[i] ?? 0) - (b[i] ?? 0);
		if (delta !== 0) return delta;
	}
	return 0;
}

export function parseManifest(manifest: unknown): GitHubRelease[] {
	let releases: unknown[] = [];
	if (Array.isArray(manifest)) releases = manifest;
	if (manifest && typeof manifest === 'object') {
		const nestedReleases = (manifest as { releases?: unknown }).releases;
		if (Array.isArray(nestedReleases)) releases = nestedReleases;
	}
	return releases.flatMap((value): GitHubRelease[] => {
		if (!value || typeof value !== 'object') return [];
		const release = value as Record<string, unknown>;
		if (typeof release.tag_name !== 'string' || !RELEASE_VERSION.test(release.tag_name)) return [];
		return [
			{
				tag_name: release.tag_name,
				html_url: typeof release.html_url === 'string' ? release.html_url : undefined,
				prerelease: release.prerelease === true,
				published_at: typeof release.published_at === 'string' ? release.published_at : undefined,
				summary: releaseSummary(release.body)
			}
		];
	});
}

export function releaseUrlFor(tagName: string, value: string | undefined): string {
	const fallback = `${RELEASES_PAGE_URL}/tag/${encodeURIComponent(tagName)}`;
	if (!value) return fallback;

	try {
		const url = new URL(value);
		const path = url.pathname.split('/').slice(1).map((part) => decodeURIComponent(part));
		if (
			url.protocol !== 'https:' ||
			url.hostname.toLowerCase() !== 'github.com' ||
			url.username !== '' ||
			url.password !== '' ||
			url.port !== '' ||
			path.length !== 5 ||
			path[0].toLowerCase() !== 'carbogninalberto' ||
			path[1].toLowerCase() !== 'kuayle' ||
			path[2] !== 'releases' ||
			path[3] !== 'tag' ||
			path[4] !== tagName
		) {
			return fallback;
		}
		return url.href;
	} catch {
		return fallback;
	}
}

/** Stable releases, newest first, with sanitised links and one-line summaries. */
export function stableReleases(manifest: unknown, limit = 4): RecentRelease[] {
	return parseManifest(manifest)
		.filter((release) => !release.prerelease && release.tag_name)
		.sort((a, b) => compareVersions(b.tag_name, a.tag_name))
		.slice(0, limit)
		.map((release) => ({
			version: release.tag_name,
			url: releaseUrlFor(release.tag_name, release.html_url),
			publishedAt: release.published_at ?? null,
			summary: release.summary ?? null
		}));
}
