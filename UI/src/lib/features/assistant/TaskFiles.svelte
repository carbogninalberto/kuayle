<script lang="ts">
	import { untrack } from 'svelte';
	import { RefreshCw, Search } from '@lucide/svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import * as Resizable from '$lib/components/ui/resizable/index.js';
	import { agentTasks, type AgentTask, type TaskFiles } from '$lib/api/agent-tasks';
	import FileTreeNode from './FileTreeNode.svelte';
	import FilePreview from './FilePreview.svelte';
	import { fileTree } from './agent-trace';
	import { parseDiff, type DiffFile } from './diff';
	import { fileError, findEntry, folderOpen, treeNodes, type DirState, type FileError } from './task-files';
	import { getLocale, m } from '$lib/features/i18n/messages';

	// A task's files at its head commit (KUA-285, KUA-319): a tree of what the
	// commit tracks, read a folder at a time (the root when the panel opens, a
	// folder when it is expanded), the files the task changed marked, a path
	// filter the server answers, and a preview of the chosen file below it. Only
	// the task's creator may read them; the server decides and the panel says so.
	let { slug, task }: { slug: string; task: AgentTask } = $props();

	// The directories read so far by path ('' is the root), replaced whole on every
	// read: raw, so a large folder's entries are not proxied one by one.
	let dirs = $state.raw<Record<string, DirState>>({});
	// The commit the root was read at; every folder and file is read at it too.
	let commit = $state('');
	let status = $state<'loading' | 'ready' | FileError>('loading');
	let reloads = $state(0);
	let query = $state('');
	// The server's answer to the filter, for the text being filtered.
	let found = $state.raw<{ status: 'loading' | 'ready' | FileError; result?: TaskFiles } | null>(null);
	// Folders the reader opened or closed, and those toggled during the current filter.
	let toggled = $state<Record<string, boolean>>({});
	let filterToggled = $state<Record<string, boolean>>({});
	let selected = $state<string | null>(null);
	let view = $state<'file' | 'diff'>('file');
	let stored: Promise<{ files: DiffFile[]; truncated: boolean }> | null = null;
	// Reads of an earlier root, task or filter are dropped when they answer.
	let generation = 0;
	let loadedTask = '';

	const root = $derived(dirs['']?.status === 'ready' ? dirs[''] : null);
	const filtering = $derived(query.trim() !== '');
	const matches = $derived(found?.result?.entries ?? []);
	const tree = $derived(filtering ? fileTree(matches, matches.length) : treeNodes(dirs));
	const current = $derived(
		selected ? (matches.find((entry) => entry.path === selected) ?? findEntry(dirs, selected)) : undefined
	);
	// The task object is replaced on every update; only another task reloads.
	const taskId = $derived(task.id);

	// Reads one folder at the root's commit; ready folders are not read again.
	function loadDir(path: string, at = commit) {
		const run = generation;
		dirs = { ...dirs, [path]: { status: 'loading' } };
		return agentTasks
			.files(slug, taskId, { ref: at, path })
			.then((result) => {
				if (run === generation)
					dirs = { ...dirs, [path]: { status: 'ready', entries: result.entries, truncated: result.truncated } };
			})
			.catch((error) => {
				if (run === generation) dirs = { ...dirs, [path]: { status: 'error', error: fileError(error) } };
			});
	}

	$effect(() => {
		const id = taskId;
		void reloads;
		const run = ++generation;
		const again = id === loadedTask;
		loadedTask = id;
		status = 'loading';
		stored = null;
		dirs = {};
		// Another task starts closed; a reload opens again what the reader had open.
		const open = untrack(() => (again ? Object.keys(toggled).filter((path) => toggled[path]) : []));
		if (!again) toggled = {};
		agentTasks
			.files(slug, id)
			.then((result) => {
				if (run !== generation) return;
				commit = result.commit;
				dirs = { '': { status: 'ready', entries: result.entries, truncated: result.truncated } };
				status = 'ready';
				for (const path of open) void loadDir(path, result.commit);
			})
			.catch((error) => {
				if (run === generation) status = fileError(error);
			});
		return () => generation++;
	});

	// The filter is answered by the server, over the whole commit, once typing pauses.
	$effect(() => {
		const text = query.trim();
		const at = commit;
		const id = taskId;
		if (!text || !at) {
			found = null;
			return;
		}
		found = { status: 'loading' };
		let stale = false;
		const timer = setTimeout(() => {
			agentTasks
				.files(slug, id, { ref: at, query: text })
				.then((result) => {
					if (!stale) found = { status: 'ready', result };
				})
				.catch((error) => {
					if (!stale) found = { status: fileError(error) };
				});
		}, 250);
		return () => {
			stale = true;
			clearTimeout(timer);
		};
	});

	function setQuery(value: string) {
		query = value;
		filterToggled = {};
	}

	const open = (path: string) => folderOpen(path, filtering ? filterToggled : toggled, filtering);
	function toggle(path: string, value: boolean) {
		if (filtering) {
			filterToggled = { ...filterToggled, [path]: value };
			return;
		}
		toggled = { ...toggled, [path]: value };
		if (value && dirs[path]?.status !== 'ready' && dirs[path]?.status !== 'loading') void loadDir(path);
	}
	const retry = (path: string) => void loadDir(path);

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
				{#if status === 'ready' && commit}<span title={commit}
						>{m['tasks.files_at_commit']({ commit: commit.slice(0, 7) })}</span
					>{/if}
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
		{#if status === 'ready' && root?.entries.length}
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
	{:else if status !== 'ready' || !root}
		<div class="flex flex-col items-start gap-2 px-3 py-3" role="alert" data-testid="files-error">
			<p class="text-sm text-muted-foreground">{message[status === 'ready' ? 'error' : status]()}</p>
			{#if status !== 'denied'}<Button variant="outline" size="sm" onclick={() => reloads++}
					>{m['tasks.files_retry']()}</Button
				>{/if}
		</div>
	{:else if !root.entries.length}
		<p class="px-3 py-3 text-sm text-muted-foreground">{m['tasks.files_empty']()}</p>
	{:else}
		<Resizable.PaneGroup direction="vertical" autoSaveId="kuayle-task-files" class="min-h-0 flex-1">
			<Resizable.Pane defaultSize={45} minSize={15} class="flex min-h-0 flex-col">
				<section class="min-h-0 flex-1 overflow-y-auto p-2" aria-label={m['tasks.files_tree']()}>
					{#if !filtering && root.truncated}<p class="mb-2 px-1 text-xs text-[var(--status-warning)]" role="note">
							{m['tasks.files_truncated']({ count: count(root.entries.length) })}
						</p>{/if}
					{#if filtering && found?.status === 'ready' && found.result && (found.result.matches ?? 0) > matches.length}<p
							class="mb-2 px-1 text-xs text-muted-foreground"
							role="status"
						>
							{m['tasks.files_matches_capped']({
								shown: count(matches.length),
								count: count(found.result.matches ?? 0)
							})}
						</p>{/if}
					{#if filtering && (!found || found.status === 'loading')}
						<p class="px-1 text-sm text-muted-foreground" role="status">{m['assistant.loading']()}</p>
					{:else if filtering && found && found.status !== 'ready' && found.status !== 'loading'}
						<div class="flex flex-col items-start gap-2 px-1" role="alert">
							<p class="text-sm text-muted-foreground">{message[found.status]()}</p>
							<Button variant="outline" size="sm" onclick={() => setQuery('')}>{m['tasks.files_clear_filter']()}</Button
							>
						</div>
					{:else if filtering && !matches.length}
						<div class="flex flex-col items-start gap-2 px-1" role="status">
							<p class="text-sm text-muted-foreground">{m['tasks.files_no_matches']({ query: query.trim() })}</p>
							<Button variant="outline" size="sm" onclick={() => setQuery('')}>{m['tasks.files_clear_filter']()}</Button
							>
						</div>
					{:else}
						<div class="flex min-w-0 flex-col">
							{#each tree as node (node.path)}<FileTreeNode
									{node}
									{selected}
									select={(path) => (selected = path)}
									{open}
									{toggle}
									{retry}
								/>{/each}
						</div>
					{/if}
				</section>
			</Resizable.Pane>
			<Resizable.Handle withHandle aria-label={m['tasks.files_resize']()} />
			<Resizable.Pane defaultSize={55} minSize={20} class="flex min-h-0 flex-col">
				{#if selected}
					{#key `${commit}:${selected}`}<FilePreview
							{slug}
							taskId={task.id}
							{commit}
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
