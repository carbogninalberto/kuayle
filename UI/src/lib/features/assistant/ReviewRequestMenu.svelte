<script lang="ts">
	import { ScanSearch } from '@lucide/svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import AgentLogo from '$lib/features/agents/AgentLogo.svelte';
	import { agentInfo } from '$lib/features/agents/agents';
	import { m } from '$lib/paraglide/messages.js';
	// Picks the harness for a review and hands it to the chat: the request is
	// made by the assistant, with approval, never directly from here.
	let {
		harnesses,
		implementer,
		onpick
	}: { harnesses: string[]; implementer: string; onpick: (harness: string) => void } = $props();
</script>

<DropdownMenu.Root>
	<DropdownMenu.Trigger>
		{#snippet child({ props })}
			<Button {...props} variant="outline" size="sm"><ScanSearch />{m['tasks.request_review']()}</Button>
		{/snippet}
	</DropdownMenu.Trigger>
	<DropdownMenu.Content align="start" class="min-w-56">
		<DropdownMenu.Label>{m['tasks.review_with']()}</DropdownMenu.Label>
		{#each harnesses as id (id)}
			<DropdownMenu.Item onclick={() => onpick(id)}
				><AgentLogo agent={id} size="sm" /><span class="min-w-0 flex-1 truncate">{agentInfo(id).name}</span
				>{#if id === implementer}<span class="text-xs text-muted-foreground">{m['tasks.review_same_agent']()}</span
					>{/if}</DropdownMenu.Item
			>
		{/each}
	</DropdownMenu.Content>
</DropdownMenu.Root>
