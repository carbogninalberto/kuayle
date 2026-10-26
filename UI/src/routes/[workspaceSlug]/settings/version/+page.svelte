<script lang="ts">
	import { onMount } from 'svelte';
	import { AlertTriangle, ExternalLink, RefreshCw, ShieldCheck } from '@lucide/svelte';
	import { m } from '$lib/paraglide/messages.js';
	import { getLocale, setLocale } from '$lib/paraglide/runtime.js';
	import { Button } from '$lib/components/ui/button';
	import * as Dialog from '$lib/components/ui/dialog';
	import { getSystemUpdateStatus, startSystemUpdate, type SystemUpdateStatus } from '$lib/api/system';
	import { authState } from '$lib/features/auth/auth.state.svelte';
	import { appToast } from '$lib/features/toast/toast';
	import { renderMarkdown } from '$lib/markdown';
	import {
		buildChangelog,
		compareVersions,
		currentReleaseUrl,
		currentVersion,
		currentVersionLabel,
		fetchReleases,
		isTrustedReleaseNoteUrl,
		visibleReleases as getVisibleReleases,
		type GitHubRelease
	} from '$lib/release';

	let releases = $state<GitHubRelease[]>([]);
	let includePrerelease = $state(false);
	let loadingReleases = $state(true);
	let releaseError = $state('');
	let updateStatus = $state<SystemUpdateStatus | null>(null);
	let loadingStatus = $state(false);
	let loadedStatus = $state(false);
	let startingUpdate = $state(false);
	let confirmOpen = $state(false);

	const isSysadmin = $derived(authState.user?.is_sysadmin === true);
	const visible = $derived(getVisibleReleases(releases, includePrerelease));
	const latestRelease = $derived(visible[0] ?? null);
	const releaseIsNewer = $derived(latestRelease ? compareVersions(latestRelease.tag_name, currentVersion) > 0 : false);
	const changelogHtml = $derived(renderMarkdown(buildChangelog(visible), isTrustedReleaseNoteUrl));
	const canStartUpdate = $derived(
		isSysadmin && updateStatus?.enabled === true && updateStatus?.running !== true && !startingUpdate
	);

	onMount(() => {
		void loadReleases();
	});

	$effect(() => {
		if (isSysadmin && !loadedStatus) {
			loadedStatus = true;
			void loadUpdateStatus();
		}
	});

	async function loadReleases() {
		loadingReleases = true;
		releaseError = '';
		try {
			releases = await fetchReleases();
			if (releases.length === 0) releaseError = m['settings.version.no_releases']();
		} catch {
			releaseError = m['settings.version.failed_load_releases']();
		} finally {
			loadingReleases = false;
		}
	}

	async function loadUpdateStatus() {
		if (!isSysadmin) return;
		loadingStatus = true;
		try {
			updateStatus = await getSystemUpdateStatus();
		} catch (err: any) {
			updateStatus = null;
			appToast.apiError(err, m['settings.version.failed_updater_status']());
		} finally {
			loadingStatus = false;
		}
	}

	async function runUpdate() {
		startingUpdate = true;
		try {
			const result = await startSystemUpdate();
			updateStatus = { enabled: true, running: result.running, message: result.message };
			confirmOpen = false;
			appToast.success(result.message);
		} catch (err: any) {
			appToast.apiError(err, m['settings.version.failed_start_update']());
		} finally {
			startingUpdate = false;
		}
	}
</script>

