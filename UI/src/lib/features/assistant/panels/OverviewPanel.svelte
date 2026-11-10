<script lang="ts">
	import { untrack, type Snippet } from 'svelte';
	import { FileDiff, FolderGit2, GitBranch, GitPullRequest, GitPullRequestCreate } from '@lucide/svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Progress } from '$lib/components/ui/progress/index.js';
	import { agentTasks, type AgentTask, type PublicationView } from '$lib/api/agent-tasks';
	import { listMachineCheckouts } from '$lib/api/dev-machines';
	import type { AgentRun } from '$lib/types/dev-machine';
	import BackgroundTasks from '../BackgroundTasks.svelte';
	import { contextUsage } from '../shell-rules';
	import type { PanelContext } from './panel-host.svelte';
	import { m } from '$lib/features/i18n/messages';

	// The conversation at a glance: where its work happens, how much of the
	// model's context it fills, what its newest coding task changed and the
	// person's background agent work. Everything shown comes from recorded
	// task, checkout and approval data; nothing is inferred from agent text.
	let { context }: { context: PanelContext } = $props();
	const chat = $derived(context.chat);
	const newest = $derived<AgentTask | undefined>(context.board?.tasks[0]);
	const usage = $derived(contextUsage(chat.messages, chat.hasMore));

	let checkout = $state<{ repository: string; branch: string; base: string } | null>(null);
	let run = $state<AgentRun | null>(null);
	let publication = $state<PublicationView | null>(null);
	let disposed = false;
	$effect(() => () => (disposed = true));

	// A delegation approval records the repository its work would use.
	const proposed = $derived.by(() => {
		for (const approval of Object.values(chat.approvals).reverse()) {
			const payload = approval.payload as { repository?: string; base_branch?: string };
			if (approval.action === 'delegate_issue' && payload.repository)
				return { repository: payload.repository, branch: payload.base_branch ?? '', base: payload.base_branch ?? '' };
		}
		return null;
	});
	const environment = $derived(checkout ?? proposed);

	$effect(() => {
		const task = newest;
		const key = [task?.id, task?.machine_id, task?.checkout_id, task?.run_id, task?.state];
		untrack(() => void loadTask(task, key.join(':')));
	});
	let loadedKey = '';
	async function loadTask(task: AgentTask | undefined, key: string) {
		loadedKey = key;
		if (!task) {
			checkout = null;
			run = null;
			publication = null;
			return;
		}
		const slug = context.slug;
		const [checkouts, runs, view] = await Promise.all([
			task.machine_id ? listMachineCheckouts(slug, task.machine_id).catch(() => []) : Promise.resolve([]),
			task.run_id ? agentTasks.runs(slug, task.id).catch(() => null) : Promise.resolve(null),
			task.run_id && ['publishing', 'awaiting_review', 'completed', 'blocked'].includes(task.state)
				? agentTasks.publication(slug, task.id, task.run_id).catch(() => null)
				: Promise.resolve(null)
		]);
		if (disposed || loadedKey !== key) return;
		const found = Array.isArray(checkouts) ? checkouts.find((c) => c.id === task.checkout_id) : undefined;
		checkout = found
			? { repository: found.repository_full_name, branch: found.working_branch, base: found.base_branch }
			: null;
		run = runs?.runs?.find((r) => r.id === task.run_id) ?? null;
		publication = view;
	}
	const changed = $derived(run?.changed_files ?? []);
	const pullRequest = $derived(
		publication?.step?.status === 'succeeded' ? (publication.pull_request_url ?? undefined) : undefined
	);
	// Work waiting for review without a recorded pull request: ask the assistant.
	const canAskForPullRequest = $derived(
		!pullRequest && !!newest && ['awaiting_review', 'blocked'].includes(newest.state) && changed.length > 0
	);
</script>

{#snippet section(title: string, body: Snippet)}
	<div class="border-b px-4 py-3 last:border-b-0">
		<h3 class="mb-2 text-[11px] font-medium tracking-wide text-muted-foreground uppercase">{title}</h3>
		{@render body()}
	</div>
{/snippet}

{#snippet environmentBody()}
	{#if environment}
		<p class="flex min-w-0 items-center gap-2 text-sm">
			<FolderGit2 size={14} class="shrink-0 text-muted-foreground" aria-hidden="true" /><span
				class="truncate font-medium"
				title={environment.repository}>{environment.repository}</span
			>
		</p>
		{#if environment.branch}<p class="mt-1 flex min-w-0 items-center gap-2 text-xs text-muted-foreground">
				<GitBranch size={13} class="shrink-0" aria-hidden="true" /><span
					class="truncate font-mono"
					title={environment.branch}>{environment.branch}</span
				>
			</p>{/if}
	{:else}
		<p class="text-xs text-muted-foreground">{m['assistant.environment_empty']()}</p>
	{/if}
{/snippet}

{#snippet contextBody()}
	<div class="flex items-center gap-3">
		<Progress value={usage.percent} class="h-1.5 flex-1" aria-label={m['assistant.context_usage']()} />
		<span class="shrink-0 text-xs tabular-nums">{usage.percent}%</span>
	</div>
	<p class="mt-1.5 text-xs text-muted-foreground">
		{m['assistant.context_detail']({ used: String(usage.used), limit: String(usage.limit) })}
	</p>
{/snippet}

{#snippet changesBody()}
	{#if newest && (changed.length || pullRequest)}
		<p class="text-sm">{m['assistant.changes_files']({ count: changed.length })}</p>
		{#if changed.length}<ul class="mt-1 space-y-0.5">
				{#each changed.slice(0, 5) as file (file)}<li class="truncate font-mono text-xs text-muted-foreground" title={file}>
						{file}
					</li>{/each}
				{#if changed.length > 5}<li class="text-xs text-muted-foreground">
						{m['assistant.changes_more']({ count: changed.length - 5 })}
					</li>{/if}
			</ul>{/if}
		<div class="mt-2 flex flex-wrap gap-2">
			{#if changed.length && newest.run_id}<Button
					size="sm"
					variant="outline"
					onclick={() =>
						(chat.diffView = {
							taskId: newest.id,
							runId: newest.run_id,
							title: `${chat.issue?.identifier ?? ''} · ${m['tasks.attempt']({ number: newest.generation })}`
						})}><FileDiff />{m['assistant.changes_review']()}</Button
				>{/if}
			{#if pullRequest}<Button size="sm" variant="outline" href={pullRequest} target="_blank" rel="noopener noreferrer"
					><GitPullRequest />{m['assistant.changes_pull_request']()}</Button
				>{:else if canAskForPullRequest}<Button
					size="sm"
					onclick={() =>
						context.draft(m['assistant.changes_create_pr_prompt']({ identifier: chat.issue?.identifier ?? '' }))}
					><GitPullRequestCreate />{m['assistant.changes_create_pr']()}</Button
				>{/if}
		</div>
	{:else}
		<p class="text-xs text-muted-foreground">{m['assistant.changes_empty']()}</p>
	{/if}
{/snippet}

{#snippet tasksBody()}
	<div class="-mx-2"><BackgroundTasks slug={context.slug} items={context.queue} live={chat.liveTasks} /></div>
{/snippet}

<div class="min-w-0">
	{@render section(m['assistant.environment'](), environmentBody)}
	{@render section(m['assistant.context'](), contextBody)}
	{@render section(m['assistant.changes'](), changesBody)}
	{@render section(m['assistant.background_tasks'](), tasksBody)}
</div>
