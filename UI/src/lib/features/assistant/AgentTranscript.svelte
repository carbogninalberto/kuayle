<script lang="ts">
	import {
		Bot,
		Brain,
		CircleAlert,
		CircleCheck,
		CirclePause,
		FileDiff,
		Globe,
		LoaderCircle,
		Terminal,
		TriangleAlert,
		User,
		Wrench,
		Layers
	} from '@lucide/svelte';
	import * as Trace from '$lib/components/ui/trace/index.js';
	import * as CodeBlock from '$lib/components/ui/code-block/index.js';
	import * as Message from '$lib/components/ui/message/index.js';
	import * as Bubble from '$lib/components/ui/bubble/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Toggle } from '$lib/components/ui/toggle/index.js';
	import type { DevMachineLogChunk } from '$lib/types/dev-machine';
	import Markdown from './Markdown.svelte';
	import CopyMessageButton from './CopyMessageButton.svelte';
	import { markdownToPlainText } from './message-text';
	import {
		groupSession,
		outputPreview,
		parseAgentSession,
		type SessionBlock,
		type SessionEntry,
		type SessionTool
	} from './agent-transcript';
	import { elapsed, formatDuration } from './shell-rules';
	import { m } from '$lib/features/i18n/messages';

	// A coding agent's run as a readable session: its messages, one chip per
	// tool call (expandable to the command and its recorded result), one row
	// per subagent (expandable to what it did), thinking markers and the
	// session header, parsed from the run's recorded native
	// output. "Raw" shows that output unchanged, for debugging. All of it is
	// the agent's untrusted output: escaped text, Markdown only through the
	// sanitizer.
	let {
		chunks,
		active = false
	}: {
		chunks: Pick<DevMachineLogChunk, 'id' | 'stream' | 'content' | 'truncated' | 'created_at'>[];
		// The run is still going: an unanswered tool call is in progress.
		active?: boolean;
	} = $props();
	let raw = $state(false);
	let fullOutput = $state<Record<string, boolean>>({});
	const blocks = $derived(groupSession(parseAgentSession(chunks)));
	const rawText = $derived(
		chunks.map((line) => `${line.stream === 'stderr' ? '[stderr] ' : ''}${line.content}`).join('')
	);
	// Times come from when each line was recorded; a call started and
	// finished in the same recorded line has no measurable duration.
	const duration = (from?: string, to?: string) => {
		const ms = from ? elapsed(from, to) : undefined;
		return !ms ? '' : formatDuration(ms);
	};
	const running = (tool: SessionTool) => tool.status === 'running' && active;
	const toolIcon = (tool: SessionTool) =>
		tool.command ? Terminal : tool.name === 'file_change' ? FileDiff : tool.name === 'web_search' ? Globe : Wrench;
	function groupTime(tools: SessionTool[]) {
		const ends = tools.map((tool) => tool.ended).filter(Boolean) as string[];
		return ends.length ? duration(tools[0].started, ends.sort().at(-1)) : '';
	}
</script>

