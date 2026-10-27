<script lang="ts">
	import { Button } from '$lib/components/ui/button/index.js';
	import * as Tooltip from '$lib/components/ui/tooltip/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import { Ellipsis } from '@lucide/svelte';
	import { m } from '$lib/paraglide/messages.js';
	import type { TaskAction } from './task-actions';
	let { actions, compact = false }: { actions: TaskAction[]; compact?: boolean } = $props();
	const external = (action: TaskAction) => (action.external ? { target: '_blank', rel: 'noopener noreferrer' } : {});
	const main = $derived(actions.filter((a) => a.group === 'main'));
	const tools = $derived(actions.filter((a) => a.group === 'tools'));
	const more = $derived(actions.filter((a) => a.group === 'more'));
</script>

{#snippet moreMenu()}
	{#if more.length}<DropdownMenu.Root>
			<DropdownMenu.Trigger>
				{#snippet child({ props })}
					<Button {...props} variant="ghost" size="icon-sm" class="ml-auto" aria-label={m['tasks.more_actions']()}
						><Ellipsis /></Button
					>
				{/snippet}
			</DropdownMenu.Trigger>
			<DropdownMenu.Content align="end">
				{#each more as action (action.id)}<DropdownMenu.Item
						variant={action.variant === 'danger' ? 'destructive' : 'default'}
						disabled={action.disabled}
						onclick={action.onclick}><action.icon />{action.label}</DropdownMenu.Item
					>{/each}
			</DropdownMenu.Content>
		</DropdownMenu.Root>{/if}
{/snippet}

{#snippet iconButton(action: TaskAction)}
	<Tooltip.Root>
		<Tooltip.Trigger>
			{#snippet child({ props })}
				<Button
					{...props}
					{...external(action)}
					variant="ghost"
					size="icon-sm"
					class={action.variant === 'danger' ? 'text-destructive hover:text-destructive' : ''}
					aria-label={action.label}
					href={action.href}
					disabled={action.disabled}
					onclick={action.onclick}><action.icon /></Button
				>
			{/snippet}
		</Tooltip.Trigger>
		<Tooltip.Content>{action.title ?? action.label}</Tooltip.Content>
	</Tooltip.Root>
{/snippet}

{#if compact}
	<div class="flex items-center gap-0.5">
		{#each [...main, ...tools] as action (action.id)}{@render iconButton(action)}{/each}
		{@render moreMenu()}
	</div>
{:else}
	<div class="flex items-center gap-2">
		{#each main as action (action.id)}<Button
				variant={action.variant === 'primary' ? 'default' : 'outline'}
				size="sm"
				{...external(action)}
				href={action.href}
				title={action.title}
				disabled={action.disabled}
				onclick={action.onclick}><action.icon />{action.label}</Button
			>{/each}
		{#if tools.length}<div class="flex items-center gap-0.5" role="group" aria-label={m['tasks.tools']()}>
				{#each tools as action (action.id)}{@render iconButton(action)}{/each}
			</div>{/if}
		{@render moreMenu()}
	</div>
{/if}
