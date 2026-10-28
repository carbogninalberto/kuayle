<script lang="ts">
	import { ChevronRight, FileDiff, X } from '@lucide/svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import * as Collapsible from '$lib/components/ui/collapsible/index.js';
	import { agentTasks } from '$lib/api/agent-tasks';
	import { parseDiff, type DiffFile } from './diff';
	import { m } from '$lib/paraglide/messages.js';

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

	$effect(() => {
		const key = `${taskId}:${runId}`;
		status = 'loading';
		closed = {};
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
		</div>
		<Button variant="ghost" size="icon-sm" aria-label={m['tasks.diff_close']()} onclick={onclose}><X /></Button>
	</header>
	<div class="min-h-0 flex-1 overflow-auto p-3">
		{#if status === 'loading'}
			<p class="text-sm text-muted-foreground" role="status">{m['assistant.loading']()}</p>
		{:else if status === 'error'}
			<p class="text-sm text-destructive" role="alert">{m['tasks.diff_error']()}</p>
		{:else if files.length === 0}
			<p class="text-sm text-muted-foreground">{m['tasks.diff_empty']()}</p>
		{:else}
			{#if truncated}<p class="mb-2 text-xs text-[var(--status-warning)]">{m['tasks.diff_truncated']()}</p>{/if}
			<div class="space-y-2">
				{#each files as file (file.path)}
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
											class="block px-3 whitespace-pre {lineClass[line.kind]}">{line.text || ' '}</span
										>{/each}</code
								></pre>
						</Collapsible.Content>
					</Collapsible.Root>
				{/each}
			</div>
		{/if}
	</div>
</section>
