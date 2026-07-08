import packageJson from '../../package.json';

export const currentVersion = packageJson.version;
export const currentVersionLabel = `v${currentVersion}`;
export const currentReleaseUrl = `https://github.com/carbogninalberto/kuayle/releases/tag/${currentVersionLabel}`;
export const releasesManifestUrl = 'https://raw.githubusercontent.com/carbogninalberto/kuayle/main/UI/static/releases.json';

export interface GitHubRelease {
	tag_name: string;
	html_url: string;
	body: string | null;
	published_at: string;
	prerelease: boolean;
	force_upgrade?: boolean;
	minimum_supported_version?: string | null;
	upgrade_url?: string | null;
	upgrade_message?: string | null;
}

export function normalizeVersion(version: string) {
	return version
		.replace(/^v/i, '')
		.split(/[-+]/)[0]
		.split('.')
		.map((part) => Number.parseInt(part, 10) || 0);
}

export function compareVersions(left: string, right: string) {
	const a = normalizeVersion(left);
	const b = normalizeVersion(right);
	const length = Math.max(a.length, b.length);

	for (let i = 0; i < length; i += 1) {
		const delta = (a[i] ?? 0) - (b[i] ?? 0);
		if (delta !== 0) return delta;
	}

	return 0;
}

export function parseReleaseManifest(manifest: unknown): GitHubRelease[] {
	if (Array.isArray(manifest)) return manifest as GitHubRelease[];

	if (manifest && typeof manifest === 'object') {
		const releases = (manifest as { releases?: unknown }).releases;
		if (Array.isArray(releases)) return releases as GitHubRelease[];
	}

	return [];
}

export async function fetchReleases(): Promise<GitHubRelease[]> {
	const response = await fetch(releasesManifestUrl, { cache: 'no-store' });
	if (!response.ok) return [];
	return parseReleaseManifest(await response.json());
}

export function visibleReleases(releases: GitHubRelease[], includePrerelease: boolean): GitHubRelease[] {
	return releases
		.filter((release) => includePrerelease || !release.prerelease)
		.sort((a, b) => compareVersions(b.tag_name, a.tag_name));
}

export function requiresUpgrade(release: GitHubRelease, version = currentVersion) {
	if (release.prerelease) return false;

	const minimumSupported = release.minimum_supported_version?.trim();
	if (minimumSupported) {
		return compareVersions(version, minimumSupported) < 0;
	}

	return release.force_upgrade === true && compareVersions(release.tag_name, version) > 0;
}

export function requiredUpgradeRelease(releases: GitHubRelease[], version = currentVersion): GitHubRelease | null {
	return (
		releases
			.filter((release) => requiresUpgrade(release, version))
			.sort((a, b) => {
				const bVersion = b.minimum_supported_version || b.tag_name;
				const aVersion = a.minimum_supported_version || a.tag_name;
				return compareVersions(bVersion, aVersion);
			})[0] ?? null
	);
}

export function buildChangelog(releases: GitHubRelease[], version = currentVersion): string {
	const newer = releases.filter((release) => compareVersions(release.tag_name, version) > 0);

	if (newer.length === 0) return '';

	const sections = newer.map((release) => {
		const body = (release.body ?? '').trim();
		return `## ${release.tag_name}${release.prerelease ? ' (prerelease)' : ''}\n\n${body || '_No notes._'}`;
	});

	return sections.join('\n\n---\n\n');
}
