<script lang="ts">
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import { m } from '$lib/paraglide/messages.js';
	import {
		ArrowDown,
		ArrowUp,
		ChevronRight,
		CornerLeftUp,
		FileText,
		Folder,
		FolderOpen,
		FolderPlus,
		MessageSquare,
		MoreHorizontal,
		Pencil,
		Plus,
		Trash2
	} from '@lucide/svelte';
	import {
		WIKI_PAGE_DRAG_TYPE,
		buildTree,
		dropZone,
		moveFor,
		subtreeIds,
		type DropZone,
		type Move,
		type TreeItem,
		type TreeNode
	} from './wiki-tree';

	// The wiki's pages as a tree, with the assistant sidebar's interactions:
	// drag to reorder or nest, inline rename, a row menu that offers every move
	// from the keyboard, and collapsible folders.
	type Item = TreeItem & { title: string };

	let {
		items,
		selectedId,
		canWrite,
		onmove,
		oncreate,
		onrename,
		ondelete,
		onchat
	}: {
		items: Item[];
		selectedId: string | null;
		canWrite: boolean;
		onmove: (move: Move) => void;
		oncreate: (kind: 'page' | 'folder', parentId: string | null) => void;
		onrename: (id: string, title: string) => void;
		ondelete: (id: string) => void;
		onchat?: (id: string) => void;
	} = $props();

	const tree = $derived(buildTree(items));
	// Folders holding the open page start expanded; toggles are remembered.
	let toggled = $state<Record<string, boolean>>({});
	const ancestors = $derived.by(() => {
		const open = new Set<string>();
		const byId = new Map(items.map((item) => [item.id, item]));
		for (
			let at = selectedId ? byId.get(selectedId) : undefined;
			at?.parent_id && !open.has(at.parent_id);
			at = byId.get(at.parent_id)
		)
			open.add(at.parent_id);
		return open;
	});
	const isOpen = (id: string) => toggled[id] ?? ancestors.has(id);

	let renaming = $state<{ id: string; title: string } | null>(null);
	let dragging = $state<string | null>(null);
	let target = $state<{ id: string | null; zone: DropZone } | null>(null);

	function dragStart(event: DragEvent, id: string) {
		if (!canWrite || !event.dataTransfer) return;
		event.dataTransfer.setData(WIKI_PAGE_DRAG_TYPE, id);
		event.dataTransfer.effectAllowed = 'move';
		dragging = id;
	}
	function dragEnd() {
		dragging = null;
		target = null;
	}
	function isOurs(event: DragEvent) {
		return !!dragging || !!event.dataTransfer?.types.includes(WIKI_PAGE_DRAG_TYPE);
	}
	function dragOver(event: DragEvent, id: string | null) {
		if (!canWrite || !isOurs(event)) return;
		const zone = id ? dropZone(event.offsetY, (event.currentTarget as HTMLElement).offsetHeight) : 'inside';
		if (!dragging || !moveFor(items, dragging, id, zone)) {
			target = null;
			return;
		}
		event.preventDefault();
		event.stopPropagation();
		if (event.dataTransfer) event.dataTransfer.dropEffect = 'move';
		target = { id, zone };
	}
	function drop(event: DragEvent, id: string | null) {
		if (!canWrite || !isOurs(event)) return;
		event.preventDefault();
		event.stopPropagation();
		const dragged = event.dataTransfer?.getData(WIKI_PAGE_DRAG_TYPE) || dragging;
		const zone = target?.zone ?? 'inside';
		dragEnd();
		move(dragged ? moveFor(items, dragged, id, zone) : null);
	}

	// Every move opens the folder it lands in, so the page stays in sight.
	function move(next: Move | null) {
		if (!next) return;
		if (next.parent_id) toggled = { ...toggled, [next.parent_id]: true };
		onmove(next);
	}

	function siblingsOf(item: Item) {
		return items
			.filter((other) => other.parent_id === item.parent_id)
			.sort((a, b) => a.position - b.position || a.id.localeCompare(b.id));
	}
	function step(item: Item, by: -1 | 1) {
		const siblings = siblingsOf(item);
		const at = siblings.findIndex((other) => other.id === item.id);
		const neighbour = siblings[at + by];
		if (neighbour) move(moveFor(items, item.id, neighbour.id, by < 0 ? 'before' : 'after'));
	}
	function folders(except: string) {
		const blocked = subtreeIds(items, except);
		return items.filter((item) => item.kind === 'folder' && !blocked.has(item.id));
	}

	function saveRename() {
		const current = renaming;
		renaming = null;
		const title = current?.title.trim();
		const original = items.find((item) => item.id === current?.id)?.title;
		if (current && title && title !== original) onrename(current.id, title);
	}
	function focus(node: HTMLInputElement) {
		node.focus();
		node.select();
	}

	const indicator = (id: string | null, zone: DropZone) => target?.id === id && target.zone === zone;
