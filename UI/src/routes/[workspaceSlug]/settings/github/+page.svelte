<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import {
		getGitHubStatus,
		getManifestSetup,
		handleManifestCallback,
		getInstallURL,
		handleGitHubCallback,
		disconnectGitHub,
		deleteGitHubApp,
		listGitHubRepos,
		linkGitHubRepos,
		unlinkGitHubRepo,
		listAutoTransitions,
		updateAutoTransitions
	} from '$lib/api/github';
	import type { GitHubStatus, GitHubAvailableRepo, GitHubAutoTransition } from '$lib/types/github';
	import { Button } from '$lib/components/ui/button';
	import { Badge } from '$lib/components/ui/badge';
	import { Switch } from '$lib/components/ui/switch';
	import { Input } from '$lib/components/ui/input';
	import { Checkbox } from '$lib/components/ui/checkbox';
	import { toast } from 'svelte-sonner';
	import { ExternalLink, Plus, Trash2, Loader2, Search } from 'lucide-svelte';
	import { GithubLogoIcon } from 'phosphor-svelte';

	const slug = $derived(page.params.workspaceSlug ?? '');
	let status = $state<GitHubStatus | null>(null);
	let loading = $state(true);
	let availableRepos = $state<GitHubAvailableRepo[]>([]);
	let loadingRepos = $state(false);
	let showRepoSelector = $state(false);
	let selectedRepoIds = $state<Set<number>>(new Set());
	let transitions = $state<GitHubAutoTransition[]>([]);
	let settingUp = $state(false);
	let repoSearch = $state('');
	let savingRepos = $state(false);

	const filteredRepos = $derived(
		repoSearch
			? availableRepos.filter((r) =>
					r.full_name.toLowerCase().includes(repoSearch.toLowerCase())
				)
			: availableRepos
	);

	const allFilteredSelected = $derived(
		filteredRepos.length > 0 && filteredRepos.every((r) => selectedRepoIds.has(r.github_repo_id))
	);

	const someFilteredSelected = $derived(
		!allFilteredSelected && filteredRepos.some((r) => selectedRepoIds.has(r.github_repo_id))
	);

	function toggleSelectAll() {
		const next = new Set(selectedRepoIds);
		if (allFilteredSelected) {
			for (const r of filteredRepos) next.delete(r.github_repo_id);
		} else {
			for (const r of filteredRepos) next.add(r.github_repo_id);
		}
		selectedRepoIds = next;
	}

	onMount(async () => {
		const params = new URLSearchParams(window.location.search);
		const code = params.get('code');
		const installationId = params.get('installation_id');

		// Clean URL immediately to prevent re-execution on HMR reload
		if (code || installationId) {
			const cleanPath = window.location.pathname;
			window.history.replaceState(null, '', cleanPath);
		}

		try {
			if (code) {
				// Prevent double exchange: check if already handled
				const key = `gh_code_${code}`;
				if (sessionStorage.getItem(key)) {
					// Already processed — just load status
				} else {
					sessionStorage.setItem(key, '1');
					await handleManifestCallback(slug, code);
					toast.success('GitHub App created');
				}
			} else if (installationId) {
				await handleGitHubCallback(slug, parseInt(installationId));
				toast.success('GitHub connected');
			}

			// Load current status
			status = await getGitHubStatus(slug);
			if (status.installed) {
				transitions = await listAutoTransitions(slug);
			}
		} catch (err: any) {
			console.error('GitHub setup error:', err);
			if (code || installationId) {
				toast.error(err?.error?.message || 'GitHub setup failed');
			}
		} finally {
			loading = false;
		}
	});

	function handleSetup() {
		settingUp = true;
		// Navigate directly to the backend — it serves an HTML page that auto-posts to GitHub
		window.location.href = `/api/workspaces/${slug}/github/setup`;
	}

	async function handleInstall() {
		try {
			const { url } = await getInstallURL(slug);
			window.location.href = url;
		} catch {
			toast.error('Failed to get install URL');
		}
	}

	async function handleDisconnect() {
		try {
			await disconnectGitHub(slug);
			status = await getGitHubStatus(slug);
			transitions = [];
			toast.success('GitHub disconnected');
		} catch {
			toast.error('Failed to disconnect');
		}
	}

	async function handleDeleteApp() {
		try {
			await deleteGitHubApp(slug);
			status = await getGitHubStatus(slug);
			transitions = [];
			toast.success('GitHub App removed');
		} catch {
			toast.error('Failed to remove app');
		}
	}

	async function loadAvailableRepos() {
		loadingRepos = true;
		showRepoSelector = true;
		repoSearch = '';
		try {
			availableRepos = await listGitHubRepos(slug);
			selectedRepoIds = new Set(availableRepos.filter(r => r.linked).map(r => r.github_repo_id));
		} catch {
			toast.error('Failed to load repos');
		} finally {
			loadingRepos = false;
		}
	}

	function toggleRepo(repoId: number) {
		const next = new Set(selectedRepoIds);
		if (next.has(repoId)) {
			next.delete(repoId);
		} else {
			next.add(repoId);
		}
		selectedRepoIds = next;
	}

	async function saveRepoSelection() {
		savingRepos = true;
		try {
			const newIds = [...selectedRepoIds].filter(id => !availableRepos.find(r => r.github_repo_id === id && r.linked));
			if (newIds.length > 0) {
				try {
					await linkGitHubRepos(slug, newIds);
					toast.success('Repos linked');
				} catch {
					toast.error('Failed to link repos');
				}
			}
			for (const repo of availableRepos.filter(r => r.linked)) {
				if (!selectedRepoIds.has(repo.github_repo_id)) {
					const linked = status?.repos.find(r => r.github_repo_id === repo.github_repo_id);
					if (linked) {
						try { await unlinkGitHubRepo(slug, linked.id); } catch { /* ignore */ }
					}
				}
			}
			status = await getGitHubStatus(slug);
			showRepoSelector = false;
			repoSearch = '';
		} finally {
			savingRepos = false;
		}
	}

	async function handleTransitionToggle(event: string, active: boolean) {
		const updated = transitions.map(t => t.event === event ? { ...t, is_active: active } : t);
		transitions = updated;
		try {
			await updateAutoTransitions(slug, updated);
		} catch {
			toast.error('Failed to update');
		}
	}

	const TRANSITION_LABELS: Record<string, { label: string; description: string }> = {
		branch_created: { label: 'Branch created', description: 'Move issue to In Progress when a branch is created' },
		pr_opened: { label: 'PR opened', description: 'Move issue to In Review when a PR is opened' },
		pr_merged: { label: 'PR merged', description: 'Move issue to Done when a PR is merged' },
	};
