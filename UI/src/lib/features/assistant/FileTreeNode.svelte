<script lang="ts">
	import { ChevronRight, File, Folder } from '@lucide/svelte';
	import * as Collapsible from '$lib/components/ui/collapsible/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import FileTreeNode from './FileTreeNode.svelte';
	import type { FileNode } from './agent-trace';
	import { m } from '$lib/features/i18n/messages';
	// Without `select`, files are links (href) or plain labels and folders start
	// open: an attempt's changed files. With it (a task's files panel), files
	// are buttons, the selected one is the current file, and folders follow
	// `open` and report toggles through `toggle`.
	let {
		node,
		href,
		depth = 0,
		selected,
		select,
		open,
		toggle
	}: {
		node: FileNode;
		href?: (path: string) => string | undefined;
		depth?: number;
		selected?: string | null;
		select?: (path: string) => void;
		open?: (path: string, depth: number) => boolean;
		toggle?: (path: string, open: boolean) => void;
	} = $props();
	const link = $derived(node.type === 'file' && !select ? href?.(node.path) : undefined);
	// A small ghost Button without its aria-expanded highlight (an open folder is
	// not a selected one); its chevron turns by its own trigger's state, not an
	// open folder above it.
	const folderClass =
		"group/trigger inline-flex h-7 w-full shrink-0 items-center justify-start gap-1 rounded-[min(var(--radius-md),12px)] border border-transparent bg-clip-padding px-2.5 text-[0.8rem] font-medium whitespace-nowrap outline-none select-none transition-all hover:bg-muted hover:text-foreground focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 dark:hover:bg-muted/50 [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-3.5";
	const current = $derived(node.type === 'file' && !!select && selected === node.path);
</script>

{#snippet changed(label: string)}<span
		role="img"
		aria-label={label}
		title={label}
		class="ml-auto size-1.5 shrink-0 rounded-full bg-[var(--status-warning)]"
	></span>{/snippet}

<!-- Official AI Elements File Tree composition: Collapsible folders, Button files. -->
{#if node.type === 'folder'}
	<Collapsible.Root
		open={open ? open(node.path, depth) : true}
		onOpenChange={(value) => toggle?.(node.path, value)}
	>
		<Collapsible.Trigger>
			{#snippet child({ props })}
				<Button {...props} variant="plain" class={folderClass}
					><ChevronRight class="transition-transform group-data-[state=open]/trigger:rotate-90" /><Folder /><span
						class="truncate">{node.name}</span
					>{#if node.changed}{@render changed(m['tasks.files_folder_changed']())}{/if}</Button
				>
			{/snippet}
		</Collapsible.Trigger>
		<Collapsible.Content>
			<div class="ml-4 flex flex-col">
				{#each node.children as entry (entry.path)}<FileTreeNode
						node={entry}
						{href}
						depth={depth + 1}
						{selected}
						{select}
						{open}
						{toggle}
					/>{/each}
			</div>
		</Collapsible.Content>
	</Collapsible.Root>
{:else if select}
	<Button
		variant={current ? 'secondary' : 'ghost'}
		size="sm"
		class="h-7 w-full justify-start"
		title={node.path}
		aria-current={current ? 'true' : undefined}
		onclick={() => select(node.path)}
		><File /><span class="truncate">{node.name}</span>{#if node.changed}{@render changed(
				m['tasks.files_changed']()
			)}{/if}</Button
	>
{:else if link}
	<Button
		variant="ghost"
		size="sm"
		class="h-7 w-full justify-start"
		href={link}
		target="_blank"
		rel="noopener noreferrer"><File /><span class="truncate">{node.name}</span></Button
	>
{:else}
	<Button variant="ghost" size="sm" class="h-7 w-full justify-start" disabled
		><File /><span class="truncate">{node.name}</span></Button
	>
{/if}
