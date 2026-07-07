<script lang="ts">
	import { onMount } from 'svelte';
	import { getIssueGitHubActivity } from '$lib/api/github';
	import type { GitHubIssueActivity } from '$lib/types/github';
	import { formatRelativeTime } from '$lib/utils/format';
	import { GitBranch, GitPullRequest, GitCommitHorizontal, ExternalLink, Copy, Check, ChevronRight } from 'lucide-svelte';
	import { appToast } from '$lib/features/toast/toast';

	let { slug, identifier }: { slug: string; identifier: string } = $props();

	let activity = $state<GitHubIssueActivity | null>(null);
	let loading = $state(true);
	let expanded = $state(true);
	let copiedBranch = $state<string | null>(null);

	const hasActivity = $derived(
		activity && (activity.pull_requests.length > 0 || activity.branches.length > 0 || activity.commits.length > 0)
	);

	onMount(async () => {
		try {
			activity = await getIssueGitHubActivity(slug, identifier);
		} catch {
			// GitHub not connected or no activity — silently ignore
		} finally {
			loading = false;
		}
	});

	// Listen for WebSocket updates
	onMount(() => {
		function handleGitHubUpdate(e: CustomEvent) {
			getIssueGitHubActivity(slug, identifier).then(a => { activity = a; }).catch(() => {});
		}
		window.addEventListener('ws:github:pr_updated', handleGitHubUpdate as EventListener);
		window.addEventListener('ws:github:branch_created', handleGitHubUpdate as EventListener);
		window.addEventListener('ws:github:commit_pushed', handleGitHubUpdate as EventListener);
		return () => {
			window.removeEventListener('ws:github:pr_updated', handleGitHubUpdate as EventListener);
			window.removeEventListener('ws:github:branch_created', handleGitHubUpdate as EventListener);
			window.removeEventListener('ws:github:commit_pushed', handleGitHubUpdate as EventListener);
		};
	});

	function copyBranch(name: string) {
		navigator.clipboard.writeText(name);
		copiedBranch = name;
		appToast.success('Branch name copied');
		setTimeout(() => { copiedBranch = null; }, 2000);
	}

	function prStateClass(state: string) {
		switch (state) {
			case 'merged': return 'bg-purple-500/10 text-purple-400 border-purple-500/20';
			case 'open': return 'bg-green-500/10 text-green-400 border-green-500/20';
			case 'draft': return 'bg-[var(--color-bg-tertiary)] text-[var(--color-text-tertiary)] border-[var(--app-border)]';
			case 'closed': return 'bg-red-500/10 text-red-400 border-red-500/20';
			default: return 'bg-[var(--color-bg-tertiary)] text-[var(--color-text-tertiary)] border-[var(--app-border)]';
		}
	}

	let referenceCount = $derived(
		activity ? activity.pull_requests.length + activity.branches.length + activity.commits.length : 0
	);
</script>

{#if !loading && hasActivity}
	<div class="overflow-hidden rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]/60">
		<button
			onclick={() => (expanded = !expanded)}
			class="flex w-full items-center gap-2 px-3 py-2 text-sm text-[var(--color-text-secondary)] hover:text-[var(--color-text-primary)]"
		>
			<ChevronRight size={14} class="transition-transform {expanded ? 'rotate-90' : ''}" />
			<GitBranch size={14} class="shrink-0" />
			<span class="font-medium">GitHub</span>
			<span class="rounded-full bg-[var(--color-bg-tertiary)] px-2 py-0.5 text-xs text-[var(--color-text-tertiary)]">
				{referenceCount}
			</span>
		</button>

		{#if expanded && activity}
			<div class="border-t border-[var(--app-border)]">
				<!-- Pull Requests -->
				{#each activity.pull_requests as pr}
					<a
						href={pr.html_url}
						target="_blank"
						rel="noopener"
						class="group flex items-start gap-2 px-3 py-2 text-xs transition-colors hover:bg-[var(--color-bg-hover)]"
					>
						<GitPullRequest size={13} class="mt-0.5 shrink-0 {pr.state === 'merged' ? 'text-purple-500' : pr.state === 'open' ? 'text-green-500' : 'text-[var(--color-text-tertiary)]'}" />
						<div class="min-w-0 flex-1">
							<div class="flex items-center gap-1.5">
								<span class="min-w-0 flex-1 truncate text-[var(--color-text-primary)]">{pr.title}</span>
								<span class="shrink-0 rounded-full border px-1.5 py-0 text-[9px] leading-4 {prStateClass(pr.state)}">{pr.state}</span>
							</div>
							<div class="mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-[var(--color-text-tertiary)]">
								<span>{pr.repo_full_name}#{pr.number}</span>
								<span class="text-green-600">+{pr.additions}</span>
								<span class="text-red-500">-{pr.deletions}</span>
								<span>{formatRelativeTime(pr.created_at)}</span>
							</div>
						</div>
						<ExternalLink size={12} class="mt-1 shrink-0 text-[var(--color-text-tertiary)] opacity-0 group-hover:opacity-100" />
					</a>
				{/each}

				<!-- Branches -->
				{#each activity.branches as branch}
					<div class="flex items-center gap-2 px-3 py-2 text-xs transition-colors hover:bg-[var(--color-bg-hover)]">
						<GitBranch size={13} class="shrink-0 text-[var(--color-text-tertiary)]" />
						<div class="min-w-0 flex-1">
							<code class="block truncate text-[var(--color-text-primary)]">{branch.name}</code>
							<div class="mt-0.5 truncate text-[var(--color-text-tertiary)]">{branch.repo_full_name}</div>
						</div>
						<button
							onclick={() => copyBranch(branch.name)}
							class="shrink-0 rounded p-1 text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-tertiary)] hover:text-[var(--color-text-secondary)]"
							title="Copy branch name"
						>
							{#if copiedBranch === branch.name}
								<Check size={12} class="text-green-500" />
							{:else}
								<Copy size={12} />
							{/if}
						</button>
						{#if branch.html_url}
							<a href={branch.html_url} target="_blank" rel="noopener" class="shrink-0 rounded p-1 text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-tertiary)] hover:text-[var(--color-text-secondary)]">
								<ExternalLink size={12} />
							</a>
						{/if}
					</div>
				{/each}

				<!-- Commits (show last 5) -->
				{#each activity.commits.slice(0, 5) as commit}
					<a
						href={commit.html_url}
						target="_blank"
						rel="noopener"
						class="group flex items-start gap-2 px-3 py-2 text-xs transition-colors hover:bg-[var(--color-bg-hover)]"
					>
						<GitCommitHorizontal size={13} class="mt-0.5 shrink-0 text-[var(--color-text-tertiary)]" />
						<div class="min-w-0 flex-1">
							<div class="truncate text-[var(--color-text-primary)]">{commit.message.split('\n')[0]}</div>
							<div class="mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-[var(--color-text-tertiary)]">
								<code>{commit.short_sha}</code>
								<span>{commit.repo_full_name}</span>
								{#if commit.author_login}
									<span>{commit.author_login}</span>
								{/if}
								<span>{formatRelativeTime(commit.committed_at)}</span>
							</div>
						</div>
					</a>
				{/each}
				{#if activity.commits.length > 5}
					<p class="px-3 py-2 text-[10px] text-[var(--color-text-tertiary)]">
						+{activity.commits.length - 5} more commit{activity.commits.length - 5 > 1 ? 's' : ''}
					</p>
				{/if}
			</div>
		{/if}
	</div>
{/if}
