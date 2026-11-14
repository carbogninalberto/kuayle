<script lang="ts">
	import { untrack } from 'svelte';
	import { goto } from '$app/navigation';
	import { ChevronRight, Folder, FolderKanban, FolderPlus, MoreHorizontal, Pencil, Trash2 } from '@lucide/svelte';
	import { CONVERSATION_DRAG_TYPE, dropMove, isConversationDrag, type TaskIndicator } from './shell-rules';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import * as Collapsible from '$lib/components/ui/collapsible/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import * as AlertDialog from '$lib/components/ui/alert-dialog/index.js';
	import { conversations as api, type Conversation, type ConversationFolder } from '$lib/api/conversations';
	import { appToast } from '$lib/features/toast/toast';
	import type { Project } from '$lib/types/project';
	import { matchesProjectFilter, type ProjectFilter } from './project-filter';
	import { m } from '$lib/features/i18n/messages';

	// The user's own conversations, grouped into private folders and tagged
	// with projects; neither shares a conversation. Deleting a conversation is
	// refused by the API while a reply or agent task is active.
	let {
		slug,
		conversationId,
		history = $bindable([]),
		search = '',
		projects = [],
		projectFilter = 'all',
		loading = false,
		indicators = {},
		onrenamed
	}: {
		slug: string;
		conversationId: string | null;
		history: Conversation[];
		search?: string;
		projects?: Project[];
		projectFilter?: ProjectFilter;
		// No list has loaded yet: show that Recents is loading, not that it is empty.
		loading?: boolean;
		// A conversation's agent work at a glance: running, waiting on you, failed.
		indicators?: Record<string, TaskIndicator | undefined>;
		onrenamed?: (conversation: Conversation) => void;
	} = $props();
	// The Recents list can be folded away; the choice is remembered.
	const recentsKey = untrack(() => `assistant-recents-open:${slug}`);
	let recentsOpen = $state(
		(() => {
			try {
				return localStorage.getItem(recentsKey) !== 'closed';
			} catch {
				return true;
			}
		})()
	);
	function toggleRecents(open: boolean) {
		recentsOpen = open;
		try {
			localStorage.setItem(recentsKey, open ? 'open' : 'closed');
		} catch {
			/* Folding still works without storage. */
		}
	}
	// Drag and drop: a row carries its id under its own type; a folder (or the
	// list itself, for "no folder") accepts it. The row menu's "Move to" is the
	// keyboard equivalent.
	let dragging = $state<string | null>(null);
	let dropTarget = $state<string | null | undefined>(undefined);
	function dragStart(event: DragEvent, item: Conversation) {
		if (!event.dataTransfer) return;
		event.dataTransfer.setData(CONVERSATION_DRAG_TYPE, item.id);
		event.dataTransfer.effectAllowed = 'move';
		dragging = item.id;
	}
	function dragEnd() {
		dragging = null;
		dropTarget = undefined;
	}
	function dragOver(event: DragEvent, target: string | null) {
		if (!isConversationDrag(event.dataTransfer?.types)) return;
		event.preventDefault();
		event.stopPropagation();
		if (event.dataTransfer) event.dataTransfer.dropEffect = 'move';
		dropTarget = target;
	}
	function dragLeave(event: DragEvent, target: string | null) {
		const next = event.relatedTarget as Node | null;
		if (dropTarget === target && !(event.currentTarget as HTMLElement).contains(next)) dropTarget = undefined;
	}
	function drop(event: DragEvent, target: string | null) {
		if (!isConversationDrag(event.dataTransfer?.types)) return;
		event.preventDefault();
		event.stopPropagation();
		const id = event.dataTransfer?.getData(CONVERSATION_DRAG_TYPE) || dragging;
		dragEnd();
		const conversation = history.find((c) => c.id === id);
		const move = dropMove(conversation, target);
		if (!move || !conversation) return;
		if (target) toggled = { ...toggled, [target]: true };
		void moveTo(conversation, move.folder);
	}
	const indicatorDot: Record<TaskIndicator, string> = {
		running: 'bg-[var(--status-running)] animate-pulse motion-reduce:animate-none',
		waiting: 'bg-[var(--status-warning)]',
		failed: 'bg-[var(--status-error)]'
	};
	const indicatorLabel: Record<TaskIndicator, () => string> = {
		running: m['assistant.indicator_running'],
		waiting: m['assistant.indicator_waiting'],
		failed: m['assistant.indicator_failed']
	};
	// Under a single project's filter every row is that project's.
	const showProjects = $derived(projectFilter === 'all');

	let folders = $state<ConversationFolder[]>([]);
	// Folders start collapsed, except the one holding the open conversation;
	// a folder the person toggled keeps their choice.
	let toggled = $state<Record<string, boolean>>({});
	const isOpen = (folder: ConversationFolder) =>
		toggled[folder.id] ?? history.some((c) => c.id === conversationId && c.folder_id === folder.id);
	// The folder being named in place: a new one (no id) or a rename.
	let editing = $state<{ id?: string; name: string } | null>(null);
	let saving = false;
	// The conversation being renamed in place.
	let renaming = $state<{ id: string; title: string } | null>(null);
	let deleting = $state<Conversation | null>(null);
	let deletingFolder = $state<ConversationFolder | null>(null);

	$effect(() => {
		const target = slug;
		void api
			.folders(target)
			.then((list) => target === slug && (folders = list))
			.catch(() => (folders = []));
	});

	const visible = $derived(history.filter((c) => c.title.toLocaleLowerCase().includes(search.toLocaleLowerCase())));
	const ungrouped = $derived(visible.filter((c) => !c.folder_id || !folders.some((f) => f.id === c.folder_id)));
	const inFolder = (folder: ConversationFolder) => visible.filter((c) => c.folder_id === folder.id);

	async function saveFolder() {
		const current = editing;
		if (!current || saving) return;
		const name = current.name.trim();
		const original = current.id ? folders.find((f) => f.id === current.id)?.name : undefined;
		if (!name || name === original) {
			editing = null;
			return;
		}
		saving = true;
		try {
			if (current.id) {
				const updated = await api.renameFolder(slug, current.id, name);
				folders = folders.map((f) => (f.id === current.id ? updated : f));
			} else {
				folders = [...folders, await api.createFolder(slug, name)].sort((a, b) => a.name.localeCompare(b.name));
			}
			editing = null;
		} catch (error) {
			appToast.apiError(error, m['assistant.folder_save_failed']());
		} finally {
			saving = false;
		}
	}

	function folderKeys(event: KeyboardEvent) {
		if (event.key === 'Enter') {
			event.preventDefault();
			void saveFolder();
		} else if (event.key === 'Escape') {
			event.preventDefault();
			editing = null;
		}
	}

	const focus = (node: HTMLInputElement) => {
		node.focus();
		node.select();
	};

	async function saveTitle() {
		const current = renaming;
		if (!current || saving) return;
		const title = current.title.trim();
		if (!title || title === history.find((c) => c.id === current.id)?.title) {
			renaming = null;
			return;
		}
		saving = true;
		try {
			const updated = await api.rename(slug, current.id, title);
			history = history.map((c) => (c.id === current.id ? { ...c, title: updated.title } : c));
			onrenamed?.(updated);
			renaming = null;
		} catch (error) {
			appToast.apiError(error, m['assistant.rename_failed']());
		} finally {
			saving = false;
		}
	}

	function titleKeys(event: KeyboardEvent) {
		if (event.key === 'Enter') {
			event.preventDefault();
			void saveTitle();
		} else if (event.key === 'Escape') {
			event.preventDefault();
			renaming = null;
		}
	}

	const move = (conversation: Conversation, folder: string | null) => moveTo(conversation, folder);
	async function moveTo(conversation: Conversation, folder: string | null) {
		try {
			const updated = await api.move(slug, conversation.id, folder);
			history = history.map((c) => (c.id === conversation.id ? { ...c, folder_id: updated.folder_id ?? null } : c));
		} catch (error) {
			appToast.apiError(error, m['assistant.move_failed']());
		}
	}

	// An issue chat follows its issue's project; the API refuses to move it.
	async function moveToProject(conversation: Conversation, project: string | null) {
		try {
			const updated = await api.setProject(slug, conversation.id, project);
			const next = { ...conversation, project_id: updated.project_id ?? null, project: updated.project ?? null };
			history = matchesProjectFilter(next, projectFilter)
				? history.map((c) => (c.id === conversation.id ? next : c))
				: history.filter((c) => c.id !== conversation.id);
		} catch (error) {
			appToast.apiError(error, m['assistant.move_failed']());
		}
	}

	async function confirmDelete() {
		const target = deleting;
		if (!target) return;
		deleting = null;
		try {
			await api.remove(slug, target.id);
			history = history.filter((c) => c.id !== target.id);
			if (target.id === conversationId) void goto(`/${slug}/assistant`);
		} catch (error) {
			appToast.apiError(error, m['assistant.delete_failed']());
		}
	}

	async function confirmDeleteFolder() {
		const target = deletingFolder;
		if (!target) return;
		deletingFolder = null;
		try {
			await api.deleteFolder(slug, target.id);
			folders = folders.filter((f) => f.id !== target.id);
			history = history.map((c) => (c.folder_id === target.id ? { ...c, folder_id: null } : c));
		} catch (error) {
			appToast.apiError(error, m['assistant.delete_failed']());
		}
	}
