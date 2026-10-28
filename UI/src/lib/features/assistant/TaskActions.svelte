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
	const indicatorClass: Record<NonNullable<TaskAction['indicator']>, string> = {
		starting: 'animate-pulse bg-[var(--status-running)]',
		running: 'bg-[var(--status-success)]',
		failed: 'bg-[var(--status-error)]'
	};
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

{#snippet menuButton(action: TaskAction)}
	<DropdownMenu.Root onOpenChange={(open) => open && action.onopen?.()}>
		<Tooltip.Root>
			<Tooltip.Trigger>
				{#snippet child({ props: tooltipProps })}
					<DropdownMenu.Trigger {...tooltipProps}>
						{#snippet child({ props })}
							<Button
								{...props}
								variant="ghost"
								size="icon-sm"
								class="relative"
								aria-label={action.label}
								disabled={action.disabled}
								><action.icon />{#if action.indicator}<span
										aria-hidden="true"
										class="absolute top-1 right-1 size-1.5 rounded-full {indicatorClass[action.indicator]}"
									></span>{/if}</Button
							>
						{/snippet}
					</DropdownMenu.Trigger>
				{/snippet}
			</Tooltip.Trigger>
			<Tooltip.Content>{action.title ?? action.label}</Tooltip.Content>
		</Tooltip.Root>
		<DropdownMenu.Content align="end" class="min-w-44 max-w-72">
			{#each action.menu ?? [] as entry (entry.id)}
				{#if entry.kind === 'note'}<DropdownMenu.Label
						class="text-xs font-normal break-words whitespace-normal {entry.tone === 'error'
							? 'text-destructive'
							: 'text-muted-foreground'}">{entry.text}</DropdownMenu.Label
					>{:else if entry.kind === 'separator'}<DropdownMenu.Separator />{:else}<DropdownMenu.Item
						variant={entry.variant === 'danger' ? 'destructive' : 'default'}
						disabled={entry.disabled}
						onclick={entry.onclick}
						><entry.icon /><span class="min-w-0 truncate">{entry.label}</span>{#if entry.hint}{' '}<span
								class="ml-auto shrink-0 font-mono text-xs text-muted-foreground">{entry.hint}</span
							>{/if}</DropdownMenu.Item
					>{/if}
			{/each}
		</DropdownMenu.Content>
	</DropdownMenu.Root>
{/snippet}

{#snippet iconButton(action: TaskAction)}
	{#if action.menu}{@render menuButton(action)}{:else}<Tooltip.Root>
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
		</Tooltip.Root>{/if}
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
