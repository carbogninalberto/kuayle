<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import {
		getGitHubStatus,
		getInstallURL,
		handleGitHubCallback,
		disconnectGitHub,
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
	import EmptyState from '$lib/components/shared/EmptyState.svelte';
	import { toast } from 'svelte-sonner';
	import { GitBranch, ExternalLink, Trash2, Plus, Check, RefreshCw } from 'lucide-svelte';

	const slug = $derived(page.params.workspaceSlug ?? '');
	let status = $state<GitHubStatus | null>(null);
	let loading = $state(true);
	let availableRepos = $state<GitHubAvailableRepo[]>([]);
	let loadingRepos = $state(false);
	let showRepoSelector = $state(false);
	let selectedRepoIds = $state<Set<number>>(new Set());
	let transitions = $state<GitHubAutoTransition[]>([]);
	let disconnecting = $state(false);
	let notConfigured = $state(false);

	onMount(async () => {
		try {
			status = await getGitHubStatus(slug);
			if (status.installed) {
				transitions = await listAutoTransitions(slug);
			}
		} catch (err: any) {
			if (err?.status === 404) {
				notConfigured = true;
			}
		} finally {
			loading = false;
		}
	});

	// Handle callback from GitHub App install (check URL params)
	$effect(() => {
		const installationId = page.url.searchParams.get('installation_id');
		if (installationId && slug) {
			handleGitHubCallback(slug, parseInt(installationId)).then(async () => {
				toast.success('GitHub connected');
				status = await getGitHubStatus(slug);
				transitions = await listAutoTransitions(slug);
				// Clean URL
				const url = new URL(window.location.href);
				url.searchParams.delete('installation_id');
				url.searchParams.delete('setup_action');
				window.history.replaceState({}, '', url.toString());
			}).catch(() => {
				toast.error('Failed to connect GitHub');
			});
		}
	});

	async function handleInstall() {
		try {
			const { url } = await getInstallURL(slug);
			window.location.href = url;
		} catch {
			toast.error('Failed to get install URL');
		}
	}

	async function handleDisconnect() {
		disconnecting = true;
		try {
			await disconnectGitHub(slug);
			status = { installed: false, repos: [] };
			transitions = [];
			toast.success('GitHub disconnected');
		} catch {
			toast.error('Failed to disconnect');
		} finally {
			disconnecting = false;
		}
	}

	async function loadAvailableRepos() {
		loadingRepos = true;
		showRepoSelector = true;
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
		const newIds = [...selectedRepoIds].filter(id => !availableRepos.find(r => r.github_repo_id === id && r.linked));
		if (newIds.length > 0) {
			try {
				await linkGitHubRepos(slug, newIds);
				toast.success('Repos linked');
			} catch {
				toast.error('Failed to link repos');
			}
		}

		// Unlink repos that were deselected
		for (const repo of availableRepos.filter(r => r.linked)) {
			if (!selectedRepoIds.has(repo.github_repo_id)) {
				const linked = status?.repos.find(r => r.github_repo_id === repo.github_repo_id);
				if (linked) {
					try {
						await unlinkGitHubRepo(slug, linked.id);
					} catch {
						toast.error(`Failed to unlink ${repo.full_name}`);
					}
				}
			}
		}

		status = await getGitHubStatus(slug);
		showRepoSelector = false;
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

<div class="mx-auto max-w-2xl space-y-8 p-8">
	<div>
		<h2 class="text-lg font-semibold text-[var(--color-text-primary)]">GitHub</h2>
		<p class="mt-1 text-sm text-[var(--color-text-tertiary)]">
			Connect your GitHub repositories to automatically link pull requests, branches, and commits to issues.
		</p>
	</div>

	{#if loading}
		<div class="flex items-center justify-center py-12">
			<span class="text-sm text-[var(--color-text-tertiary)]">Loading...</span>
		</div>
	{:else if notConfigured}
		<EmptyState
			title="GitHub integration not configured"
			description="The server administrator needs to configure GitHub App credentials to enable this integration."
		/>
	{:else if !status?.installed}
		<!-- Not connected -->
		<div class="rounded-lg border border-[var(--app-border)] p-6">
			<div class="flex items-center gap-3">
				<div class="flex h-10 w-10 items-center justify-center rounded-lg bg-[var(--color-bg-tertiary)]">
					<GitBranch size={20} class="text-[var(--color-text-secondary)]" />
				</div>
				<div class="flex-1">
					<h3 class="text-sm font-medium text-[var(--color-text-primary)]">Connect GitHub</h3>
					<p class="text-xs text-[var(--color-text-tertiary)]">Install the Kuayle GitHub App to get started</p>
				</div>
				<Button size="sm" onclick={handleInstall}>
					Connect
				</Button>
			</div>
		</div>
	{:else}
		<!-- Connected -->
		<div class="space-y-6">
			<!-- Connection info -->
			<div class="rounded-lg border border-[var(--app-border)] p-4">
				<div class="flex items-center justify-between">
					<div class="flex items-center gap-3">
						<div class="flex h-8 w-8 items-center justify-center rounded-lg bg-[var(--color-bg-tertiary)]">
							<GitBranch size={16} class="text-[var(--color-text-secondary)]" />
						</div>
						<div>
							<div class="flex items-center gap-2">
								<span class="text-sm font-medium text-[var(--color-text-primary)]">{status.installation?.account_login}</span>
								<Badge variant="outline" class="text-[10px]">
									{status.installation?.account_type}
								</Badge>
							</div>
							<p class="text-xs text-[var(--color-text-tertiary)]">GitHub App connected</p>
						</div>
					</div>
					<Button variant="destructive" size="sm" onclick={handleDisconnect} disabled={disconnecting}>
						{disconnecting ? 'Disconnecting...' : 'Disconnect'}
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
					<p class="mt-3 text-sm text-[var(--color-text-tertiary)]">No repositories linked yet. Click "Manage repos" to select repositories.</p>
				{:else}
					<div class="mt-3 space-y-1">
						{#each status.repos as repo}
							<div class="flex items-center justify-between rounded-md border border-[var(--app-border)] px-3 py-2">
								<div class="flex items-center gap-2">
									<GitBranch size={14} class="text-[var(--color-text-tertiary)]" />
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

				<!-- Repo selector -->
				{#if showRepoSelector}
					<div class="mt-3 rounded-lg border border-[var(--app-border)] p-4">
						{#if loadingRepos}
							<p class="text-sm text-[var(--color-text-tertiary)]">Loading repositories...</p>
						{:else}
							<div class="max-h-64 space-y-1 overflow-y-auto">
								{#each availableRepos as repo}
									<button
										onclick={() => toggleRepo(repo.github_repo_id)}
										class="flex w-full items-center gap-3 rounded-md px-2 py-1.5 text-sm hover:bg-[var(--color-bg-hover)] {selectedRepoIds.has(repo.github_repo_id) ? 'bg-[var(--color-bg-hover)]/50' : ''}"
									>
										<div class="flex h-4 w-4 items-center justify-center rounded border {selectedRepoIds.has(repo.github_repo_id) ? 'border-[var(--app-accent)] bg-[var(--app-accent)]' : 'border-[var(--app-border)]'}">
											{#if selectedRepoIds.has(repo.github_repo_id)}
												<Check size={10} class="text-[var(--app-accent-foreground)]" />
											{/if}
										</div>
										<span class="text-[var(--color-text-primary)]">{repo.full_name}</span>
										{#if repo.private}
											<Badge variant="outline" class="text-[9px]">Private</Badge>
										{/if}
									</button>
								{/each}
							</div>
							<div class="mt-3 flex justify-end gap-2">
								<Button variant="outline" size="sm" onclick={() => (showRepoSelector = false)}>Cancel</Button>
								<Button size="sm" onclick={saveRepoSelection}>Save</Button>
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
		</div>
	{/if}
</div>
