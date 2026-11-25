<script lang="ts">
	import { RefreshCw, Search } from '@lucide/svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import * as Resizable from '$lib/components/ui/resizable/index.js';
	import { agentTasks, type AgentTask, type TaskFiles } from '$lib/api/agent-tasks';
	import FileTreeNode from './FileTreeNode.svelte';
	import FilePreview from './FilePreview.svelte';
	import { fileTree } from './agent-trace';
	import { parseDiff, type DiffFile } from './diff';
	import { fileError, folderOpen, matchFiles, type FileError } from './task-files';
	import { getLocale, m } from '$lib/features/i18n/messages';

	// A task's files at its head commit (KUA-285): a tree of what the commit
	// tracks, the files the task changed marked, filtered by path, and a preview
	// of the chosen file below it. Only the task's creator may read them; the
	// server decides and the panel says so.
	let { slug, task }: { slug: string; task: AgentTask } = $props();

	let listing = $state<TaskFiles | null>(null);
	let status = $state<'loading' | 'ready' | FileError>('loading');
	let reloads = $state(0);
	let query = $state('');
	// Folders the reader opened or closed, and those toggled during the current filter.
	let toggled = $state<Record<string, boolean>>({});
	let filterToggled = $state<Record<string, boolean>>({});
	let selected = $state<string | null>(null);
	let view = $state<'file' | 'diff'>('file');
	let stored: Promise<{ files: DiffFile[]; truncated: boolean }> | null = null;

	const entries = $derived(listing?.files ?? []);
	const filtering = $derived(query.trim() !== '');
	const matched = $derived(matchFiles(entries, query));
	const tree = $derived(fileTree(matched.files, matched.files.length));
	const changedCount = $derived(entries.filter((entry) => entry.changed).length);
	const current = $derived(entries.find((entry) => entry.path === selected));
	// The task object is replaced on every update; only another task reloads.
	const taskId = $derived(task.id);

	$effect(() => {
		const id = taskId;
		void reloads;
		let stale = false;
		status = 'loading';
		stored = null;
		agentTasks
			.files(slug, id)
			.then((result) => {
				if (stale) return;
				listing = result;
				status = 'ready';
				if (selected && !result.files.some((entry) => entry.path === selected)) selected = null;
			})
			.catch((error) => {
				if (!stale) status = fileError(error);
			});
		return () => (stale = true);
	});

	function setQuery(value: string) {
		query = value;
		filterToggled = {};
	}

	const open = (path: string, depth: number) => folderOpen(path, depth, filtering ? filterToggled : toggled, filtering);
	function toggle(path: string, value: boolean) {
		if (filtering) filterToggled = { ...filterToggled, [path]: value };
		else toggled = { ...toggled, [path]: value };
	}

	// The diff of the newest attempt that recorded changed files: the same
	// attempt the listing's changed marks come from.
	function diff() {
		stored ??= agentTasks
			.runs(slug, task.id)
			.then(async ({ runs }) => {
				const run = runs.find((r) => r.changed_files?.length) ?? runs.find((r) => r.id === task.run_id);
				if (!run) return { files: [], truncated: false };
				const result = await agentTasks.diff(slug, task.id, run.id);
				return { files: parseDiff(result.diff), truncated: result.truncated };
			})
			.catch((error) => {
				stored = null;
				throw error;
			});
		return stored;
	}

	const count = (value: number) => value.toLocaleString(getLocale());
	const message: Record<FileError, () => string> = {
		denied: m['tasks.files_denied'],
		unavailable: m['tasks.files_unavailable'],
		busy: m['tasks.files_busy'],
		stale: m['tasks.files_error'],
		error: m['tasks.files_error']
	};
</script>

