<script lang="ts">
	import { onMount } from 'svelte';
	import { getIssueGitHubActivity } from '$lib/api/github';
	import type { GitHubIssueActivity } from '$lib/types/github';
	import { formatRelativeTime } from '$lib/utils/format';
	import { GitBranch, GitPullRequest, GitCommitHorizontal, ExternalLink, Copy, Check, ChevronDown } from 'lucide-svelte';
	import { Badge } from '$lib/components/ui/badge';
	import { toast } from 'svelte-sonner';

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
		toast.success('Branch name copied');
		setTimeout(() => { copiedBranch = null; }, 2000);
	}

	function prStateBadge(state: string): 'default' | 'secondary' | 'outline' | 'destructive' {
		switch (state) {
			case 'merged': return 'default';
			case 'open': return 'secondary';
			case 'draft': return 'outline';
			case 'closed': return 'destructive';
			default: return 'outline';
		}
	}
</script>

{#if !loading && hasActivity}
	<div class="border-t border-[var(--app-border)] pt-3">
		<button
			onclick={() => (expanded = !expanded)}
			class="flex w-full items-center gap-2 text-xs font-medium text-[var(--color-text-secondary)]"
		>
			<GitBranch size={13} class="shrink-0" />
			GitHub
			<ChevronDown size={12} class="ml-auto transition-transform {expanded ? '' : '-rotate-90'}" />
		</button>

		{#if expanded && activity}
			<div class="mt-2 space-y-2">
				<!-- Pull Requests -->
				{#each activity.pull_requests as pr}
					<a
						href={pr.html_url}
						target="_blank"
						rel="noopener"
						class="group flex items-start gap-2 rounded-md px-2 py-1.5 text-xs hover:bg-[var(--color-bg-hover)]"
					>
						<GitPullRequest size={13} class="mt-0.5 shrink-0 {pr.state === 'merged' ? 'text-purple-500' : pr.state === 'open' ? 'text-green-500' : 'text-[var(--color-text-tertiary)]'}" />
						<div class="min-w-0 flex-1">
							<div class="flex items-center gap-1.5">
								<span class="truncate text-[var(--color-text-primary)]">{pr.title}</span>
								<Badge variant={prStateBadge(pr.state)} class="shrink-0 text-[9px]">{pr.state}</Badge>
							</div>
							<div class="mt-0.5 flex items-center gap-2 text-[var(--color-text-tertiary)]">
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
					<div class="flex items-center gap-2 rounded-md px-2 py-1.5 text-xs">
						<GitBranch size={13} class="shrink-0 text-[var(--color-text-tertiary)]" />
						<code class="min-w-0 flex-1 truncate rounded bg-[var(--color-bg-tertiary)] px-1.5 py-0.5 text-[var(--color-text-primary)]">{branch.name}</code>
						<button
							onclick={() => copyBranch(branch.name)}
							class="shrink-0 rounded p-0.5 text-[var(--color-text-tertiary)] hover:text-[var(--color-text-secondary)]"
							title="Copy branch name"
						>
							{#if copiedBranch === branch.name}
								<Check size={12} class="text-green-500" />
							{:else}
								<Copy size={12} />
							{/if}
						</button>
						{#if branch.html_url}
							<a href={branch.html_url} target="_blank" rel="noopener" class="shrink-0 text-[var(--color-text-tertiary)] hover:text-[var(--color-text-secondary)]">
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
						class="group flex items-start gap-2 rounded-md px-2 py-1 text-xs hover:bg-[var(--color-bg-hover)]"
					>
						<GitCommitHorizontal size={13} class="mt-0.5 shrink-0 text-[var(--color-text-tertiary)]" />
						<div class="min-w-0 flex-1">
							<span class="truncate text-[var(--color-text-primary)]">{commit.message.split('\n')[0]}</span>
							<div class="mt-0.5 flex items-center gap-2 text-[var(--color-text-tertiary)]">
								<code>{commit.short_sha}</code>
								{#if commit.author_login}
									<span>{commit.author_login}</span>
								{/if}
								<span>{formatRelativeTime(commit.committed_at)}</span>
							</div>
						</div>
					</a>
				{/each}
				{#if activity.commits.length > 5}
					<p class="px-2 text-[10px] text-[var(--color-text-tertiary)]">
						+{activity.commits.length - 5} more commit{activity.commits.length - 5 > 1 ? 's' : ''}
					</p>
				{/if}
			</div>
		{/if}
	</div>
{/if}
