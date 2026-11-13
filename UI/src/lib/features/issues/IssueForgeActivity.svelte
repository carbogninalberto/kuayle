<script lang="ts">
	import { Button } from '$lib/components/ui/button/index.js';
	import { onMount } from 'svelte';
	import { getIssueForgeActivity } from '$lib/api/forges';
	import type { ForgeIssueActivity } from '$lib/types/forge';
	import { formatRelativeTime } from '$lib/utils/format';
	import { getLocale } from '$lib/features/i18n/messages';
	import { GitBranch, GitPullRequest, GitCommitHorizontal, ExternalLink, Copy, Check, ChevronRight } from '@lucide/svelte';
	import { appToast } from '$lib/features/toast/toast';

	let { slug, identifier }: { slug: string; identifier: string } = $props();

	let activity = $state<ForgeIssueActivity | null>(null);
	let loading = $state(true);
	let expanded = $state(true);
	let copiedBranch = $state<string | null>(null);

	const hasActivity = $derived(
		activity &&
			(activity.merge_requests.length > 0 ||
				activity.branches.length > 0 ||
				activity.commits.length > 0)
	);

	async function load() {
		try {
			activity = await getIssueForgeActivity(slug, identifier);
		} catch {
			// Forges not connected or no activity — silently ignore.
		} finally {
			loading = false;
		}
	}

	onMount(load);

	onMount(() => {
		function refresh() {
			getIssueForgeActivity(slug, identifier)
				.then((next) => {
					activity = next;
				})
				.catch(() => {});
		}
		window.addEventListener('ws:forge:mr_updated', refresh);
		window.addEventListener('ws:forge:branch_created', refresh);
		window.addEventListener('ws:forge:commit_pushed', refresh);
		return () => {
			window.removeEventListener('ws:forge:mr_updated', refresh);
			window.removeEventListener('ws:forge:branch_created', refresh);
			window.removeEventListener('ws:forge:commit_pushed', refresh);
		};
	});

	function copyBranch(name: string) {
		navigator.clipboard.writeText(name);
		copiedBranch = name;
		appToast.success('Branch name copied');
		setTimeout(() => {
			copiedBranch = null;
		}, 2000);
	}

	function prStateClass(state: string) {
		switch (state) {
			case 'merged':
				return 'bg-purple-500/10 text-purple-400 border-purple-500/20';
			case 'open':
				return 'bg-green-500/10 text-green-400 border-green-500/20';
			case 'draft':
				return 'bg-[var(--color-bg-tertiary)] text-[var(--color-text-tertiary)] border-[var(--app-border)]';
			case 'closed':
				return 'bg-red-500/10 text-red-400 border-red-500/20';
			default:
				return 'bg-[var(--color-bg-tertiary)] text-[var(--color-text-tertiary)] border-[var(--app-border)]';
		}
	}

	const referenceCount = $derived(
		activity
			? activity.merge_requests.length + activity.branches.length + activity.commits.length
			: 0
	);
</script>

{#if !loading && hasActivity}
	<div class="overflow-hidden rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]/60">
		<Button
			variant="plain"
			onclick={() => (expanded = !expanded)}
			class="flex w-full items-center gap-2 px-3 py-2 text-sm text-[var(--color-text-secondary)] hover:text-[var(--color-text-primary)]"
		>
			<ChevronRight size={14} class="transition-transform {expanded ? 'rotate-90' : ''}" />
			<GitBranch size={14} class="shrink-0" />
			<span class="font-medium">Forge</span>
			<span class="rounded-full bg-[var(--color-bg-tertiary)] px-2 py-0.5 text-xs text-[var(--color-text-tertiary)]">
				{referenceCount}
			</span>
		</Button>

		{#if expanded && activity}
			<div class="border-t border-[var(--app-border)]">
				{#each activity.merge_requests as mr}
					<a
						href={mr.web_url || undefined}
						target="_blank"
						rel="noopener noreferrer"
						class="group flex items-start gap-2 px-3 py-2 text-xs transition-colors hover:bg-[var(--color-bg-hover)]"
					>
						<GitPullRequest
							size={13}
							class="mt-0.5 shrink-0 {mr.state === 'merged'
								? 'text-purple-500'
								: mr.state === 'open'
									? 'text-green-500'
									: 'text-[var(--color-text-tertiary)]'}"
						/>
						<div class="min-w-0 flex-1">
							<div class="flex items-center gap-1.5">
								<span class="min-w-0 flex-1 truncate text-[var(--color-text-primary)]">{mr.title}</span>
								<span class="shrink-0 rounded-full border px-1.5 py-0 text-[9px] capitalize leading-4 {prStateClass(mr.state)}">{mr.provider}</span>
								<span class="shrink-0 rounded-full border px-1.5 py-0 text-[9px] leading-4 {prStateClass(mr.state)}">{mr.state}</span>
							</div>
							<div class="mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-[var(--color-text-tertiary)]">
								<span>{mr.repo_full_name}#{mr.number}</span>
								<span>{formatRelativeTime(mr.created_at, getLocale())}</span>
							</div>
						</div>
						<ExternalLink size={12} class="mt-1 shrink-0 text-[var(--color-text-tertiary)] opacity-0 group-hover:opacity-100" />
					</a>
				{/each}

				{#each activity.branches as branch}
					<div class="flex items-center gap-2 px-3 py-2 text-xs transition-colors hover:bg-[var(--color-bg-hover)]">
						<GitBranch size={13} class="shrink-0 text-[var(--color-text-tertiary)]" />
						<div class="min-w-0 flex-1">
							<code class="block truncate text-[var(--color-text-primary)]">{branch.name}</code>
							<div class="mt-0.5 truncate text-[var(--color-text-tertiary)]">{branch.repo_full_name} · {branch.provider}</div>
						</div>
						<Button
							variant="plain"
							onclick={() => copyBranch(branch.name)}
							class="shrink-0 rounded p-1 text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-tertiary)] hover:text-[var(--color-text-secondary)]"
							title="Copy branch name"
						>
							{#if copiedBranch === branch.name}
								<Check size={12} class="text-green-500" />
							{:else}
								<Copy size={12} />
							{/if}
						</Button>
						{#if branch.web_url}
							<a href={branch.web_url} target="_blank" rel="noopener noreferrer" class="shrink-0 rounded p-1 text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-tertiary)] hover:text-[var(--color-text-secondary)]">
								<ExternalLink size={12} />
							</a>
						{/if}
					</div>
				{/each}

				{#each activity.commits.slice(0, 5) as commit}
					<a
						href={commit.web_url || undefined}
						target="_blank"
						rel="noopener noreferrer"
						class="group flex items-start gap-2 px-3 py-2 text-xs transition-colors hover:bg-[var(--color-bg-hover)]"
					>
						<GitCommitHorizontal size={13} class="mt-0.5 shrink-0 text-[var(--color-text-tertiary)]" />
						<div class="min-w-0 flex-1">
							<div class="truncate text-[var(--color-text-primary)]">{commit.message.split('\n')[0]}</div>
							<div class="mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-[var(--color-text-tertiary)]">
								<code>{commit.short_sha}</code>
								<span>{commit.repo_full_name}</span>
								<span>{commit.provider}</span>
								{#if commit.author_login}
									<span>{commit.author_login}</span>
								{/if}
								<span>{formatRelativeTime(commit.committed_at, getLocale())}</span>
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