<div class="flex h-full min-h-0 flex-col" data-testid="task-files">
	<div class="shrink-0 space-y-2 border-b px-3 py-2">
		<div class="flex min-h-8 items-center gap-2">
			<p class="min-w-0 flex-1 truncate text-xs text-muted-foreground" data-testid="files-summary">
				{#if status === 'ready' && listing}
					{entries.length === 1
						? m['tasks.files_count_one']({ count: count(1) })
						: m['tasks.files_count']({ count: count(entries.length) })}{#if changedCount}{' · '}<span
							class="text-foreground">{m['tasks.files_changed_count']({ count: count(changedCount) })}</span
						>{/if}{' · '}<span title={listing.commit}
						>{m['tasks.files_at_commit']({ commit: listing.commit.slice(0, 7) })}</span
					>
				{/if}
			</p>
			<Button
				variant="ghost"
				size="icon-sm"
				aria-label={m['tasks.files_reload']()}
				title={m['tasks.files_reload']()}
				disabled={status === 'loading'}
				onclick={() => reloads++}><RefreshCw /></Button
			>
		</div>
		{#if status === 'ready' && entries.length}
			<div class="relative">
				<Search class="pointer-events-none absolute top-1/2 left-2 size-3.5 -translate-y-1/2 text-muted-foreground" />
				<Input
					type="search"
					bind:value={() => query, setQuery}
					placeholder={m['tasks.files_filter']()}
					aria-label={m['tasks.files_filter']()}
					class="h-8 pl-7 text-xs"
				/>
			</div>
		{/if}
	</div>
	{#if status === 'loading'}
		<p class="px-3 py-3 text-sm text-muted-foreground" role="status">{m['assistant.loading']()}</p>
	{:else if status !== 'ready' || !listing}
		<div class="flex flex-col items-start gap-2 px-3 py-3" role="alert" data-testid="files-error">
			<p class="text-sm text-muted-foreground">{message[status === 'ready' ? 'error' : status]()}</p>
			{#if status !== 'denied'}<Button variant="outline" size="sm" onclick={() => reloads++}
					>{m['tasks.files_retry']()}</Button
				>{/if}
		</div>
	{:else if !entries.length}
		<p class="px-3 py-3 text-sm text-muted-foreground">{m['tasks.files_empty']()}</p>
	{:else}
		<Resizable.PaneGroup direction="vertical" autoSaveId="kuayle-task-files" class="min-h-0 flex-1">
			<Resizable.Pane defaultSize={45} minSize={15} class="flex min-h-0 flex-col">
				<section class="min-h-0 flex-1 overflow-y-auto p-2" aria-label={m['tasks.files_tree']()}>
					{#if listing.truncated}<p class="mb-2 px-1 text-xs text-[var(--status-warning)]" role="note">
							{m['tasks.files_truncated']({ count: count(entries.length) })}
						</p>{/if}
					{#if filtering && matched.count > matched.files.length}<p
							class="mb-2 px-1 text-xs text-muted-foreground"
							role="status"
						>
							{m['tasks.files_matches_capped']({ shown: count(matched.files.length), count: count(matched.count) })}
						</p>{/if}
					{#if !matched.files.length}
						<div class="flex flex-col items-start gap-2 px-1" role="status">
							<p class="text-sm text-muted-foreground">{m['tasks.files_no_matches']({ query: query.trim() })}</p>
							<Button variant="outline" size="sm" onclick={() => setQuery('')}>{m['tasks.files_clear_filter']()}</Button>
						</div>
					{:else}
						<div class="flex min-w-0 flex-col">
							{#each tree as node (node.path)}<FileTreeNode
									{node}
									{selected}
									select={(path) => (selected = path)}
									{open}
									{toggle}
								/>{/each}
						</div>
					{/if}
				</section>
			</Resizable.Pane>
			<Resizable.Handle withHandle aria-label={m['tasks.files_resize']()} />
			<Resizable.Pane defaultSize={55} minSize={20} class="flex min-h-0 flex-col">
				{#if selected}
					{#key `${listing.commit}:${selected}`}<FilePreview
							{slug}
							taskId={task.id}
							commit={listing.commit}
							path={selected}
							changed={!!current?.changed}
							bind:view
							{diff}
							reload={() => reloads++}
						/>{/key}
				{:else}
					<p class="px-3 py-3 text-sm text-muted-foreground">{m['tasks.files_pick']()}</p>
				{/if}
			</Resizable.Pane>
		</Resizable.PaneGroup>
	{/if}
</div>