</script>

<div class="mx-auto max-w-2xl space-y-8 p-6">
	<div>
		<h2 class="text-lg font-semibold text-[var(--color-text-primary)]">GitHub</h2>
		<p class="mt-1 text-sm text-[var(--color-text-tertiary)]">
			Connect your GitHub repositories to automatically link pull requests, branches, and commits to issues.
		</p>
	</div>

	{#if loading}
		<div class="flex items-center justify-center py-12">
			<Loader2 size={20} class="animate-spin text-[var(--color-text-tertiary)]" />
		</div>
	{:else if !status?.configured}
		<!-- State 1: No app configured — show setup button -->
		<div class="rounded-lg border border-[var(--app-border)] p-6">
			<div class="flex items-center gap-3">
				<div class="flex h-10 w-10 items-center justify-center rounded-lg bg-[var(--color-bg-tertiary)]">
					<GithubLogoIcon size={20} class="text-[var(--color-text-secondary)]" />
				</div>
				<div class="flex-1">
					<h3 class="text-sm font-medium text-[var(--color-text-primary)]">Set up GitHub App</h3>
					<p class="text-xs text-[var(--color-text-tertiary)]">Create a GitHub App to connect your repositories. Takes about 30 seconds.</p>
				</div>
				<Button size="sm" onclick={handleSetup} disabled={settingUp}>
					{#if settingUp}
						<Loader2 size={14} class="animate-spin" />
					{:else}
						Set up
					{/if}
				</Button>
			</div>
		</div>

		<div class="rounded-md bg-[var(--color-bg-secondary)] px-4 py-3">
			<p class="text-xs text-[var(--color-text-tertiary)]">
				Clicking "Set up" will redirect you to GitHub with a pre-filled form. Confirm to create the app, then you'll be redirected back here automatically. No manual configuration needed.
			</p>
		</div>
	{:else if !status?.installed}
		<!-- State 2: App configured but not installed -->
		<div class="space-y-4">
			<div class="rounded-lg border border-[var(--app-border)] p-4">
				<div class="flex items-center justify-between">
					<div class="flex items-center gap-3">
						<div class="flex h-8 w-8 items-center justify-center rounded-lg bg-[var(--color-bg-tertiary)]">
							<GithubLogoIcon size={16} class="text-[var(--color-text-secondary)]" />
						</div>
						<div>
							<span class="text-sm font-medium text-[var(--color-text-primary)]">GitHub App ready</span>
							<p class="text-xs text-[var(--color-text-tertiary)]">Now install it on your GitHub account to connect repositories.</p>
						</div>
					</div>
					<Button size="sm" onclick={handleInstall}>
						Install on GitHub
					</Button>
				</div>
			</div>

			<button
				onclick={handleDeleteApp}
				class="text-xs text-[var(--color-text-tertiary)] hover:text-[var(--color-error)]"
			>
				Remove GitHub App
			</button>
		</div>
	{:else}
		<!-- State 3: Fully connected -->
		<div class="space-y-6">
			<!-- Connection info -->
			<div class="rounded-lg border border-[var(--app-border)] p-4">
				<div class="flex items-center justify-between">
					<div class="flex items-center gap-3">
						<div class="flex h-8 w-8 items-center justify-center rounded-lg bg-[var(--color-bg-tertiary)]">
							<GithubLogoIcon size={16} class="text-[var(--color-text-secondary)]" />
						</div>
						<div>
							<div class="flex items-center gap-2">
								<span class="text-sm font-medium text-[var(--color-text-primary)]">{status.installation?.account_login}</span>
								<Badge variant="outline" class="text-[10px]">{status.installation?.account_type}</Badge>
							</div>
							<p class="text-xs text-[var(--color-text-tertiary)]">GitHub App connected</p>
						</div>
					</div>
					<Button variant="destructive" size="sm" onclick={handleDisconnect}>
						Disconnect
					</Button>
				</div>
			</div>

			<!-- Linked repos -->
			<div>
				<div class="flex items-center justify-between">
					<h3 class="text-sm font-medium text-[var(--color-text-primary)]">Linked Repositories</h3>
					<Button variant="outline" size="sm" onclick={loadAvailableRepos}>
						<Plus size={14} class="mr-1" />
						Manage repos
					</Button>
				</div>

				{#if status.repos.length === 0}
					<p class="mt-3 text-sm text-[var(--color-text-tertiary)]">No repositories linked yet.</p>
				{:else}
					<div class="mt-3 space-y-1">
						{#each status.repos as repo}
							<div class="flex items-center justify-between rounded-md border border-[var(--app-border)] px-3 py-2">
								<div class="flex items-center gap-2">
									<GithubLogoIcon size={14} class="text-[var(--color-text-tertiary)]" />
									<span class="text-sm text-[var(--color-text-primary)]">{repo.full_name}</span>
									<span class="text-xs text-[var(--color-text-tertiary)]">{repo.default_branch}</span>
								</div>
								<a href="https://github.com/{repo.full_name}" target="_blank" rel="noopener" class="text-[var(--color-text-tertiary)] hover:text-[var(--color-text-secondary)]">
									<ExternalLink size={14} />
								</a>
							</div>
						{/each}
					</div>
				{/if}

				{#if showRepoSelector}
					<div class="mt-3 rounded-lg border border-[var(--app-border)] p-4">
						{#if loadingRepos}
							<div class="flex items-center justify-center py-8">
								<Loader2 size={18} class="animate-spin text-[var(--color-text-tertiary)]" />
							</div>
						{:else}
							<!-- Search -->
							<div class="relative">
								<Search size={14} class="absolute left-2.5 top-1/2 -translate-y-1/2 text-[var(--color-text-tertiary)]" />
								<Input
									type="text"
									placeholder="Search repositories..."
									bind:value={repoSearch}
									class="pl-8 h-8 text-sm"
								/>
							</div>

							<!-- Select all -->
							<button
								onclick={toggleSelectAll}
								class="mt-2 flex w-full items-center gap-2.5 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]"
							>
								<Checkbox
									checked={allFilteredSelected}
									indeterminate={someFilteredSelected}
									class="pointer-events-none"
								/>
								<span class="text-xs">Select all{repoSearch ? ' filtered' : ''} ({filteredRepos.length})</span>
							</button>

							<!-- Repo list -->
							<div class="mt-1 max-h-64 space-y-0.5 overflow-y-auto">
								{#each filteredRepos as repo}
									<button
										onclick={() => toggleRepo(repo.github_repo_id)}
										class="flex w-full items-center gap-2.5 rounded-md px-2 py-1.5 text-sm hover:bg-[var(--color-bg-hover)] {selectedRepoIds.has(repo.github_repo_id) ? 'bg-[var(--color-bg-hover)]/50' : ''}"
									>
										<Checkbox
											checked={selectedRepoIds.has(repo.github_repo_id)}
											class="pointer-events-none"
										/>
										<GithubLogoIcon size={14} class="shrink-0 text-[var(--color-text-tertiary)]" />
										<span class="truncate text-[var(--color-text-primary)]">{repo.full_name}</span>
										{#if repo.private}
											<Badge variant="outline" class="ml-auto shrink-0 text-[9px]">Private</Badge>
										{/if}
									</button>
								{:else}
									<p class="py-4 text-center text-xs text-[var(--color-text-tertiary)]">
										{repoSearch ? 'No repositories match your search.' : 'No repositories available.'}
									</p>
								{/each}
							</div>

							<!-- Actions -->
							<div class="mt-3 flex items-center justify-between border-t border-[var(--app-border)] pt-3">
								<span class="text-xs text-[var(--color-text-tertiary)]">
									{selectedRepoIds.size} selected
								</span>
								<div class="flex gap-2">
									<Button variant="outline" size="sm" onclick={() => { showRepoSelector = false; repoSearch = ''; }}>Cancel</Button>
									<Button size="sm" onclick={saveRepoSelection} disabled={savingRepos}>
										{#if savingRepos}
											<Loader2 size={14} class="animate-spin" />
										{:else}
											Save
										{/if}
									</Button>
								</div>
							</div>
						{/if}
					</div>
				{/if}
			</div>

			<!-- Auto-transitions -->
			{#if transitions.length > 0}
				<div>
					<h3 class="text-sm font-medium text-[var(--color-text-primary)]">Automations</h3>
					<p class="mt-1 text-xs text-[var(--color-text-tertiary)]">Automatically transition issue status based on GitHub activity.</p>
					<div class="mt-3 space-y-2">
						{#each transitions as t}
							{@const info = TRANSITION_LABELS[t.event]}
							{#if info}
								<div class="flex items-center justify-between rounded-md border border-[var(--app-border)] px-3 py-2.5">
									<div>
										<span class="text-sm text-[var(--color-text-primary)]">{info.label}</span>
										<p class="text-xs text-[var(--color-text-tertiary)]">{info.description}</p>
									</div>
									<Switch checked={t.is_active} onCheckedChange={(v) => handleTransitionToggle(t.event, v)} />
								</div>
							{/if}
						{/each}
					</div>
				</div>
			{/if}

			<!-- Danger zone -->
			<div class="border-t border-[var(--app-border)] pt-4">
				<button
					onclick={handleDeleteApp}
					class="flex items-center gap-1.5 text-xs text-[var(--color-text-tertiary)] hover:text-[var(--color-error)]"
				>
					<Trash2 size={12} />
					Remove GitHub App entirely
				</button>
			</div>
		</div>
	{/if}
</div>
