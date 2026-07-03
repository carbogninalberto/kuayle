<script lang="ts">
	import { onMount } from 'svelte';
	import { Button } from '$lib/components/ui/button';
	import * as Dialog from '$lib/components/ui/dialog';
	import { currentVersion, currentVersionLabel } from '$lib/release';
	import Info from '@lucide/svelte/icons/info';

	interface GitHubRelease {
		tag_name: string;
		html_url: string;
		body: string | null;
	}

	const DISMISSED_KEY = 'kuayle_release_notice_dismissed';

	let latestRelease = $state<GitHubRelease | null>(null);
	let dialogOpen = $state(false);
	let hasOpened = $state(false);
	const releaseIsNewer = $derived(
		latestRelease ? compareVersions(latestRelease.tag_name, currentVersion) > 0 : false
	);

	$effect(() => {
		if (dialogOpen) {
			hasOpened = true;
			return;
		}

		if (hasOpened && latestRelease) {
			persistDismissed(latestRelease.tag_name);
			latestRelease = null;
			hasOpened = false;
		}
	});

	function normalize(version: string) {
		return version
			.replace(/^v/i, '')
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

	async function loadLatestRelease(forceOpen = false) {
		try {
			const response = await fetch('https://api.github.com/repos/carbogninalberto/kuayle/releases/latest', {
				headers: {
					Accept: 'application/vnd.github+json'
				}
			});

			if (!response.ok) return;

			const release = (await response.json()) as GitHubRelease;

			if (forceOpen || (compareVersions(release.tag_name, currentVersion) > 0 && !isDismissed(release.tag_name))) {
				latestRelease = release;
				dialogOpen = true;
			}
		} catch {
			// Silently ignore release lookup failures.
		}
	}

	onMount(() => {
		void loadLatestRelease();
	});
</script>

{#if latestRelease}
	<Dialog.Root bind:open={dialogOpen}>
		<Dialog.Content
			class="top-4 max-h-[calc(100vh-2rem)] border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-0 sm:top-[10vh] sm:max-w-xl"
		>
			<div class="flex max-h-[calc(100vh-2rem)] flex-col">
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
						<span>Current is <strong class="font-semibold text-[var(--color-text-primary)]">{currentVersionLabel}</strong></span>
					</Dialog.Description>
				</Dialog.Header>

				<div class="min-h-0 overflow-y-auto px-5 py-4">
					<details>
						<summary class="cursor-pointer text-sm font-medium text-[var(--color-text-primary)] outline-none focus:outline-none focus-visible:outline-none focus-visible:ring-0">
							Changelog
						</summary>
						<div class="mt-3 min-w-0 text-sm leading-relaxed text-[var(--color-text-secondary)]">
							{#if latestRelease.body}
								<pre
									class="max-h-[45vh] overflow-auto whitespace-pre-wrap font-sans text-xs leading-relaxed break-words [overflow-wrap:anywhere] sm:text-sm"
								>{latestRelease.body}</pre>
							{:else}
								<p>No notes.</p>
							{/if}
						</div>
					</details>
				</div>

				<div
					class="flex flex-col-reverse gap-2 border-t border-[var(--app-border)] bg-[var(--color-bg)] px-5 py-4 sm:flex-row sm:justify-end"
				>
					<Button variant="outline" onclick={() => (dialogOpen = false)}>Dismiss</Button>
					<Button href={latestRelease.html_url} target="_blank" rel="noopener">Release</Button>
				</div>
			</div>
		</Dialog.Content>
	</Dialog.Root>
{/if}
