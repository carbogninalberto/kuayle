<script lang="ts">
	import { onMount, untrack } from 'svelte';
	import { goto } from '$app/navigation';
	import { ChevronRight, Folder, FolderPlus, Pencil, Trash2 } from '@lucide/svelte';
	import { CONVERSATION_DRAG_TYPE, dropMove, isConversationDrag, type TaskIndicator } from './shell-rules';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import * as Collapsible from '$lib/components/ui/collapsible/index.js';
	import * as ContextMenu from '$lib/components/ui/context-menu/index.js';
	import * as AlertDialog from '$lib/components/ui/alert-dialog/index.js';
	import { conversations as api, type Conversation, type ConversationFolder } from '$lib/api/conversations';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/features/i18n/messages';

	let {
		slug,
		conversationId,
		history,
		onchange,
		folder: selectedFolder = $bindable(null),
		hasMore,
		failed,
		onmore,
		loading = false,
		indicators = {},
		onrenamed
	}: {
		slug: string;
		conversationId: string | null;
		history: Conversation[];
		onchange: (update: (items: Conversation[]) => Conversation[]) => void;
		folder: string | null;
		hasMore: boolean;
		failed: boolean;
		onmore: () => void;
		loading?: boolean;
		// A conversation's agent work at a glance: running, waiting on you, failed.
		indicators?: Record<string, TaskIndicator | undefined>;
		onrenamed?: (conversation: Conversation) => void;
	} = $props();
	// The folder list can be folded away; the choice is remembered.
	const foldersKey = untrack(() => `assistant-folders-open:${slug}`);
	let foldersOpen = $state(
		(() => {
			try {
				return localStorage.getItem(foldersKey) !== 'closed';
			} catch {
				return true;
			}
		})()
	);
	function toggleFolders(open: boolean) {
		foldersOpen = open;
		if (!open) selectedFolder = null;
		try {
			localStorage.setItem(foldersKey, open ? 'open' : 'closed');
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
	let folders = $state<ConversationFolder[]>([]);
	// The folder being named in place: a new one (no id) or a rename.
	let editing = $state<{ id?: string; name: string } | null>(null);
	let saving = false;
	// The conversation being renamed in place.
	let renaming = $state<{ id: string; title: string } | null>(null);
	let deleting = $state<Conversation | null>(null);
	let deletingFolder = $state<ConversationFolder | null>(null);

	onMount(() => {
		void api.folders(slug)
			.then((list) => (folders = list))
			.catch((error) => appToast.apiError(error, m['assistant.error']()));
	});

	function openMenuFromKeyboard(event: KeyboardEvent) {
		if (event.key !== 'ContextMenu' && !(event.shiftKey && event.key === 'F10')) return;
		event.preventDefault();
		const trigger = event.currentTarget as HTMLElement;
		const { left, bottom } = trigger.getBoundingClientRect();
		trigger.dispatchEvent(new MouseEvent('contextmenu', {
			bubbles: true, cancelable: true, clientX: left, clientY: bottom
		}));
	}

	// A touch that opened the menu must not also activate its link or folder.
	function finishTouch(event: TouchEvent) {
		if ((event.currentTarget as HTMLElement).dataset.state === 'open') event.preventDefault();
	}

	function adjustCount(id: string | null | undefined, delta: number) {
		if (id) folders = folders.map((f) => f.id === id ? { ...f, count: f.count + delta } : f);
	}

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
				folders = folders.map((f) => (f.id === current.id ? { ...f, name: updated.name } : f));
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
			onchange((items) => items.map((c) => (c.id === current.id ? { ...c, title: updated.title } : c)));
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

	async function moveTo(conversation: Conversation, folder: string | null) {
		try {
			const updated = await api.move(slug, conversation.id, folder);
			adjustCount(conversation.folder_id, -1);
			adjustCount(updated.folder_id, 1);
			onchange((items) => items.map((c) => (c.id === conversation.id ? { ...c, folder_id: updated.folder_id } : c)));
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
			adjustCount(target.folder_id, -1);
			onchange((items) => items.filter((c) => c.id !== target.id));
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
			if (selectedFolder === target.id) selectedFolder = null;
			folders = folders.filter((f) => f.id !== target.id);
			onchange((items) => items.map((c) => (c.folder_id === target.id ? { ...c, folder_id: null } : c)));
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
		<ContextMenu.Root>
			<ContextMenu.Trigger>
				{#snippet child({ props })}
					<a
						{...props}
						href="/{slug}/assistant?conversation={encodeURIComponent(item.id)}"
						aria-current={item.id === conversationId ? 'page' : undefined}
						aria-haspopup="menu"
						aria-keyshortcuts="Shift+F10"
						tabindex="0"
						onkeydown={openMenuFromKeyboard}
						ontouchend={finishTouch}
						draggable="true"
						ondragstart={(event) => dragStart(event, item)}
						ondragend={dragEnd}
						data-conversation={item.id}
						class="flex h-8 min-w-0 items-center gap-1.5 rounded-md px-2 text-sm hover:bg-muted focus-visible:outline-ring {item.id === conversationId
							? 'bg-muted font-medium text-foreground' : 'text-muted-foreground'} {dragging === item.id ? 'opacity-50' : ''}"
					>
						<span class="min-w-0 flex-1 truncate">{item.title}</span>
						{#if indicator}
							<span role="img" aria-label={indicatorLabel[indicator]()} title={indicatorLabel[indicator]()}
								class="size-1.5 shrink-0 rounded-full {indicatorDot[indicator]}"></span>
						{/if}
						{#if item.project}
							<Badge variant="secondary" class="max-w-24 shrink font-normal" title={item.project.name}>
								<span class="truncate">{item.project.name}</span>
							</Badge>
						{/if}
					</a>
				{/snippet}
			</ContextMenu.Trigger>
			<ContextMenu.Content class="w-48">
				<ContextMenu.Item onSelect={() => (renaming = { id: item.id, title: item.title })}
					><Pencil />{m['assistant.rename_conversation']()}</ContextMenu.Item
				>
				<ContextMenu.Sub>
					<ContextMenu.SubTrigger><Folder />{m['assistant.move_to']()}</ContextMenu.SubTrigger>
					<ContextMenu.SubContent>
						<ContextMenu.Item disabled={!item.folder_id} onSelect={() => void moveTo(item, null)}
							>{m['assistant.no_folder']()}</ContextMenu.Item
						>
						{#each folders as folder (folder.id)}
							<ContextMenu.Item disabled={item.folder_id === folder.id} onSelect={() => void moveTo(item, folder.id)}
								>{folder.name}</ContextMenu.Item
							>
						{/each}
					</ContextMenu.SubContent>
				</ContextMenu.Sub>
				<ContextMenu.Separator />
				<ContextMenu.Item variant="destructive" onSelect={() => (deleting = item)}
					><Trash2 />{m['assistant.delete_conversation']()}</ContextMenu.Item
				>
			</ContextMenu.Content>
		</ContextMenu.Root>
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

<nav aria-label={m['assistant.history']()} class="min-w-0 [-webkit-touch-callout:none]">
	<Collapsible.Root open={foldersOpen} onOpenChange={toggleFolders} class="min-w-0">
		<div class="flex items-center gap-1 pr-1">
			<Collapsible.Trigger>
				{#snippet child({ props })}
					<Button
						{...props}
						variant="plain"
						class="flex h-7 min-w-0 flex-1 items-center gap-1 rounded-md px-2 text-left text-[11px] font-medium tracking-wide text-muted-foreground uppercase hover:text-foreground"
						><ChevronRight
							class="size-3 shrink-0 transition-transform {foldersOpen ? 'rotate-90' : ''}"
							aria-hidden="true"
						/>{m['assistant.folders']()}</Button
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
					toggleFolders(true);
					editing = { name: '' };
				}}><FolderPlus class="size-3.5" /></Button
			>
		</div>
		<Collapsible.Content>
			{#if editing && !editing.id}
				<div class="flex items-center gap-1.5 px-2 py-0.5">
					<Folder class="size-3.5 shrink-0 text-muted-foreground" />{@render nameField()}
				</div>
			{/if}
			{#each folders as folder (folder.id)}
				{#if editing?.id === folder.id}
					<div class="flex items-center gap-1.5 px-2 py-0.5">
						<Folder class="size-3.5 shrink-0 text-muted-foreground" />{@render nameField()}
					</div>
				{:else}
					<ContextMenu.Root>
						<ContextMenu.Trigger>
							{#snippet child({ props })}
								<Button
									{...props}
									variant="plain"
									tabindex={0}
									aria-pressed={selectedFolder === folder.id}
									aria-haspopup="menu"
									aria-keyshortcuts="Shift+F10"
									onkeydown={openMenuFromKeyboard}
									ontouchend={finishTouch}
									onclick={() => (selectedFolder = selectedFolder === folder.id ? null : folder.id)}
									data-folder={folder.id}
									ondragover={(event) => dragOver(event, folder.id)}
									ondragleave={(event) => dragLeave(event, folder.id)}
									ondrop={(event) => drop(event, folder.id)}
									class="flex h-8 w-full items-center gap-1.5 rounded-md px-2 text-left text-sm hover:bg-muted aria-pressed:bg-muted {dropTarget === folder.id ? 'ring-1 ring-ring' : ''}"
								>
									<Folder class="size-3.5 shrink-0 text-muted-foreground" />
									<span class="min-w-0 flex-1 truncate font-medium">{folder.name}</span>
									<span class="text-xs text-muted-foreground">{folder.count}</span>
								</Button>
							{/snippet}
						</ContextMenu.Trigger>
						<ContextMenu.Content>
							<ContextMenu.Item onSelect={() => (editing = { id: folder.id, name: folder.name })}>
								<Pencil />{m['assistant.rename_conversation']()}
							</ContextMenu.Item>
							<ContextMenu.Item variant="destructive" onSelect={() => (deletingFolder = folder)}>
								<Trash2 />{m['common.delete']()}
							</ContextMenu.Item>
						</ContextMenu.Content>
					</ContextMenu.Root>
				{/if}
			{/each}
		</Collapsible.Content>
	</Collapsible.Root>

	<!-- Dropping on the list moves a conversation out of its folder. -->
	<!-- svelte-ignore a11y_no_static_element_interactions -->
	<div
		class="mt-2 flex min-h-12 flex-col gap-0.5 rounded-md pb-2 {dropTarget === null ? 'bg-muted/40 ring-1 ring-ring/40' : ''}"
		ondragover={(event) => dragOver(event, null)}
		ondragleave={(event) => dragLeave(event, null)}
		ondrop={(event) => drop(event, null)}
	>
		{#each history as item (item.id)}{@render row(item)}{/each}
		{#if loading}
			<p role="status" class="px-2 py-1 text-xs text-muted-foreground">{m['assistant.recents_loading']()}</p>
		{:else if failed || hasMore}
			<Button variant="ghost" size="sm" onclick={onmore}>
				{failed ? m['assistant.retry']() : m['agent_messages.load_more']()}
			</Button>
		{:else if !history.length}
			<p class="px-2 py-1 text-xs text-muted-foreground">
				{selectedFolder ? m['assistant.folder_empty']() : m['assistant.recents_empty']()}
			</p>
		{/if}
	</div>
</nav>

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