<div class="mx-auto max-w-3xl px-8 py-10">
	<div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
		<div>
			<h1 class="text-2xl font-semibold text-[var(--color-text-primary)]">{m['settings.version.title']()}</h1>
			<p class="mt-1 text-sm text-[var(--color-text-tertiary)]">
				{m['settings.version.desc']()}
			</p>
		</div>
		<Button variant="outline" onclick={loadReleases} disabled={loadingReleases}>
			<RefreshCw size={14} class={loadingReleases ? 'animate-spin' : ''} />
			{loadingReleases ? m['settings.version.checking']() : m['settings.version.check_releases']()}
		</Button>
	</div>

	<div class="mt-8 rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
		<div class="flex flex-col gap-4 px-5 py-4 sm:flex-row sm:items-center sm:justify-between">
			<div>
				<p class="text-sm font-medium text-[var(--color-text-primary)]">{m['settings.version.installed']()}</p>
				<p class="text-xs text-[var(--color-text-tertiary)]">{m['settings.version.installed_desc']()}</p>
			</div>
			<div class="flex items-center gap-2">
				<span class="rounded-md border border-[var(--app-border)] bg-[var(--color-bg)] px-2 py-1 font-mono text-sm text-[var(--color-text-primary)]">
					{currentVersionLabel}
				</span>
				<Button variant="outline" size="sm" href={currentReleaseUrl} target="_blank" rel="noopener">
					<ExternalLink size={13} />
					{m['settings.version.release']()}
				</Button>
			</div>
		</div>

		<div class="border-t border-[var(--app-border)]"></div>

		<div class="flex flex-col gap-4 px-5 py-4 sm:flex-row sm:items-center sm:justify-between">
			<div>
				<p class="text-sm font-medium text-[var(--color-text-primary)]">{m['settings.version.latest']()}</p>
				<p class="text-xs text-[var(--color-text-tertiary)]">
					{#if loadingReleases}
						{m['settings.version.checking_manifest']()}
					{:else if releaseError}
						{releaseError}
					{:else if releaseIsNewer}
						{m['settings.version.newer_available']()}
					{:else}
						{m['settings.version.up_to_date']()}
					{/if}
				</p>
			</div>
			<div class="flex items-center gap-2">
				<span class="rounded-md border border-[var(--app-border)] bg-[var(--color-bg)] px-2 py-1 font-mono text-sm text-[var(--color-text-primary)]">
					{latestRelease?.tag_name ?? m['settings.unknown']()}
				</span>
				{#if latestRelease}
					<Button variant="outline" size="sm" href={latestRelease.html_url} target="_blank" rel="noopener">
						<ExternalLink size={13} />
						Open
					</Button>
				{/if}
			</div>
		</div>
	</div>

	<div class="mt-6 rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
		<div class="flex flex-col gap-3 border-b border-[var(--app-border)] px-5 py-4 sm:flex-row sm:items-center sm:justify-between">
			<div>
				<h2 class="text-sm font-medium text-[var(--color-text-primary)]">{m['settings.version.changelog']()}</h2>
				<p class="text-xs text-[var(--color-text-tertiary)]">
					{m['settings.version.changelog_desc']({ version: currentVersionLabel })}
				</p>
			</div>
			<Button variant="plain" type="button"
				class="w-fit rounded-md px-2 py-1 text-xs text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]"
				onclick={() => (includePrerelease = !includePrerelease)}
			>
				{m['settings.version.pre_releases']()}
			</Button>
		</div>
		<div class="px-5 py-4">
			{#if loadingReleases}
				<p class="text-sm text-[var(--color-text-secondary)]">{m['settings.version.loading_changelog']()}</p>
			{:else if changelogHtml}
				<!-- eslint-disable svelte/no-at-html-tags -->
				<div class="changelog-md text-sm leading-relaxed text-[var(--color-text-secondary)]">
					{@html changelogHtml}
				</div>
			{:else}
				<p class="text-sm text-[var(--color-text-secondary)]">{m['settings.version.no_release_notes']()}</p>
			{/if}
		</div>
	</div>

	<div id="system-update" class="mt-6 scroll-mt-6 rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
		<div class="flex items-start gap-3 px-5 py-4">
			{#if isSysadmin}
				<ShieldCheck size={18} class="mt-0.5 shrink-0 text-[var(--app-accent-light)]" />
				<div class="min-w-0 flex-1">
					<h2 class="text-sm font-medium text-[var(--color-text-primary)]">{m['settings.version.system_update']()}</h2>
					<p class="mt-1 text-xs text-[var(--color-text-tertiary)]">
						{m['settings.version.update_desc']()}
					</p>

					<div class="mt-4 rounded-md border border-[var(--app-border)] bg-[var(--color-bg)] px-3 py-2 text-xs text-[var(--color-text-secondary)]">
						{#if loadingStatus}
							{m['settings.version.checking_updater']()}
						{:else if updateStatus?.enabled === false}
							{updateStatus.message ?? m['settings.version.no_updater']()}
						{:else if updateStatus?.running}
							{updateStatus.message ?? m['settings.version.update_running']()}
						{:else if updateStatus}
							{updateStatus.message ?? m['settings.version.updater_ready']()}
						{:else}
							{m['settings.version.updater_unavailable']()}
						{/if}
					</div>

					{#if updateStatus?.enabled === false}
						<div class="mt-3 rounded-md border border-[var(--app-border)] bg-[var(--color-bg)] px-3 py-2 font-mono text-xs text-[var(--color-text-tertiary)]">
							bash selfhosting/update.sh
						</div>
					{/if}

					<div class="mt-4 flex flex-col gap-2 sm:flex-row sm:justify-end">
						<Button variant="outline" onclick={loadUpdateStatus} disabled={loadingStatus}>{m['settings.version.check_updater']()}</Button>
						<Button onclick={() => (confirmOpen = true)} disabled={!canStartUpdate}>
							{startingUpdate ? m['settings.starting']() : updateStatus?.running ? m['settings.version.update_running']() : m['settings.version.run_update']()}
						</Button>
					</div>
				</div>
			{:else}
				<AlertTriangle size={18} class="mt-0.5 shrink-0 text-[var(--color-text-tertiary)]" />
				<div>
					<h2 class="text-sm font-medium text-[var(--color-text-primary)]">{m['settings.version.system_update']()}</h2>
					<p class="mt-1 text-xs text-[var(--color-text-tertiary)]">
						{m['settings.version.non_sysadmin']()}
					</p>
				</div>
			{/if}
		</div>
	</div>
</div>

<Dialog.Root bind:open={confirmOpen}>
	<Dialog.Content class="sm:max-w-md border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
		<Dialog.Header>
			<Dialog.Title>{m['settings.version.run_confirm']()}</Dialog.Title>
			<Dialog.Description>
					{m['settings.version.run_confirm_desc']()}
			</Dialog.Description>
		</Dialog.Header>
		<div class="rounded-md border border-[var(--app-border)] bg-[var(--color-bg)] px-3 py-2 font-mono text-xs text-[var(--color-text-secondary)]">
			bash selfhosting/update.sh
		</div>
		<Dialog.Footer>
			<Button variant="outline" onclick={() => (confirmOpen = false)} disabled={startingUpdate}>{m['settings.cancel']()}</Button>
			<Button onclick={runUpdate} disabled={startingUpdate}>{startingUpdate ? m['settings.starting']() : m['settings.version.start_update']()}</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>

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
	.changelog-md :global(code) {
		font-family: var(--app-font-mono, ui-monospace, SFMono-Regular, Menlo, monospace);
		font-size: 0.85em;
		padding: 0.1rem 0.3rem;
		border-radius: 4px;
		background: var(--color-bg);
		border: 1px solid var(--app-border);
	}
	.changelog-md :global(hr) {
		margin: 1rem 0;
		border: none;
		border-top: 1px solid var(--app-border);
	}
	.changelog-md :global(*:first-child) {
		margin-top: 0;
	}
	.changelog-md :global(*:last-child) {
		margin-bottom: 0;
	}
</style>