</script>

{#snippet row(item: Conversation)}
	{#if renaming?.id === item.id}
		<div class="flex items-center px-1 py-0.5">
			<Input
				aria-label={m['assistant.conversation_name']()}
				maxlength={200}
				class="h-8 min-w-0 flex-1 text-sm"
				bind:value={renaming.title}
				onkeydown={titleKeys}
				onblur={() => void saveTitle()}
				{@attach focus}
			/>
		</div>
	{:else}
		{@const indicator = indicators[item.id]}
		<!-- Dragging is a pointer shortcut; the row menu's "Move to" does the same from the keyboard. -->
		<!-- svelte-ignore a11y_no_static_element_interactions -->
		<div
			class="group flex h-8 min-w-0 items-center rounded-md hover:bg-muted {item.id === conversationId
				? 'bg-muted'
				: ''} {dragging === item.id ? 'opacity-50' : ''}"
			draggable="true"
			ondragstart={(event) => dragStart(event, item)}
			ondragend={dragEnd}
			data-conversation={item.id}
		>
			<a
				href="/{slug}/assistant?conversation={encodeURIComponent(item.id)}"
				aria-current={item.id === conversationId ? 'page' : undefined}
				draggable="false"
				class="min-w-0 flex-1 truncate px-2 py-1.5 text-sm {item.id === conversationId
					? 'font-medium text-foreground'
					: 'text-muted-foreground'}">{item.title}</a
			>
			{#if indicator}<span
					role="img"
					aria-label={indicatorLabel[indicator]()}
					title={indicatorLabel[indicator]()}
					class="mr-1 size-1.5 shrink-0 rounded-full {indicatorDot[indicator]}"
				></span>{/if}
			{#if showProjects && item.project}
				<Badge variant="secondary" class="max-w-24 shrink font-normal" title={item.project.name}
					><span class="truncate">{item.project.name}</span></Badge
				>
			{/if}
			<DropdownMenu.Root>
				<DropdownMenu.Trigger>
					{#snippet child({ props })}
						<Button
							{...props}
							variant="ghost"
							size="icon-sm"
							class="mr-1 shrink-0 opacity-0 group-hover:opacity-100 focus-visible:opacity-100 data-[state=open]:opacity-100"
							aria-label={m['assistant.conversation_actions']({ title: item.title })}><MoreHorizontal /></Button
						>
					{/snippet}
				</DropdownMenu.Trigger>
				<DropdownMenu.Content align="end" class="w-48">
					<DropdownMenu.Item onSelect={() => (renaming = { id: item.id, title: item.title })}
						><Pencil />{m['assistant.rename_conversation']()}</DropdownMenu.Item
					>
					<DropdownMenu.Sub>
						<DropdownMenu.SubTrigger><Folder />{m['assistant.move_to']()}</DropdownMenu.SubTrigger>
						<DropdownMenu.SubContent>
							<DropdownMenu.Item disabled={!item.folder_id} onSelect={() => void move(item, null)}
								>{m['assistant.no_folder']()}</DropdownMenu.Item
							>
							{#each folders as folder (folder.id)}
								<DropdownMenu.Item disabled={item.folder_id === folder.id} onSelect={() => void move(item, folder.id)}
									>{folder.name}</DropdownMenu.Item
								>
							{/each}
						</DropdownMenu.SubContent>
					</DropdownMenu.Sub>
					<DropdownMenu.Sub>
						<DropdownMenu.SubTrigger><FolderKanban />{m['assistant.move_to_project']()}</DropdownMenu.SubTrigger>
						<DropdownMenu.SubContent class="max-h-72 overflow-y-auto">
							{#if item.issue_id}
								<DropdownMenu.Label class="max-w-56 text-xs font-normal text-muted-foreground"
									>{m['assistant.issue_chat_project']()}</DropdownMenu.Label
								>
							{/if}
							<DropdownMenu.Item
								disabled={!!item.issue_id || !item.project}
								onSelect={() => void moveToProject(item, null)}>{m['assistant.no_project']()}</DropdownMenu.Item
							>
							{#each projects as project (project.id)}
								<DropdownMenu.Item
									disabled={!!item.issue_id || item.project?.id === project.id}
									onSelect={() => void moveToProject(item, project.id)}>{project.name}</DropdownMenu.Item
								>
							{/each}
						</DropdownMenu.SubContent>
					</DropdownMenu.Sub>
					<DropdownMenu.Separator />
					<DropdownMenu.Item variant="destructive" onSelect={() => (deleting = item)}
						><Trash2 />{m['assistant.delete_conversation']()}</DropdownMenu.Item
					>
				</DropdownMenu.Content>
			</DropdownMenu.Root>
		</div>
	{/if}
{/snippet}


{#snippet nameField()}
	{#if editing}
		<Input
			aria-label={m['assistant.folder_name']()}
			maxlength={80}
			class="h-7 min-w-0 flex-1 text-sm"
			bind:value={editing.name}
			onkeydown={folderKeys}
			onblur={() => void saveFolder()}
			{@attach focus}
		/>
	{/if}
{/snippet}

<Collapsible.Root open={recentsOpen} onOpenChange={toggleRecents} class="min-w-0">
<div class="flex items-center gap-1 pr-1">
	<Collapsible.Trigger>
		{#snippet child({ props })}
			<Button
				{...props}
				variant="plain"
				class="flex h-7 min-w-0 flex-1 items-center gap-1 rounded-md px-2 text-left text-[11px] font-medium tracking-wide text-muted-foreground uppercase hover:text-foreground"
				><ChevronRight
					class="size-3 shrink-0 transition-transform {recentsOpen ? 'rotate-90' : ''}"
					aria-hidden="true"
				/>{m['assistant.recents']()}</Button
			>
		{/snippet}
	</Collapsible.Trigger>
	<Button
		variant="ghost"
		size="icon-sm"
		class="size-6 text-muted-foreground"
		aria-label={m['assistant.new_folder']()}
		title={m['assistant.new_folder']()}
		onclick={() => {
			toggleRecents(true);
			editing = { name: '' };
		}}><FolderPlus class="size-3.5" /></Button
	>
</div>
<Collapsible.Content>
<nav
	class="flex min-h-12 flex-col gap-0.5 rounded-md pb-2 {dropTarget === null ? 'bg-muted/40 ring-1 ring-ring/40' : ''}"
	aria-label={m['assistant.history']()}
	ondragover={(event) => dragOver(event, null)}
	ondragleave={(event) => dragLeave(event, null)}
	ondrop={(event) => drop(event, null)}
>
	{#if editing && !editing.id}
		<div class="flex items-center gap-1.5 px-2 py-0.5">
			<Folder class="size-3.5 shrink-0 text-muted-foreground" />{@render nameField()}
		</div>
	{/if}
	{#each folders as folder (folder.id)}
		{@const items = inFolder(folder)}
		<Collapsible.Root open={isOpen(folder)} onOpenChange={(open) => (toggled = { ...toggled, [folder.id]: open })}>
			{#if editing?.id === folder.id}
				<div class="flex items-center gap-1.5 px-2 py-0.5">
					<Folder class="size-3.5 shrink-0 text-muted-foreground" />{@render nameField()}
				</div>
			{:else}
				<div
					class="group flex items-center rounded-md hover:bg-muted {dropTarget === folder.id
						? 'bg-muted ring-1 ring-ring/60'
						: ''}"
					role="group"
					aria-label={m['assistant.folder_drop']({ name: folder.name })}
					data-folder={folder.id}
					ondragover={(event) => dragOver(event, folder.id)}
					ondragleave={(event) => dragLeave(event, folder.id)}
					ondrop={(event) => drop(event, folder.id)}
				>
					<Collapsible.Trigger class="flex min-w-0 flex-1 items-center gap-1.5 px-2 py-1.5 text-left text-sm">
						<ChevronRight class="size-3.5 shrink-0 transition-transform {isOpen(folder) ? 'rotate-90' : ''}" />
						<Folder class="size-3.5 shrink-0 text-muted-foreground" />
						<span class="truncate font-medium">{folder.name}</span>
						<span class="ml-auto text-xs text-muted-foreground">{items.length}</span>
					</Collapsible.Trigger>
					<DropdownMenu.Root>
						<DropdownMenu.Trigger>
							{#snippet child({ props })}
								<Button
									{...props}
									variant="ghost"
									size="icon-sm"
									class="mr-1 shrink-0 opacity-0 group-hover:opacity-100 focus-visible:opacity-100 data-[state=open]:opacity-100"
									aria-label={m['assistant.folder_actions']({ name: folder.name })}><MoreHorizontal /></Button
								>
							{/snippet}
						</DropdownMenu.Trigger>
						<DropdownMenu.Content align="end">
							<DropdownMenu.Item onSelect={() => (editing = { id: folder.id, name: folder.name })}
								><Pencil />{m['assistant.rename_folder']()}</DropdownMenu.Item
							>
							<DropdownMenu.Item variant="destructive" onSelect={() => (deletingFolder = folder)}
								><Trash2 />{m['assistant.delete_folder']()}</DropdownMenu.Item
							>
						</DropdownMenu.Content>
					</DropdownMenu.Root>
				</div>
			{/if}
			<Collapsible.Content
				class="ml-3 flex flex-col gap-0.5 border-l pl-1"
				ondragover={(event: DragEvent) => dragOver(event, folder.id)}
				ondrop={(event: DragEvent) => drop(event, folder.id)}
			>
				{#each items as item (item.id)}{@render row(item)}{:else}
					<p class="px-2 py-1 text-xs text-muted-foreground">{m['assistant.folder_empty']()}</p>
				{/each}
			</Collapsible.Content>
		</Collapsible.Root>
	{/each}
	{#each ungrouped as item (item.id)}{@render row(item)}{/each}
	{#if loading}
		<p role="status" class="px-2 py-1 text-xs text-muted-foreground">{m['assistant.recents_loading']()}</p>
	{:else if !visible.length}
		<p class="px-2 py-1 text-xs text-muted-foreground">{m['assistant.recents_empty']()}</p>
	{/if}
</nav>
</Collapsible.Content>
</Collapsible.Root>

<AlertDialog.Root open={!!deleting} onOpenChange={(open) => !open && (deleting = null)}>
	<AlertDialog.Content>
		<AlertDialog.Header>
			<AlertDialog.Title>{m['assistant.delete_conversation_title']()}</AlertDialog.Title>
			<AlertDialog.Description
				>{m['assistant.delete_conversation_description']({ title: deleting?.title ?? '' })}</AlertDialog.Description
			>
		</AlertDialog.Header>
		<AlertDialog.Footer>
			<AlertDialog.Cancel>{m['common.cancel']()}</AlertDialog.Cancel>
			<AlertDialog.Action onclick={() => void confirmDelete()}
				>{m['assistant.delete_conversation']()}</AlertDialog.Action
			>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>

<AlertDialog.Root open={!!deletingFolder} onOpenChange={(open) => !open && (deletingFolder = null)}>
	<AlertDialog.Content>
		<AlertDialog.Header>
			<AlertDialog.Title>{m['assistant.delete_folder']()}</AlertDialog.Title>
			<AlertDialog.Description
				>{m['assistant.delete_folder_description']({ name: deletingFolder?.name ?? '' })}</AlertDialog.Description
			>
		</AlertDialog.Header>
		<AlertDialog.Footer>
			<AlertDialog.Cancel>{m['common.cancel']()}</AlertDialog.Cancel>
			<AlertDialog.Action onclick={() => void confirmDeleteFolder()}
				>{m['assistant.delete_folder']()}</AlertDialog.Action
			>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>
