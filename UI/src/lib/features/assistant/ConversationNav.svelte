<script lang="ts">
	import { goto } from '$app/navigation';
	import { ChevronRight, Folder, FolderPlus, MoreHorizontal, Pencil, Trash2 } from '@lucide/svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import * as Collapsible from '$lib/components/ui/collapsible/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import * as AlertDialog from '$lib/components/ui/alert-dialog/index.js';
	import { conversations as api, type Conversation, type ConversationFolder } from '$lib/api/conversations';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/paraglide/messages.js';

	// The user's own conversations, grouped into private folders. Deleting a
	// conversation is refused by the API while a reply or agent task is active.
	let {
		slug,
		conversationId,
		history = $bindable([]),
		search = '',
		onrenamed
	}: {
		slug: string;
		conversationId: string | null;
		history: Conversation[];
		search?: string;
		onrenamed?: (conversation: Conversation) => void;
	} = $props();

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

	async function move(conversation: Conversation, folder: string | null) {
		try {
			const updated = await api.move(slug, conversation.id, folder);
			history = history.map((c) => (c.id === conversation.id ? { ...c, folder_id: updated.folder_id ?? null } : c));
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
		<div
			class="group flex min-w-0 items-center rounded-md hover:bg-muted {item.id === conversationId ? 'bg-muted' : ''}"
		>
			<a
				href="/{slug}/assistant?conversation={encodeURIComponent(item.id)}"
				aria-current={item.id === conversationId ? 'page' : undefined}
				class="min-w-0 flex-1 truncate px-2 py-2 text-sm {item.id === conversationId
					? 'font-medium'
					: 'text-muted-foreground'}">{item.title}</a
			>
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
					<DropdownMenu.Separator />
					<DropdownMenu.Item variant="destructive" onSelect={() => (deleting = item)}
						><Trash2 />{m['assistant.delete_conversation']()}</DropdownMenu.Item
					>
				</DropdownMenu.Content>
			</DropdownMenu.Root>
		</div>
	{/if}
{/snippet}

<Button
	variant="ghost"
	size="sm"
	class="h-7 justify-start self-start px-2 text-xs"
	onclick={() => (editing = { name: '' })}><FolderPlus />{m['assistant.new_folder']()}</Button
>
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

<nav class="flex flex-col gap-1" aria-label={m['assistant.history']()}>
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
				<div class="group flex items-center rounded-md hover:bg-muted">
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
			<Collapsible.Content class="ml-3 flex flex-col gap-1 border-l pl-1">
				{#each items as item (item.id)}{@render row(item)}{:else}
					<p class="px-2 py-1 text-xs text-muted-foreground">{m['assistant.folder_empty']()}</p>
				{/each}
			</Collapsible.Content>
		</Collapsible.Root>
	{/each}
	{#each ungrouped as item (item.id)}{@render row(item)}{/each}
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
