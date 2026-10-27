<script lang="ts">
	import { goto } from '$app/navigation';
	import { ChevronRight, Folder, FolderPlus, MoreHorizontal, Pencil, Trash2 } from '@lucide/svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import * as Collapsible from '$lib/components/ui/collapsible/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import * as AlertDialog from '$lib/components/ui/alert-dialog/index.js';
	import * as Dialog from '$lib/components/ui/dialog/index.js';
	import { conversations as api, type Conversation, type ConversationFolder } from '$lib/api/conversations';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/paraglide/messages.js';

	// The user's own conversations, grouped into private folders. Deleting a
	// conversation is refused by the API while a reply or agent task is active.
	let {
		slug,
		conversationId,
		history = $bindable([]),
		search = ''
	}: { slug: string; conversationId: string | null; history: Conversation[]; search?: string } = $props();

	let folders = $state<ConversationFolder[]>([]);
	let closed = $state<Record<string, boolean>>({});
	let folderDialog = $state<{ id?: string; name: string } | null>(null);
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
		if (!folderDialog) return;
		const { id, name } = folderDialog;
		try {
			if (id) {
				const updated = await api.renameFolder(slug, id, name);
				folders = folders.map((f) => (f.id === id ? updated : f));
			} else {
				folders = [...folders, await api.createFolder(slug, name)].sort((a, b) => a.name.localeCompare(b.name));
			}
			folderDialog = null;
		} catch (error) {
			appToast.apiError(error, m['assistant.folder_save_failed']());
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
	<div class="group flex min-w-0 items-center rounded-md hover:bg-muted {item.id === conversationId ? 'bg-muted' : ''}">
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
{/snippet}

<div class="flex items-center justify-between px-2">
	<span class="sr-only">{m['assistant.history']()}</span>
	<Button variant="ghost" size="sm" class="ml-auto h-7 text-xs" onclick={() => (folderDialog = { name: '' })}
		><FolderPlus />{m['assistant.new_folder']()}</Button
	>
</div>
<nav class="flex flex-col gap-1" aria-label={m['assistant.history']()}>
	{#each folders as folder (folder.id)}
		{@const items = inFolder(folder)}
		<Collapsible.Root open={!closed[folder.id]} onOpenChange={(open) => (closed = { ...closed, [folder.id]: !open })}>
			<div class="group flex items-center rounded-md hover:bg-muted">
				<Collapsible.Trigger class="flex min-w-0 flex-1 items-center gap-1.5 px-2 py-1.5 text-left text-sm">
					<ChevronRight class="size-3.5 shrink-0 transition-transform {closed[folder.id] ? '' : 'rotate-90'}" />
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
						<DropdownMenu.Item onSelect={() => (folderDialog = { id: folder.id, name: folder.name })}
							><Pencil />{m['assistant.rename_folder']()}</DropdownMenu.Item
						>
						<DropdownMenu.Item variant="destructive" onSelect={() => (deletingFolder = folder)}
							><Trash2 />{m['assistant.delete_folder']()}</DropdownMenu.Item
						>
					</DropdownMenu.Content>
				</DropdownMenu.Root>
			</div>
			<Collapsible.Content class="ml-3 flex flex-col gap-1 border-l pl-1">
				{#each items as item (item.id)}{@render row(item)}{:else}
					<p class="px-2 py-1 text-xs text-muted-foreground">{m['assistant.folder_empty']()}</p>
				{/each}
			</Collapsible.Content>
		</Collapsible.Root>
	{/each}
	{#each ungrouped as item (item.id)}{@render row(item)}{/each}
</nav>

<Dialog.Root open={!!folderDialog} onOpenChange={(open) => !open && (folderDialog = null)}>
	<Dialog.Content class="sm:max-w-sm">
		<Dialog.Header>
			<Dialog.Title>{folderDialog?.id ? m['assistant.rename_folder']() : m['assistant.new_folder']()}</Dialog.Title>
		</Dialog.Header>
		{#if folderDialog}
			<form
				class="space-y-4"
				onsubmit={(event) => {
					event.preventDefault();
					void saveFolder();
				}}
			>
				<Input aria-label={m['assistant.folder_name']()} maxlength={80} bind:value={folderDialog.name} />
				<Dialog.Footer>
					<Button type="button" variant="outline" onclick={() => (folderDialog = null)}>{m['common.cancel']()}</Button>
					<Button type="submit" disabled={!folderDialog.name.trim()}>{m['assistant.save_folder']()}</Button>
				</Dialog.Footer>
			</form>
		{/if}
	</Dialog.Content>
</Dialog.Root>

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
