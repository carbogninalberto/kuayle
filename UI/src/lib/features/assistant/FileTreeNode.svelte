<script lang="ts">
	import { ChevronRight, File, Folder } from '@lucide/svelte';
	import * as Collapsible from '$lib/components/ui/collapsible/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import FileTreeNode from './FileTreeNode.svelte';
	import type { FileNode } from './agent-trace';
	let { node, href }: { node: FileNode; href?: (path: string) => string | undefined } = $props();
	const link = $derived(node.type === 'file' ? href?.(node.path) : undefined);
</script>

<!-- Official AI Elements File Tree composition: Collapsible folders, Button files. -->
{#if node.type === 'folder'}
	<Collapsible.Root open class="group/folder">
		<Collapsible.Trigger>
			{#snippet child({ props })}
				<Button {...props} variant="ghost" size="sm" class="h-7 w-full justify-start"
					><ChevronRight class="transition-transform group-data-[state=open]/folder:rotate-90" /><Folder /><span
						class="truncate">{node.name}</span
					></Button
				>
			{/snippet}
		</Collapsible.Trigger>
		<Collapsible.Content>
			<div class="ml-4 flex flex-col">
				{#each node.children as entry (entry.path)}<FileTreeNode node={entry} {href} />{/each}
			</div>
		</Collapsible.Content>
	</Collapsible.Root>
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