</script>

{#snippet row(node: TreeNode<Item>)}
	{@const item = node.item}
	{@const folder = item.kind === 'folder'}
	{@const open = isOpen(item.id)}
	{@const Icon = folder ? (open ? FolderOpen : Folder) : FileText}
	<!-- Dragging is a pointer shortcut; the row menu offers the same moves from the keyboard. -->
	<!-- svelte-ignore a11y_no_static_element_interactions -->
	<div
		role="treeitem"
		tabindex="-1"
		aria-selected={item.id === selectedId}
		aria-expanded={node.children.length > 0 || folder ? open : undefined}
		aria-level={node.depth + 1}
		data-page={item.id}
		class="group relative flex h-8 min-w-0 items-center rounded-md {item.id === selectedId
			? 'bg-[var(--color-bg-hover)]'
			: 'hover:bg-[var(--color-bg-hover)]'} {dragging === item.id ? 'opacity-50' : ''} {indicator(item.id, 'inside')
			? 'ring-1 ring-[var(--app-accent)]'
			: ''}"
		style="padding-left: {node.depth * 0.75}rem"
		draggable={canWrite && renaming?.id !== item.id}
		ondragstart={(event) => dragStart(event, item.id)}
		ondragend={dragEnd}
		ondragover={(event) => dragOver(event, item.id)}
		ondragleave={() => target?.id === item.id && (target = null)}
		ondrop={(event) => drop(event, item.id)}
	>
		{#if indicator(item.id, 'before')}<span
				class="pointer-events-none absolute inset-x-1 top-0 h-0.5 rounded bg-[var(--app-accent)]"
			></span>{/if}
		{#if indicator(item.id, 'after')}<span
				class="pointer-events-none absolute inset-x-1 bottom-0 h-0.5 rounded bg-[var(--app-accent)]"
			></span>{/if}
		{#if node.children.length > 0 || folder}
			<Button
				variant="plain"
				class="flex size-5 shrink-0 items-center justify-center rounded text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)]"
				aria-label={open
					? m['wiki.tree.collapse']({ title: item.title })
					: m['wiki.tree.expand']({ title: item.title })}
				onclick={() => (toggled = { ...toggled, [item.id]: !open })}
				><ChevronRight size={12} class="transition-transform {open ? 'rotate-90' : ''}" /></Button
			>
		{:else}
			<span class="size-5 shrink-0"></span>
		{/if}
		{#if renaming?.id === item.id}
			<Input
				aria-label={m['wiki.tree.rename_label']()}
				maxlength={200}
				class="h-7 min-w-0 flex-1 text-sm"
				bind:value={renaming.title}
				onkeydown={(event) => {
					if (event.key === 'Enter') saveRename();
					if (event.key === 'Escape') renaming = null;
				}}
				onblur={saveRename}
				{@attach focus}
			/>
		{:else}
			<a
				href="?page={item.id}"
				draggable="false"
				aria-current={item.id === selectedId ? 'page' : undefined}
				class="flex min-w-0 flex-1 items-center gap-2 py-1.5 pr-1 text-sm {item.id === selectedId
					? 'text-[var(--color-text-primary)]'
					: 'text-[var(--color-text-secondary)] hover:text-[var(--color-text-primary)]'}"
				ondblclick={(event) => {
					if (!canWrite) return;
					event.preventDefault();
					renaming = { id: item.id, title: item.title };
				}}
			>
				<Icon size={14} class="shrink-0 text-[var(--color-text-tertiary)]" />
				<span class="truncate">{item.title}</span>
			</a>
		{/if}
		{#if canWrite || onchat}
			<DropdownMenu.Root>
				<DropdownMenu.Trigger>
					{#snippet child({ props })}
						<Button
							{...props}
							variant="ghost"
							size="icon-sm"
							class="mr-0.5 size-6 shrink-0 opacity-0 group-hover:opacity-100 focus-visible:opacity-100 data-[state=open]:opacity-100"
							aria-label={m['wiki.tree.actions']({ title: item.title })}><MoreHorizontal /></Button
						>
					{/snippet}
				</DropdownMenu.Trigger>
				<DropdownMenu.Content align="end" class="w-52">
					{#if onchat}
						<DropdownMenu.Item onSelect={() => onchat?.(item.id)}
							><MessageSquare />{m['wiki.open_in_chat']()}</DropdownMenu.Item
						>
					{/if}
					{#if canWrite}
						<DropdownMenu.Item onSelect={() => oncreate('page', item.id)}
							><Plus />{m['wiki.tree.new_child']()}</DropdownMenu.Item
						>
						<DropdownMenu.Item onSelect={() => (renaming = { id: item.id, title: item.title })}
							><Pencil />{m['wiki.tree.rename']()}</DropdownMenu.Item
						>
						<DropdownMenu.Separator />
						<DropdownMenu.Item onSelect={() => step(item, -1)} disabled={siblingsOf(item)[0]?.id === item.id}
							><ArrowUp />{m['wiki.tree.move_up']()}</DropdownMenu.Item
						>
						<DropdownMenu.Item onSelect={() => step(item, 1)} disabled={siblingsOf(item).at(-1)?.id === item.id}
							><ArrowDown />{m['wiki.tree.move_down']()}</DropdownMenu.Item
						>
						<DropdownMenu.Sub>
							<DropdownMenu.SubTrigger><Folder />{m['wiki.tree.move_to']()}</DropdownMenu.SubTrigger>
							<DropdownMenu.SubContent class="max-h-72 overflow-y-auto">
								<DropdownMenu.Item
									disabled={!item.parent_id}
									onSelect={() => move(moveFor(items, item.id, null, 'inside'))}
									><CornerLeftUp />{m['wiki.tree.top_level']()}</DropdownMenu.Item
								>
								{#each folders(item.id) as folder (folder.id)}
									<DropdownMenu.Item
										disabled={item.parent_id === folder.id}
										onSelect={() => move(moveFor(items, item.id, folder.id, 'inside'))}
										>{folder.title}</DropdownMenu.Item
									>
								{/each}
							</DropdownMenu.SubContent>
						</DropdownMenu.Sub>
						<DropdownMenu.Separator />
						<DropdownMenu.Item variant="destructive" onSelect={() => ondelete(item.id)}
							><Trash2 />{folder ? m['wiki.tree.delete_folder']() : m['wiki.delete']()}</DropdownMenu.Item
						>
					{/if}
				</DropdownMenu.Content>
			</DropdownMenu.Root>
		{/if}
	</div>
	{#if node.children.length > 0 && open}
		<div role="group">
			{#each node.children as child (child.item.id)}
				{@render row(child)}
			{/each}
		</div>
	{:else if folder && open && node.children.length === 0}
		<p class="py-1 text-xs text-[var(--color-text-tertiary)]" style="padding-left: {(node.depth + 1) * 0.75 + 1.75}rem">
			{m['wiki.tree.empty_folder']()}
		</p>
	{/if}
{/snippet}

<div class="flex items-center justify-between gap-1 px-4 pt-3 pb-1">
	<h2 class="text-[11px] font-medium tracking-wider text-[var(--color-text-tertiary)] uppercase">
		{m['wiki.pages']()}
	</h2>
	{#if canWrite}
		<div class="flex items-center">
			<Button
				variant="ghost"
				size="icon-sm"
				class="size-6"
				aria-label={m['wiki.tree.new_folder']()}
				title={m['wiki.tree.new_folder']()}
				onclick={() => oncreate('folder', null)}><FolderPlus size={14} /></Button
			>
			<Button
				variant="ghost"
				size="icon-sm"
				class="size-6"
				aria-label={m['wiki.new_page']()}
				title={m['wiki.new_page']()}
				onclick={() => oncreate('page', null)}><Plus size={14} /></Button
			>
		</div>
	{/if}
</div>
<!-- The list itself is the top-level drop target. -->
<div
	role="tree"
	aria-label={m['wiki.pages']()}
	tabindex="-1"
	class="flex min-h-12 flex-col gap-px px-2 pb-2 {target?.id === null
		? 'rounded-md bg-[var(--color-bg-hover)]/40'
		: ''}"
	ondragover={(event) => dragOver(event, null)}
	ondrop={(event) => drop(event, null)}
>
	{#each tree as node (node.item.id)}
		{@render row(node)}
	{:else}
		<p class="px-2 py-1 text-xs text-[var(--color-text-tertiary)]">{m['wiki.no_pages']()}</p>
	{/each}
</div>
