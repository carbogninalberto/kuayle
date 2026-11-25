<script lang="ts">
	import { ChevronLeft, ChevronRight, File, RefreshCw } from '@lucide/svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import * as CodeBlock from '$lib/components/ui/code-block/index.js';
	import * as Tabs from '$lib/components/ui/tabs/index.js';
	import { agentTasks, type TaskFileContent } from '$lib/api/agent-tasks';
	import DiffLines from './DiffLines.svelte';
	import type { DiffFile } from './diff';
	import { highlightCap, languageFor, loadHighlighter, tokenLines, type Token } from './code-highlight';
	import { diffOf, fileError, nextPage, previousPage, type FileError } from './task-files';
	import { getLocale, m } from '$lib/features/i18n/messages';

	// One file of a task's files panel at the listed commit (KUA-285): a page of
	// its lines numbered in Code Block, highlighted by lowlight up to a size cap
	// and plain above it; a long file pages by line range. A file the task
	// changed also shows its part of the stored diff.
	let {
		slug,
		taskId,
		commit,
		path,
		changed,
		view = $bindable('file'),
		diff,
		reload
	}: {
		slug: string;
		taskId: string;
		commit: string;
		path: string;
		changed: boolean;
		view?: 'file' | 'diff';
		// The stored diff of the attempt that changed the files, parsed once per listing.
		diff: () => Promise<{ files: DiffFile[]; truncated: boolean }>;
		// Reads the listing again (the file moved on since it was read).
		reload: () => void;
	} = $props();

	// Pages, tokens and the diff are replaced whole: raw state, never proxied line by line.
	let page = $state.raw<TaskFileContent | null>(null);
	let status = $state<'loading' | 'ready' | FileError>('loading');
	let start = $state(1);
	let history = $state<number[]>([]);
	let attempt = $state(0);
	// Highlighted lines of the page, or null to show it plain.
	let tokens = $state.raw<Token[][] | null>(null);
	const highlightable = $derived(!!page && page.content.length <= highlightCap);
	const plainLines = $derived(page ? page.content.replace(/\n$/, '').split('\n') : []);
	const hasNext = $derived(!!page && !!nextPage(page, history));
	const paged = $derived(!!page && (page.start > 1 || hasNext));
	// A cut line: the page says it is truncated, yet the file does not go on.
	const cut = $derived(!!page && page.truncated && !hasNext);
	const name = $derived(path.slice(path.lastIndexOf('/') + 1));
	const count = (value: number) => value.toLocaleString(getLocale());

	$effect(() => {
		const from = start;
		void attempt;
		let stale = false;
		status = 'loading';
		tokens = null;
		agentTasks
			.fileContent(slug, taskId, path, { ref: commit, start: from })
			.then(async (result) => {
				if (stale) return;
				page = result;
				status = 'ready';
				if (result.binary || result.too_large || result.content.length > highlightCap) return;
				const lowlight = await loadHighlighter();
				const language = languageFor(path, lowlight);
				if (stale || !language) return;
				tokens = tokenLines(lowlight.highlight(language, result.content));
			})
			.catch((error) => {
				if (!stale) status = fileError(error);
			});
		return () => (stale = true);
	});

	function turn(to: { start: number; history: number[] } | null) {
		if (!to) return;
		history = to.history;
		start = to.start;
	}

	let stored = $state.raw<{ file?: DiffFile; truncated: boolean } | null>(null);
	let diffStatus = $state<'idle' | 'loading' | 'ready' | 'error'>('idle');
	function loadDiff() {
		diffStatus = 'loading';
		diff()
			.then((result) => {
				stored = { file: diffOf(result.files, path), truncated: result.truncated };
				diffStatus = 'ready';
			})
			.catch(() => (diffStatus = 'error'));
	}
	$effect(() => {
		if (changed && view === 'diff' && diffStatus === 'idle') loadDiff();
	});
</script>

