<script lang="ts">
	import { Button } from '$lib/components/ui/button/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import * as Tooltip from '$lib/components/ui/tooltip/index.js';
	import { Ellipsis } from '@lucide/svelte';
	import AgentLogo from '$lib/features/agents/AgentLogo.svelte';
	import { m } from '$lib/features/i18n/messages';
	import type { TaskAction, TaskMenuEntry } from './task-actions';
	// The task card's one overflow menu: every action that is not a next step,
	// each with an icon and a label. Tools come first, then the rest (retry,
	// cancel). An action with entries opens a submenu (the task app, reviews).
	let { actions }: { actions: TaskAction[] } = $props();
	const tools = $derived(actions.filter((a) => a.group === 'tools'));
	const more = $derived(actions.filter((a) => a.group === 'more'));
	// The menu's own state dot mirrors a running or failed tool (the app).
	const indicator = $derived(tools.find((a) => a.indicator)?.indicator);
	// An item that focuses something else (the composer, a terminal) runs once
	// the menu has closed, so the menu's focus handling cannot take it back.
	let handoff: (() => void) | undefined;
	const choose = (item: { onclick?: () => void; movesFocus?: boolean }) => () => {
		if (item.movesFocus) handoff = item.onclick;
		else item.onclick?.();
	};
	const indicatorClass: Record<NonNullable<TaskAction['indicator']>, string> = {
		starting: 'animate-pulse bg-[var(--status-running)] motion-reduce:animate-none',
		running: 'bg-[var(--status-success)]',
		failed: 'bg-[var(--status-error)]'
	};
</script>

{#snippet entryView(entry: TaskMenuEntry)}
	{#if entry.kind === 'note'}<DropdownMenu.Label
			class="text-xs font-normal break-words whitespace-normal {entry.tone === 'error'
				? 'text-destructive'
				: 'text-muted-foreground'}">{entry.text}</DropdownMenu.Label
		>{:else if entry.kind === 'separator'}<DropdownMenu.Separator />{:else}<DropdownMenu.Item
			variant={entry.variant === 'danger' ? 'destructive' : 'default'}
			disabled={entry.disabled}
			onclick={choose(entry)}
			>{#if entry.logo}<AgentLogo agent={entry.logo} size="sm" class="size-4" />{:else if entry.icon}<entry.icon
				/>{/if}<span class="min-w-0 truncate">{entry.label}</span>{#if entry.hint}{' '}<span
					class="ml-auto shrink-0 pl-2 text-xs text-muted-foreground {entry.logo ? '' : 'font-mono'}">{entry.hint}</span
				>{/if}</DropdownMenu.Item
		>{/if}
{/snippet}

{#snippet actionView(action: TaskAction)}
	{#if action.menu}
		<DropdownMenu.Sub onOpenChange={(open) => open && action.onopen?.()}>
			<DropdownMenu.SubTrigger disabled={action.disabled} aria-label={action.label}
				><action.icon /><span class="min-w-0 truncate">{action.label}</span>{#if action.indicator}<span
						aria-hidden="true"
						class="ml-1 size-1.5 shrink-0 rounded-full {indicatorClass[action.indicator]}"
					></span>{/if}</DropdownMenu.SubTrigger
			>
			<DropdownMenu.SubContent class="max-w-72 min-w-44">
				{#each action.menu as entry (entry.id)}{@render entryView(entry)}{/each}
			</DropdownMenu.SubContent>
		</DropdownMenu.Sub>
	{:else if action.href}
		<DropdownMenu.Item title={action.title}>
			{#snippet child({ props })}
				<a {...props} href={action.href} {...action.external ? { target: '_blank', rel: 'noopener noreferrer' } : {}}
					><action.icon /><span class="min-w-0 truncate">{action.label}</span></a
				>
			{/snippet}
		</DropdownMenu.Item>
	{:else}
		<DropdownMenu.Item
			variant={action.variant === 'danger' ? 'destructive' : 'default'}
			title={action.title}
			disabled={action.disabled}
			onclick={choose(action)}><action.icon /><span class="min-w-0 truncate">{action.label}</span></DropdownMenu.Item
		>
	{/if}
{/snippet}

{#if tools.length || more.length}
	<DropdownMenu.Root>
		<Tooltip.Root>
			<Tooltip.Trigger>
				{#snippet child({ props: tooltipProps })}
					<DropdownMenu.Trigger {...tooltipProps}>
						{#snippet child({ props })}
							<Button
								{...props}
								variant="ghost"
								size="icon-xs"
								class="relative text-muted-foreground hover:text-foreground"
								aria-label={m['tasks.more_actions']()}
								><Ellipsis />{#if indicator}<span
										aria-hidden="true"
										class="absolute top-0.5 right-0.5 size-1.5 rounded-full {indicatorClass[indicator]}"
									></span>{/if}</Button
							>
						{/snippet}
					</DropdownMenu.Trigger>
				{/snippet}
			</Tooltip.Trigger>
			<Tooltip.Content>{m['tasks.more_actions']()}</Tooltip.Content>
		</Tooltip.Root>
		<DropdownMenu.Content
			align="end"
			class="max-w-72 min-w-52"
			onCloseAutoFocus={(event) => {
				const run = handoff;
				handoff = undefined;
				if (!run) return;
				event.preventDefault();
				// After the menu's own focus handling has finished.
				setTimeout(run);
			}}
		>
			{#if tools.length}<DropdownMenu.Group aria-label={m['tasks.tools']()}>
					{#each tools as action (action.id)}{@render actionView(action)}{/each}
				</DropdownMenu.Group>{/if}
			{#if tools.length && more.length}<DropdownMenu.Separator />{/if}
			{#each more as action (action.id)}{@render actionView(action)}{/each}
		</DropdownMenu.Content>
	</DropdownMenu.Root>
{/if}
