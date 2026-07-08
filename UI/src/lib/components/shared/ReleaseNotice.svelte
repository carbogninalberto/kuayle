<script lang="ts">
	import { onMount } from 'svelte';
	import { Button } from '$lib/components/ui/button';
	import * as Dialog from '$lib/components/ui/dialog';
	import { currentVersion, currentVersionLabel, releasesManifestUrl } from '$lib/release';
	import { authState } from '$lib/features/auth/auth.state.svelte';
	import { renderMarkdown } from '$lib/markdown';
	import Info from '@lucide/svelte/icons/info';

	interface GitHubRelease {
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

	const DISMISSED_KEY = 'kuayle_release_notice_dismissed';
	const PRERELEASE_KEY = 'kuayle_release_notice_include_prerelease';

	let allReleases = $state<GitHubRelease[]>([]);
	let latestRelease = $state<GitHubRelease | null>(null);
	let requiredRelease = $state<GitHubRelease | null>(null);
	let changelogHtml = $state('');
	let dialogOpen = $state(false);
	let hasOpened = $state(false);
	let includePrerelease = $state(false);
	let loaded = $state(false);

	const releaseIsNewer = $derived(latestRelease ? compareVersions(latestRelease.tag_name, currentVersion) > 0 : false);
	const requiredVersionLabel = $derived(requiredRelease?.minimum_supported_version || requiredRelease?.tag_name || '');
	const upgradeUrl = $derived(requiredRelease?.upgrade_url || requiredRelease?.html_url || releasesManifestUrl);

	$effect(() => {
		if (requiredRelease) {
			dialogOpen = false;
			hasOpened = false;
			return;
		}

		if (dialogOpen) {
			hasOpened = true;
			return;
		}

		if (hasOpened && latestRelease) {
			persistDismissed(latestRelease.tag_name);
			latestRelease = null;
			changelogHtml = '';
			hasOpened = false;
		}
	});

	function normalize(version: string) {
		return version
			.replace(/^v/i, '')
			.split(/[-+]/)[0]
			.split('.')
			.map((part) => Number.parseInt(part, 10) || 0);
	}

	function compareVersions(left: string, right: string) {
		const a = normalize(left);
		const b = normalize(right);
		const length = Math.max(a.length, b.length);

		for (let i = 0; i < length; i += 1) {
			const delta = (a[i] ?? 0) - (b[i] ?? 0);
			if (delta !== 0) return delta;
		}

		return 0;
	}

	function isDismissed(tagName: string) {
		if (typeof localStorage === 'undefined') return false;
		try {
			return localStorage.getItem(DISMISSED_KEY) === tagName;
		} catch {
			return false;
		}
	}

	function persistDismissed(tagName: string) {
		if (typeof localStorage === 'undefined') return;
		try {
			localStorage.setItem(DISMISSED_KEY, tagName);
		} catch {
			// Storage can be unavailable in restricted browser contexts.
		}
	}

	function isPrereleaseEnabled() {
		if (typeof localStorage === 'undefined') return false;
		try {
			return localStorage.getItem(PRERELEASE_KEY) === '1';
		} catch {
			return false;
		}
	}

	function persistPrerelease(enabled: boolean) {
		if (typeof localStorage === 'undefined') return;
		try {
			localStorage.setItem(PRERELEASE_KEY, enabled ? '1' : '0');
		} catch {
			// ignore
		}
	}

	function visibleReleases(): GitHubRelease[] {
		return allReleases
			.filter((release) => includePrerelease || !release.prerelease)
			.sort((a, b) => compareVersions(b.tag_name, a.tag_name));
	}

	function requiresUpgrade(release: GitHubRelease) {
		if (release.prerelease) return false;

		const minimumSupported = release.minimum_supported_version?.trim();
		if (minimumSupported) {
			return compareVersions(currentVersion, minimumSupported) < 0;
		}

		return release.force_upgrade === true && compareVersions(release.tag_name, currentVersion) > 0;
	}

	function requiredUpgradeRelease(): GitHubRelease | null {
		return (
			allReleases
				.filter(requiresUpgrade)
				.sort((a, b) => {
					const bVersion = b.minimum_supported_version || b.tag_name;
					const aVersion = a.minimum_supported_version || a.tag_name;
					return compareVersions(bVersion, aVersion);
				})[0] ?? null
		);
	}

	function parseReleaseManifest(manifest: unknown): GitHubRelease[] {
		if (Array.isArray(manifest)) return manifest as GitHubRelease[];

		if (manifest && typeof manifest === 'object') {
			const releases = (manifest as { releases?: unknown }).releases;
			if (Array.isArray(releases)) return releases as GitHubRelease[];
		}

		return [];
	}

	function buildChangelog(visible: GitHubRelease[]): string {
		const newer = visible.filter((release) => compareVersions(release.tag_name, currentVersion) > 0);

		if (newer.length === 0) return '';

		const sections = newer.map((release) => {
			const body = (release.body ?? '').trim();
			return `## ${release.tag_name}${release.prerelease ? ' (prerelease)' : ''}\n\n${body || '_No notes._'}`;
		});

		return sections.join('\n\n---\n\n');
	}

	function applyReleases(autoOpen: boolean) {
		const visible = visibleReleases();
		const latest = visible[0] ?? null;
		latestRelease = latest;
		requiredRelease = requiredUpgradeRelease();
		changelogHtml = latest ? renderMarkdown(buildChangelog(visible)) : '';

		if (
			autoOpen &&
			authState.authenticated &&
			!requiredRelease &&
			latest &&
			compareVersions(latest.tag_name, currentVersion) > 0 &&
			!isDismissed(latest.tag_name)
		) {
			dialogOpen = true;
		}
	}

	async function loadReleases(autoOpen = true) {
		loaded = false;
		try {
			const response = await fetch(releasesManifestUrl, { cache: 'no-store' });

			if (!response.ok) return;

			const manifest = await response.json();
			const releases = parseReleaseManifest(manifest);
			if (releases.length === 0) return;

			allReleases = releases;
			applyReleases(autoOpen);
		} catch {
			// Silently ignore release lookup failures.
		} finally {
			loaded = true;
		}
	}

	function togglePrerelease() {
		includePrerelease = !includePrerelease;
		persistPrerelease(includePrerelease);
		if (allReleases.length > 0) {
			// Re-evaluate with the new filter without an extra network request.
			applyReleases(false);
		} else {
			void loadReleases(false);
		}
	}

	onMount(() => {
		includePrerelease = isPrereleaseEnabled();
		void loadReleases(false);
	});

	$effect(() => {
		if (!authState.authenticated) {
			dialogOpen = false;
			hasOpened = false;
			return;
		}

		if (loaded && latestRelease && releaseIsNewer && !requiredRelease && !isDismissed(latestRelease.tag_name)) {
			dialogOpen = true;
		}
	});
</script>

{#if requiredRelease}
	<div class="fixed inset-0 z-[100] flex min-h-dvh items-center justify-center bg-background px-4 py-8 text-foreground">
		<div
			class="relative w-full max-w-md rounded-xl border border-border bg-card text-card-foreground shadow-lg"
			role="alertdialog"
			aria-modal="true"
			aria-labelledby="upgrade-required-title"
			aria-describedby="upgrade-required-description"
			tabindex="-1"
		>
			<div class="space-y-6 p-6 pb-4">
				<div class="flex items-center gap-2 text-sm font-semibold">
					<img src="/favicon.svg" alt="" class="size-8 rounded-md" />
					<span>Kuayle</span>
				</div>

				<div class="space-y-2">
					<p class="text-xs font-medium tracking-wide text-muted-foreground uppercase">Upgrade required</p>
					<h1 id="upgrade-required-title" class="text-2xl leading-tight font-semibold tracking-tight">
						This version is no longer supported
					</h1>
				</div>
			</div>
			<div class="space-y-4 px-6 pb-6 text-sm leading-6 text-muted-foreground">
				<p id="upgrade-required-description">
					Current version: <strong class="font-medium text-foreground">{currentVersionLabel}</strong>. Required version:
					<strong class="font-medium text-foreground">{requiredVersionLabel}</strong> or newer.
				</p>
				<p>
					{requiredRelease.upgrade_message ||
						'Ask the instance owner to upgrade Kuayle, then refresh this page to load the new app version.'}
				</p>
				<div class="rounded-lg border border-border bg-muted px-3 py-2 font-mono text-xs text-muted-foreground">
					bash selfhosting/update.sh
				</div>
			</div>
			<div class="flex flex-col-reverse gap-2 border-t border-border p-6 pt-4 sm:flex-row sm:justify-end">
				<Button variant="outline" onclick={() => window.location.reload()}>Refresh app</Button>
				<Button href={upgradeUrl} target="_blank" rel="noopener">Open release</Button>
			</div>
		</div>
	</div>
{:else if latestRelease}
	<Dialog.Root bind:open={dialogOpen}>
		<Dialog.Content
			class="top-4 max-h-[calc(100dvh-2rem)] border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-0 sm:top-[10dvh] sm:max-h-[80dvh] sm:max-w-xl"
		>
			<div class="flex max-h-[calc(100dvh-2rem)] flex-col sm:max-h-[80dvh]">
				<Dialog.Header class="border-b border-[var(--app-border)] px-5 py-4 pr-12">
					<p class="text-xs font-semibold tracking-widest text-[var(--app-accent-light)] uppercase">
						{releaseIsNewer ? 'Update available' : 'Release'}
					</p>
					<Dialog.Title class="flex items-center gap-2 text-[var(--color-text-primary)]">
						<span aria-hidden="true">{releaseIsNewer ? '🚀' : 'ℹ️'}</span>
						<span>{latestRelease.tag_name}</span>
					</Dialog.Title>
					<Dialog.Description class="flex items-center gap-1.5 text-[var(--color-text-secondary)]">
						<Info class="size-3.5" />
						<span
							>Current is <strong class="font-semibold text-[var(--color-text-primary)]">{currentVersionLabel}</strong
							></span
						>
					</Dialog.Description>
				</Dialog.Header>

				<div class="min-h-0 overflow-y-auto px-5 py-4">
					<details open>
						<summary
							class="cursor-pointer text-sm font-medium text-[var(--color-text-primary)] outline-none focus:outline-none focus-visible:outline-none focus-visible:ring-0"
						>
							Changelog
						</summary>
						<div class="mt-3 flex items-center justify-between gap-2 text-xs text-[var(--color-text-tertiary)]">
							<span>
								Showing changes from <strong class="font-semibold text-[var(--color-text-secondary)]"
									>{currentVersionLabel}</strong
								>
								to <strong class="font-semibold text-[var(--color-text-secondary)]">{latestRelease.tag_name}</strong>
							</span>
							<button
								type="button"
								class="cursor-pointer select-none rounded px-1.5 py-0.5 text-[var(--color-text-tertiary)] transition-colors hover:text-[var(--color-text-secondary)]"
								onclick={togglePrerelease}
								title="Toggle pre-release visibility"
							>
								{includePrerelease ? 'Hide' : 'Show'} pre-releases
							</button>
						</div>
						{#if changelogHtml}
							<!-- eslint-disable svelte/no-at-html-tags -->
							<div class="changelog-md mt-3 text-sm leading-relaxed text-[var(--color-text-secondary)]">
								{@html changelogHtml}
							</div>
						{:else}
							<p class="mt-3 text-sm text-[var(--color-text-secondary)]">No notes.</p>
						{/if}
					</details>
				</div>

				<div
					class="flex flex-col-reverse gap-2 border-t border-[var(--app-border)] bg-[var(--color-bg)] px-5 py-4 sm:flex-row sm:justify-end"
				>
					<Button variant="outline" onclick={() => void loadReleases(false)}>Check again</Button>
					<Button variant="outline" onclick={() => (dialogOpen = false)}>Dismiss</Button>
					<Button href={latestRelease.html_url} target="_blank" rel="noopener">Release</Button>
				</div>
			</div>
		</Dialog.Content>
	</Dialog.Root>
{/if}

<style>
	.changelog-md :global(h1),
	.changelog-md :global(h2),
	.changelog-md :global(h3),
	.changelog-md :global(h4) {
		color: var(--color-text-primary);
		font-weight: 600;
		line-height: 1.3;
		margin: 1rem 0 0.5rem;
	}
	.changelog-md :global(h1) {
		font-size: 1.1rem;
	}
	.changelog-md :global(h2) {
		font-size: 1rem;
	}
	.changelog-md :global(h3) {
		font-size: 0.9rem;
	}
	.changelog-md :global(h4) {
		font-size: 0.85rem;
	}
	.changelog-md :global(p) {
		margin: 0.4rem 0;
	}
	.changelog-md :global(ul),
	.changelog-md :global(ol) {
		margin: 0.4rem 0;
		padding-left: 1.25rem;
	}
	.changelog-md :global(ul) {
		list-style: disc;
	}
	.changelog-md :global(ol) {
		list-style: decimal;
	}
	.changelog-md :global(li) {
		margin: 0.2rem 0;
	}
	.changelog-md :global(a) {
		color: var(--app-accent-light);
		text-decoration: underline;
		text-underline-offset: 2px;
	}
	.changelog-md :global(a:hover) {
		opacity: 0.85;
	}
	.changelog-md :global(code) {
		font-family: var(--app-font-mono, ui-monospace, SFMono-Regular, Menlo, monospace);
		font-size: 0.85em;
		padding: 0.1rem 0.3rem;
		border-radius: 4px;
		background: var(--color-bg);
		border: 1px solid var(--app-border);
	}
	.changelog-md :global(pre) {
		margin: 0.5rem 0;
		padding: 0.6rem 0.75rem;
		overflow-x: auto;
		border-radius: 6px;
		background: var(--color-bg);
		border: 1px solid var(--app-border);
	}
	.changelog-md :global(pre code) {
		padding: 0;
		border: none;
		background: transparent;
	}
	.changelog-md :global(blockquote) {
		margin: 0.5rem 0;
		padding-left: 0.75rem;
		border-left: 2px solid var(--app-border);
		color: var(--color-text-tertiary);
	}
	.changelog-md :global(hr) {
		margin: 1rem 0;
		border: none;
		border-top: 1px solid var(--app-border);
	}
	.changelog-md :global(img) {
		max-width: 100%;
		border-radius: 6px;
	}
	.changelog-md :global(h2:first-child),
	.changelog-md :global(*:first-child) {
		margin-top: 0;
	}
	.changelog-md :global(*:last-child) {
		margin-bottom: 0;
	}
</style>