{#snippet note(text: string, tone = 'text-muted-foreground')}
	<p class="px-3 py-2 text-xs {tone}">{text}</p>
{/snippet}

{#snippet failure(kind: FileError | 'error')}
	<div class="flex flex-col items-start gap-2 px-3 py-3" role="alert">
		<p class="text-sm text-muted-foreground">
			{kind === 'denied'
				? m['tasks.files_denied']()
				: kind === 'unavailable'
					? m['tasks.files_unavailable']()
					: kind === 'busy'
						? m['tasks.files_busy']()
						: kind === 'stale'
							? m['tasks.files_stale']()
							: m['tasks.files_content_error']()}
		</p>
		{#if kind === 'stale'}<Button variant="outline" size="sm" onclick={reload}><RefreshCw />{m['tasks.files_reload']()}</Button>
		{:else if kind !== 'denied'}<Button variant="outline" size="sm" onclick={() => attempt++}
				>{m['tasks.files_retry']()}</Button
			>{/if}
	</div>
{/snippet}

{#snippet fileBody()}
	{#if status === 'loading'}
		<p class="px-3 py-2 text-sm text-muted-foreground" role="status">{m['assistant.loading']()}</p>
	{:else if status !== 'ready' || !page}
		{@render failure(status === 'ready' ? 'error' : status)}
	{:else if page.binary}
		{@render note(m['tasks.files_binary']())}
	{:else if page.too_large}
		{@render note(m['tasks.files_too_large']())}
	{:else if page.total_lines === 0}
		{@render note(m['tasks.files_empty_file']())}
	{:else}
		{#if paged}
			<div class="flex shrink-0 items-center gap-2 border-b px-3 py-1.5" data-testid="file-pages">
				<p class="min-w-0 flex-1 truncate text-xs text-muted-foreground">
					{m['tasks.files_lines']({ start: count(page.start), end: count(page.end), total: count(page.total_lines) })}
				</p>
				<Button
					variant="ghost"
					size="icon-sm"
					aria-label={m['tasks.files_previous']()}
					title={m['tasks.files_previous']()}
					disabled={!history.length}
					onclick={() => turn(previousPage(history))}><ChevronLeft /></Button
				>
				<Button
					variant="ghost"
					size="icon-sm"
					aria-label={m['tasks.files_next']()}
					title={m['tasks.files_next']()}
					disabled={!hasNext}
					onclick={() => page && turn(nextPage(page, history))}><ChevronRight /></Button
				>
			</div>
		{/if}
		{#if cut}{@render note(m['tasks.files_line_cut'](), 'text-[var(--status-warning)]')}{/if}
		{#if !highlightable}{@render note(m['tasks.files_plain']())}{/if}
		<CodeBlock.Root size="sm" class="file-code min-h-0 flex-1 rounded-none border-0 dark:bg-transparent">
			<CodeBlock.Content lineNumbers startLine={page.start} class="min-h-0 flex-1" data-selectable="text">
				{#if tokens}
					{#each tokens as line, index (index)}<CodeBlock.Line code={line} />{/each}
				{:else}
					{#each plainLines as line, index (index)}<CodeBlock.Line code={line} />{/each}
				{/if}
			</CodeBlock.Content>
		</CodeBlock.Root>
	{/if}
{/snippet}

{#snippet diffBody()}
	{#if diffStatus === 'loading' || diffStatus === 'idle'}
		<p class="px-3 py-2 text-sm text-muted-foreground" role="status">{m['assistant.loading']()}</p>
	{:else if diffStatus === 'error' || !stored}
		<div class="flex flex-col items-start gap-2 px-3 py-3" role="alert">
			<p class="text-sm text-muted-foreground">{m['tasks.diff_error']()}</p>
			<Button variant="outline" size="sm" onclick={loadDiff}>{m['tasks.files_retry']()}</Button>
		</div>
	{:else if stored.file}
		<div class="min-h-0 flex-1 overflow-auto py-2" data-selectable="text" data-testid="file-diff">
			<DiffLines lines={stored.file.lines} />
		</div>
	{:else}
		{@render note(stored.truncated ? m['tasks.diff_truncated']() : m['tasks.files_diff_missing']())}
	{/if}
{/snippet}

{#snippet header()}
	<File class="size-3.5 shrink-0 text-muted-foreground" />
	<h3 class="min-w-0 flex-1 truncate font-mono text-xs" title={path}>{name}</h3>
{/snippet}

<section class="flex h-full min-h-0 flex-col" aria-label={path} data-testid="file-preview">
	{#if changed}
		<Tabs.Root bind:value={view} class="flex min-h-0 flex-1 flex-col gap-0">
			<div class="flex h-10 shrink-0 items-center gap-2 border-b px-3">
				{@render header()}
				<Tabs.List class="h-7" aria-label={m['tasks.files_view']()}>
					<Tabs.Trigger value="file" class="px-2 text-xs">{m['tasks.files_view_file']()}</Tabs.Trigger>
					<Tabs.Trigger value="diff" class="px-2 text-xs">{m['tasks.files_view_diff']()}</Tabs.Trigger>
				</Tabs.List>
			</div>
			<Tabs.Content value="file" class="flex min-h-0 flex-col">{@render fileBody()}</Tabs.Content>
			<Tabs.Content value="diff" class="flex min-h-0 flex-col">{@render diffBody()}</Tabs.Content>
		</Tabs.Root>
	{:else}
		<div class="flex h-10 shrink-0 items-center gap-2 border-b px-3">{@render header()}</div>
		<div class="flex min-h-0 flex-1 flex-col">{@render fileBody()}</div>
	{/if}
</section>

<style>
	/* lowlight's classes, coloured with the theme's status tokens so every theme keeps its contrast. */
	:global(.file-code .hljs-comment),
	:global(.file-code .hljs-quote) {
		color: var(--muted-foreground);
		font-style: italic;
	}
	:global(.file-code .hljs-keyword),
	:global(.file-code .hljs-selector-tag),
	:global(.file-code .hljs-built_in),
	:global(.file-code .hljs-type),
	:global(.file-code .hljs-doctag),
	:global(.file-code .hljs-meta) {
		color: var(--status-running);
	}
	:global(.file-code .hljs-string),
	:global(.file-code .hljs-regexp),
	:global(.file-code .hljs-addition),
	:global(.file-code .hljs-template-tag) {
		color: var(--status-success);
	}
	:global(.file-code .hljs-number),
	:global(.file-code .hljs-literal),
	:global(.file-code .hljs-symbol),
	:global(.file-code .hljs-bullet),
	:global(.file-code .hljs-attr),
	:global(.file-code .hljs-attribute),
	:global(.file-code .hljs-variable) {
		color: var(--status-warning);
	}
	:global(.file-code .hljs-tag),
	:global(.file-code .hljs-name),
	:global(.file-code .hljs-deletion),
	:global(.file-code .hljs-selector-class),
	:global(.file-code .hljs-selector-id) {
		color: var(--status-error);
	}
	:global(.file-code .hljs-title),
	:global(.file-code .hljs-section) {
		font-weight: 600;
	}
</style>