{#snippet output(
	id: string,
	text: string,
	title: string,
	variant: 'default' | 'destructive' = 'default',
	whole = false
)}
	<!-- Tool results are previewed; plain harness output scrolls in full, as before. -->
	{@const view = whole || fullOutput[id] ? { preview: text, hidden: 0 } : outputPreview(text)}
	<CodeBlock.Root size="sm" {variant}>
		<CodeBlock.Header
			><CodeBlock.Title>{title}</CodeBlock.Title><CodeBlock.Actions
				><CodeBlock.CopyButton value={text} aria-label={m['assistant.copy']()} /></CodeBlock.Actions
			></CodeBlock.Header
		>
		<CodeBlock.Content code={view.preview} wrap class="{whole ? 'max-h-72' : 'max-h-96'} text-xs" />
	</CodeBlock.Root>
	{#if !whole && (view.hidden || fullOutput[id])}<Button
			variant="ghost"
			size="sm"
			class="h-6 px-2 text-xs"
			aria-expanded={!!fullOutput[id]}
			onclick={() => (fullOutput = { ...fullOutput, [id]: !fullOutput[id] })}
			>{fullOutput[id]
				? m['tasks.transcript_show_less']()
				: m['tasks.transcript_show_more']({ count: view.hidden })}</Button
		>{/if}
{/snippet}

{#snippet toolView(tool: SessionTool)}
	{@const Icon = toolIcon(tool)}
	{@const time = duration(tool.started, tool.ended)}
	<Trace.Collapsible>
		<Trace.CollapsibleTrigger>
			<Trace.Line status={tool.status === 'failed' ? 'error' : running(tool) ? 'running' : undefined}>
				<Trace.Icon
					>{#if tool.status === 'failed'}<CircleAlert />{:else if running(tool)}<LoaderCircle
							class="animate-spin"
						/>{:else}<Icon />{/if}</Trace.Icon
				>
				<Trace.Title class="break-words whitespace-normal"
					>{#if tool.status === 'failed'}<span class="sr-only">{m['tasks.transcript_step_failed']({ title: '' })}</span
						>{/if}{tool.title}</Trace.Title
				>{#if time}<Trace.Description class="shrink-0 tabular-nums">{time}</Trace.Description>{/if}
			</Trace.Line>
		</Trace.CollapsibleTrigger>
		<Trace.CollapsibleContent class="space-y-2">
			{#if tool.input}<CodeBlock.Root size="sm"
					><CodeBlock.Header
						><CodeBlock.Title
							>{tool.command ? m['tasks.transcript_command']() : m['assistant.tool_input']()}</CodeBlock.Title
						></CodeBlock.Header
					><CodeBlock.Content code={tool.input} wrap class="max-h-48 text-xs" /></CodeBlock.Root
				>{/if}
			{#if tool.output}{@render output(
					tool.id,
					tool.output,
					m['assistant.tool_output'](),
					tool.status === 'failed' ? 'destructive' : 'default'
				)}{:else if !running(tool)}<p class="text-xs text-muted-foreground">
					{m['tasks.transcript_no_result']()}
				</p>{/if}
		</Trace.CollapsibleContent>
	</Trace.Collapsible>
{/snippet}

{#snippet thinkingView(entry: Extract<SessionEntry, { kind: 'thinking' }>)}
	{@const label =
		entry.ms !== undefined
			? m['tasks.transcript_thought_for']({ time: formatDuration(entry.ms) })
			: m['tasks.transcript_thought']()}
	{#if entry.text}<Trace.Collapsible>
			<Trace.CollapsibleTrigger
				><Trace.Line><Trace.Icon><Brain /></Trace.Icon><Trace.Title>{label}</Trace.Title></Trace.Line
				></Trace.CollapsibleTrigger
			>
			<Trace.CollapsibleContent class="text-xs"><Markdown source={entry.text} /></Trace.CollapsibleContent>
		</Trace.Collapsible>{:else}<Trace.Line class="text-xs"
			><Trace.Icon><Brain /></Trace.Icon><Trace.Title>{label}</Trace.Title></Trace.Line
		>{/if}
{/snippet}

{#snippet subagentView(block: Extract<SessionBlock, { kind: 'subagent' }>)}
	{@const busy = block.status === 'running' && active}
	{@const failed = block.status === 'failed'}
	{@const time = busy ? '' : duration(block.tool.started, block.tool.ended)}
	<Trace.Collapsible>
		<Trace.CollapsibleTrigger>
			<Trace.Line status={failed ? 'error' : busy ? 'running' : undefined}>
				<Trace.Icon
					>{#if failed}<CircleAlert />{:else if busy}<LoaderCircle class="animate-spin" />{:else}<Bot
						/>{/if}</Trace.Icon
				>
				<Trace.Title class="break-words whitespace-normal"
					>{#if failed}<span class="sr-only">{m['tasks.transcript_step_failed']({ title: '' })}</span
						>{:else if busy}<span class="sr-only">{m['tasks.state.running']()}: </span>{/if}{block.tool
						.title}</Trace.Title
				>{#if time}<Trace.Description class="shrink-0 tabular-nums">{time}</Trace.Description>{/if}
			</Trace.Line>
		</Trace.CollapsibleTrigger>
		<Trace.CollapsibleContent class="flex flex-col gap-3">
			{#each block.blocks as inner (inner.key)}{@render blockView(inner)}{/each}
			{#if block.tool.output}{@render output(
					block.tool.id,
					block.tool.output,
					m['assistant.tool_output'](),
					failed ? 'destructive' : 'default'
				)}{:else if !block.blocks.length && block.tool.input}<CodeBlock.Root size="sm"
					><CodeBlock.Content code={block.tool.input} wrap class="max-h-48 text-xs" /></CodeBlock.Root
				>{/if}
		</Trace.CollapsibleContent>
	</Trace.Collapsible>
{/snippet}

{#snippet blockView(block: SessionBlock)}
	{#if block.kind === 'header'}
		<p
			class="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-0.5 text-xs text-muted-foreground"
			aria-label={m['tasks.transcript_header']()}
		>
			<Bot size={13} aria-hidden="true" class="shrink-0" />
			{#if block.model}<span class="font-medium text-foreground">{block.model}</span>{/if}
			{#if block.version}<span>{block.version}</span>{/if}
			{#if block.cwd}<span class="min-w-0 truncate font-mono" title={block.cwd}>{block.cwd}</span>{/if}
			{#if block.session}<span class="font-mono" title={block.session}>{block.session.slice(0, 12)}</span>{/if}
		</p>
	{:else if block.kind === 'text'}
		<Message.Root align="start" role="article" aria-label={m['tasks.live_from_agent']()}>
			<Message.Content class="w-full">
				<Bubble.Root variant="ghost"><Bubble.Content><Markdown source={block.text} /></Bubble.Content></Bubble.Root>
				<Message.Footer><CopyMessageButton text={() => markdownToPlainText(block.text)} /></Message.Footer>
			</Message.Content>
		</Message.Root>
	{:else if block.kind === 'user'}
		<Message.Root align="end" role="article" aria-label={m['tasks.transcript_from_you']()}>
			<Message.Content class="items-end">
				<Message.Header class="gap-1 text-[11px]"
					><User size={12} aria-hidden="true" />{m['tasks.transcript_from_you']()}<CopyMessageButton
						text={block.text}
					/></Message.Header
				>
				<Bubble.Root variant="muted"
					><Bubble.Content class="rounded-2xl rounded-br-md px-3.5 py-2"
						><p class="line-clamp-6 break-words whitespace-pre-wrap">{block.text}</p></Bubble.Content
					></Bubble.Root
				>
			</Message.Content>
		</Message.Root>
	{:else if block.kind === 'thinking'}
		<Trace.Root class="min-w-0">{@render thinkingView(block)}</Trace.Root>
	{:else if block.kind === 'tool'}
		<Trace.Root class="min-w-0">{@render toolView(block.tool)}</Trace.Root>
	{:else if block.kind === 'subagent'}
		<Trace.Root class="min-w-0">{@render subagentView(block)}</Trace.Root>
	{:else if block.kind === 'steps'}
		{@const busy = active && block.tools.some((tool) => tool.status === 'running')}
		{@const time = groupTime(block.tools)}
		<Trace.Root class="min-w-0">
			<Trace.Collapsible>
				<Trace.CollapsibleTrigger>
					<Trace.Line status={block.failed ? 'error' : busy ? 'running' : undefined}>
						<Trace.Icon
							>{#if busy}<LoaderCircle class="animate-spin" />{:else if block.commands}<Terminal />{:else}<Layers
								/>{/if}</Trace.Icon
						>
						<Trace.Title
							>{block.commands
								? m['tasks.transcript_ran_commands']({ count: block.tools.length })
								: m['tasks.transcript_used_tools']({ count: block.tools.length })}</Trace.Title
						>{#if block.failed}<Trace.Description class="shrink-0"
								>{m['tasks.transcript_failed_count']({ count: block.failed })}</Trace.Description
							>{/if}{#if time}<Trace.Description class="shrink-0 tabular-nums">{time}</Trace.Description>{/if}
					</Trace.Line>
				</Trace.CollapsibleTrigger>
				<Trace.CollapsibleContent>
					<Trace.Root class="min-w-0 gap-1.5 border-l pl-3">
						{#each block.entries as entry (entry.key)}
							{#if entry.kind === 'tool'}{@render toolView(entry.tool)}{:else}{@render thinkingView(entry)}{/if}
						{/each}
					</Trace.Root>
				</Trace.CollapsibleContent>
			</Trace.Collapsible>
		</Trace.Root>
	{:else if block.kind === 'omitted'}
		<Trace.Root class="min-w-0"
			><Trace.Line status="warning"
				><Trace.Icon><TriangleAlert /></Trace.Icon><Trace.Title class="break-words whitespace-normal"
					>{block.reason === 'limit' ? m['tasks.transcript_limit']() : m['tasks.transcript_too_large']()}</Trace.Title
				></Trace.Line
			></Trace.Root
		>
	{:else if block.kind === 'plain'}
		<div class="min-w-0 space-y-1">
			{@render output(
				block.key,
				block.text,
				block.stream === 'stderr' ? m['tasks.transcript_stderr']() : m['tasks.transcript_output'](),
				'default',
				true
			)}
		</div>
	{:else if block.kind === 'events'}
		<Trace.Root class="min-w-0">
			<Trace.Collapsible>
				<Trace.CollapsibleTrigger
					><Trace.Line
						><Trace.Icon><Layers /></Trace.Icon><Trace.Title
							>{m['tasks.transcript_other_events']({ count: block.events.length })}</Trace.Title
						></Trace.Line
					></Trace.CollapsibleTrigger
				>
				<Trace.CollapsibleContent class="space-y-2">
					{#each block.events as event (event.key)}{@render output(event.key, event.raw, event.type)}{/each}
				</Trace.CollapsibleContent>
			</Trace.Collapsible>
		</Trace.Root>
	{:else if block.kind === 'result'}
		{@const time = block.ms !== undefined ? formatDuration(block.ms) : ''}
		<Trace.Root class="min-w-0">
			<Trace.Line status={block.usageLimit ? 'warning' : block.failed ? 'error' : 'success'}
				><Trace.Icon
					>{#if block.usageLimit}<CirclePause />{:else if block.failed}<CircleAlert />{:else}<CircleCheck
						/>{/if}</Trace.Icon
				><Trace.Title class="break-words whitespace-normal"
					>{block.usageLimit
						? m['tasks.transcript_finished_limited']({ time: time || '—' })
						: block.failed
							? m['tasks.transcript_finished_failed']({ time: time || '—' })
							: m['tasks.transcript_finished']({ time: time || '—' })}</Trace.Title
				></Trace.Line
			>
			{#if block.text}<p
					class="text-xs break-words whitespace-pre-wrap {block.usageLimit
						? 'text-[var(--status-warning)]'
						: 'text-destructive'}"
				>
					{block.text}
				</p>{/if}
		</Trace.Root>
	{:else if block.kind === 'error'}
		<Trace.Root class="min-w-0"
			><Trace.Line status="error"
				><Trace.Icon><CircleAlert /></Trace.Icon><Trace.Title class="break-words whitespace-normal"
					>{m['tasks.transcript_error']({ message: block.message })}</Trace.Title
				></Trace.Line
			></Trace.Root
		>
	{/if}
{/snippet}

<section aria-label={m['tasks.transcript_title']()} class="min-w-0 space-y-3" data-testid="agent-transcript">
	<div class="flex min-w-0 items-center justify-between gap-2">
		<h3 class="text-[11px] font-medium tracking-wide text-muted-foreground uppercase">
			{m['tasks.transcript_title']()}
		</h3>
		<Toggle size="sm" variant="outline" bind:pressed={raw} title={m['tasks.transcript_raw_help']()}
			><Terminal />{m['tasks.transcript_raw']()}</Toggle
		>
	</div>
	{#if raw}
		<CodeBlock.Root
			><CodeBlock.Header
				><CodeBlock.Title>{m['tasks.output']()}</CodeBlock.Title><CodeBlock.Actions
					><CodeBlock.CopyButton value={rawText} aria-label={m['assistant.copy']()} /></CodeBlock.Actions
				></CodeBlock.Header
			><CodeBlock.Content code={rawText || m['tasks.no_output']()} wrap class="max-h-[32rem] text-xs" /></CodeBlock.Root
		>
	{:else if !blocks.length}
		<p class="text-xs text-muted-foreground">
			{active ? m['tasks.transcript_starting']() : m['tasks.no_output']()}
		</p>
	{:else}
		<div class="flex min-w-0 flex-col gap-3">
			{#each blocks as block (block.key)}{@render blockView(block)}{/each}
		</div>
	{/if}
</section>
