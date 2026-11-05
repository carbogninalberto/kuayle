<script lang="ts">
	import { ChevronRight, ChevronsDownUp, ChevronsUpDown, FileDiff, Search, X } from '@lucide/svelte';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import * as Collapsible from '$lib/components/ui/collapsible/index.js';
	import { agentTasks } from '$lib/api/agent-tasks';
	import { diffTotals, lineMatches, parseDiff, searchDiff, type DiffFile } from './diff';
	import { m } from '$lib/paraglide/messages.js';

	// Layout decision (KUA-67): the aggregate total lives in the header; search and
	// collapse/expand-all sit in a toolbar pinned above the scrolling list, not a
	// footer. Rationale and evaluated extra controls: docs/kua-67-diff-panel.md.
	// Search only filters/highlights and never overrides `closed`; collapse/expand-all
	// act on the files currently visible, so the two never conflict.

	// An attempt's captured diff against its base branch, beside the chat.
	let {
		slug,
		taskId,
		runId,
		title,
		onclose
	}: { slug: string; taskId: string; runId: string; title: string; onclose: () => void } = $props();

	let files = $state<DiffFile[]>([]);
	let truncated = $state(false);
	let status = $state<'loading' | 'ready' | 'error'>('loading');
	let closed = $state<Record<string, boolean>>({});
	let query = $state('');
	const totals = $derived(diffTotals(files));
	const fileCount = (count: number) => (count === 1 ? m['tasks.diff_file_one']() : m['tasks.diff_files']({ count }));
	const visible = $derived(searchDiff(files, query));
	const searching = $derived(query.trim() !== '');
	const allClosed = $derived(visible.length > 0 && visible.every((f) => closed[f.path]));

	function setAll(collapse: boolean) {
		closed = { ...closed, ...Object.fromEntries(visible.map((f) => [f.path, collapse])) };
	}

	$effect(() => {
		const key = `${taskId}:${runId}`;
		status = 'loading';
		closed = {};
		query = '';
		void agentTasks
			.diff(slug, taskId, runId)
			.then((result) => {
				if (key !== `${taskId}:${runId}`) return;
				files = parseDiff(result.diff);
				truncated = result.truncated;
				status = 'ready';
			})
			.catch(() => (status = 'error'));
	});

	const lineClass = {
		add: 'bg-[var(--status-success)]/10 text-[var(--status-success)]',
		del: 'bg-[var(--status-error)]/10 text-[var(--status-error)]',
		hunk: 'bg-muted text-muted-foreground',
		meta: 'text-muted-foreground',
		context: 'text-foreground'
	};
</script>

<section class="flex h-full min-w-0 flex-col bg-background" aria-label={m['tasks.diff_title']()}>
	<header class="flex shrink-0 items-center gap-2 border-b px-4 py-2">
		<FileDiff class="size-4 shrink-0 text-muted-foreground" />
		<div class="min-w-0 flex-1">
			<h2 class="truncate text-sm font-medium">{m['tasks.diff_title']()}</h2>
			<p class="truncate text-xs text-muted-foreground">{title}</p>
			{#if status === 'ready'}
				<p class="truncate text-xs" data-testid="diff-totals">
					{#if totals.files === 0}
						<span class="text-muted-foreground">{m['tasks.diff_no_changes']()}</span>
					{:else}
						<span class="text-muted-foreground">{fileCount(totals.files)}</span>
						<span class="text-[var(--status-success)]">+{totals.additions}</span>
						<span class="text-[var(--status-error)]">−{totals.deletions}</span>
					{/if}
				</p>
			{/if}
		</div>
		<Button variant="ghost" size="icon-sm" aria-label={m['tasks.diff_close']()} onclick={onclose}><X /></Button>
	</header>
	{#if status === 'ready' && files.length > 0}
		<div class="flex shrink-0 items-center gap-2 border-b px-3 py-2">
			<div class="relative min-w-0 flex-1">
				<Search class="pointer-events-none absolute top-1/2 left-2 size-3.5 -translate-y-1/2 text-muted-foreground" />
				<Input
					type="search"
					bind:value={query}
					placeholder={m['tasks.diff_search']()}
					aria-label={m['tasks.diff_search']()}
					class="h-8 pl-7 text-xs"
				/>
			</div>
			<Button
				variant="ghost"
				size="icon-sm"
				aria-label={allClosed ? m['tasks.diff_expand_all']() : m['tasks.diff_collapse_all']()}
				title={allClosed ? m['tasks.diff_expand_all']() : m['tasks.diff_collapse_all']()}
				disabled={visible.length === 0}
				onclick={() => setAll(!allClosed)}
			>
				{#if allClosed}<ChevronsUpDown />{:else}<ChevronsDownUp />{/if}
			</Button>
		</div>
		{#if searching}
			<p class="shrink-0 border-b px-3 py-1 text-xs text-muted-foreground" role="status" data-testid="diff-match-count">
				{m['tasks.diff_match_count']({ count: visible.length, total: files.length })}
			</p>
		{/if}
	{/if}
	<div class="min-h-0 flex-1 overflow-auto p-3">
		{#if status === 'loading'}
			<p class="text-sm text-muted-foreground" role="status">{m['assistant.loading']()}</p>
		{:else if status === 'error'}
			<p class="text-sm text-destructive" role="alert">{m['tasks.diff_error']()}</p>
		{:else if files.length === 0}
			<p class="text-sm text-muted-foreground">{m['tasks.diff_empty']()}</p>
		{:else if visible.length === 0}
			{#if truncated}<p class="mb-2 text-xs text-[var(--status-warning)]">{m['tasks.diff_truncated']()}</p>{/if}
			<div class="flex flex-col items-start gap-2" role="status">
				<p class="text-sm text-muted-foreground">{m['tasks.diff_no_matches']({ query: query.trim() })}</p>
				<Button variant="outline" size="sm" onclick={() => (query = '')}>{m['tasks.diff_clear_search']()}</Button>
			</div>
		{:else}
			{#if truncated}<p class="mb-2 text-xs text-[var(--status-warning)]">{m['tasks.diff_truncated']()}</p>{/if}
			<div class="space-y-2">
				{#each visible as file (file.path)}
					<Collapsible.Root
						open={!closed[file.path]}
						onOpenChange={(open) => (closed = { ...closed, [file.path]: !open })}
						class="overflow-hidden rounded-md border"
					>
						<Collapsible.Trigger
							class="flex w-full min-w-0 items-center gap-2 bg-muted/40 px-3 py-1.5 text-left text-xs"
						>
							<ChevronRight class="size-3.5 shrink-0 transition-transform {closed[file.path] ? '' : 'rotate-90'}" />
							<span class="min-w-0 flex-1 truncate font-mono">{file.path}</span>
							<span class="shrink-0 text-[var(--status-success)]">+{file.additions}</span>
							<span class="shrink-0 text-[var(--status-error)]">−{file.deletions}</span>
						</Collapsible.Trigger>
						<Collapsible.Content>
							<pre class="overflow-x-auto text-xs leading-5"><code
									>{#each file.lines as line, index (index)}<span
											class="block px-3 whitespace-pre {lineClass[line.kind]} {searching && lineMatches(line.text, query) ? 'bg-[var(--status-warning)]/25' : ''}">{line.text || ' '}</span
										>{/each}</code
								></pre>
						</Collapsible.Content>
					</Collapsible.Root>
				{/each}
			</div>
		{/if}
	</div>
</section>
