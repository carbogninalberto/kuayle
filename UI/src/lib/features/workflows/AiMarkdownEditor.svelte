<script lang="ts">
	import type { Snippet } from 'svelte';
	import { ArrowUp, LoaderCircle, Sparkles, TextSelect, Undo2 } from '@lucide/svelte';
	import * as Tabs from '$lib/components/ui/tabs/index.js';
	import * as Tooltip from '$lib/components/ui/tooltip/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Textarea } from '$lib/components/ui/textarea/index.js';
	import { aiWrite, type AiWriteKind } from '$lib/api/ai-write';
	import { appToast } from '$lib/features/toast/toast';
	import { cn } from '$lib/utils.js';
	import { m } from '$lib/features/i18n/messages';
	import Markdown from '$lib/features/assistant/Markdown.svelte';

	// A Markdown editor with Write and Preview and an "Ask AI" bar inside its
	// frame: with text selected the AI rewrites only the selection, otherwise it
	// writes the whole text from the request. Every AI change can be undone and
	// nothing is saved until the caller saves. Used for skills, agent profile
	// instructions and workflow step prompts.
	let {
		value = $bindable(''),
		slug,
		kind,
		label,
		editable = true,
		placeholder,
		class: className = 'min-h-72',
		mono = true,
		preview,
		actions,
		testid
	}: {
		value: string;
		slug: string;
		kind: AiWriteKind;
		/** The editor's accessible name. */
		label: string;
		editable?: boolean;
		placeholder?: string;
		/** Height of the writing area. */
		class?: string;
		mono?: boolean;
		/** A custom preview of the text; Markdown by default. */
		preview?: Snippet<[string]>;
		/** Extra icon actions in the header, before Ask AI. */
		actions?: Snippet;
		testid?: string;
	} = $props();

	let view = $state<'write' | 'preview'>('write'),
		editor = $state<HTMLTextAreaElement | null>(null),
		asking = $state(false),
		open = $state(false),
		request = $state(''),
		selection = $state<{ start: number; end: number } | null>(null),
		before = $state<string | null>(null),
		input = $state<HTMLInputElement | null>(null);

	function track() {
		if (!editor) return;
		const { selectionStart: start, selectionEnd: end } = editor;
		selection = end > start ? { start, end } : null;
	}

	async function toggleAI() {
		open = !open;
		view = 'write';
		if (open) {
			await Promise.resolve();
			input?.focus();
		}
	}

	async function send(event?: SubmitEvent) {
		event?.preventDefault();
		const range = selection;
		if (asking || (!range && !request.trim())) return;
		asking = true;
		try {
			const { text } = await aiWrite(slug, {
				kind,
				mode: range ? 'rewrite' : 'generate',
				instruction: request.trim(),
				source: value,
				...(range ? { selection: value.slice(range.start, range.end) } : {})
			});
			before = value;
			value = range ? value.slice(0, range.start) + text + value.slice(range.end) : text + '\n';
			selection = null;
			request = '';
			appToast.success(m['ai_editor.done']());
		} catch (error) {
			const code = (error as { error?: { code?: string } }).error?.code;
			if (code === 'AI_NOT_CONFIGURED') appToast.error(m['ai_editor.not_configured']());
			else appToast.apiError(error, m['ai_editor.failed']());
		} finally {
			asking = false;
		}
	}
</script>

<Tooltip.Provider delayDuration={300}>
	<div
		class="overflow-hidden rounded-lg border bg-background transition-colors focus-within:border-ring"
		data-testid={testid}
	>
		<div class="flex items-center gap-1 border-b bg-muted/30 px-1.5 py-1">
			<Tabs.Root bind:value={view}>
				<Tabs.List class="h-7">
					<Tabs.Trigger value="write" class="px-2 text-xs">{m['ai_editor.write']()}</Tabs.Trigger>
					<Tabs.Trigger value="preview" class="px-2 text-xs">{m['ai_editor.preview']()}</Tabs.Trigger>
				</Tabs.List>
			</Tabs.Root>
			<div class="ml-auto flex items-center gap-0.5">
				{#if editable}
					{@render actions?.()}
					{#if before !== null && !asking}
						<Tooltip.Root>
							<Tooltip.Trigger>
								{#snippet child({ props })}<Button
										{...props}
										size="icon-sm"
										variant="ghost"
										aria-label={m['ai_editor.undo']()}
										onclick={() => {
											value = before ?? value;
											before = null;
										}}><Undo2 /></Button
									>{/snippet}
							</Tooltip.Trigger>
							<Tooltip.Content>{m['ai_editor.undo']()}</Tooltip.Content>
						</Tooltip.Root>
					{/if}
					<Tooltip.Root>
						<Tooltip.Trigger>
							{#snippet child({ props })}<Button
									{...props}
									size="sm"
									variant={open ? 'secondary' : 'ghost'}
									class="h-7 gap-1.5 px-2 text-xs"
									aria-pressed={open}
									onclick={toggleAI}><Sparkles class="size-3.5 text-violet-500" />{m['ai_editor.ask']()}</Button
								>{/snippet}
						</Tooltip.Trigger>
						<Tooltip.Content class="max-w-64">{m['ai_editor.hint']()}</Tooltip.Content>
					</Tooltip.Root>
				{/if}
			</div>
		</div>

		{#if view === 'write'}
			<Textarea
				variant="plain"
				aria-label={label}
				bind:ref={editor}
				bind:value
				{placeholder}
				disabled={!editable || asking}
				spellcheck={!mono}
				onselect={track}
				onkeyup={track}
				onmouseup={track}
				oninput={() => (selection = null)}
				class={cn(
					'block w-full resize-y bg-transparent px-3 py-2.5 leading-relaxed outline-none placeholder:text-muted-foreground disabled:opacity-70',
					mono ? 'font-mono text-[13px]' : 'text-sm',
					className
				)}
			/>
		{:else}
			<div class={cn('px-4 py-3', className)}>
				{#if preview}{@render preview(value)}
				{:else if value.trim()}<Markdown source={value} class="text-sm" />
				{:else}<p class="text-sm text-muted-foreground">{m['ai_editor.empty']()}</p>{/if}
			</div>
		{/if}

		{#if open && editable}
			<form class="flex items-center gap-2 border-t bg-muted/30 px-2 py-1.5" onsubmit={send}>
				{#if asking}<LoaderCircle class="size-4 shrink-0 animate-spin text-violet-500" />
				{:else}<Sparkles class="size-4 shrink-0 text-violet-500" />{/if}
				{#if selection}<span
						class="flex shrink-0 items-center gap-1 rounded-md bg-violet-500/10 px-1.5 py-0.5 text-[11px] text-violet-700 dark:text-violet-300"
						><TextSelect class="size-3" />{m['ai_editor.selection']({ count: selection.end - selection.start })}</span
					>{/if}
				<Input
					bind:ref={input}
					bind:value={request}
					variant="plain"
					maxlength={2000}
					disabled={asking}
					aria-label={m['ai_editor.ask']()}
					placeholder={asking
						? m['ai_editor.working']()
						: selection
							? m['ai_editor.placeholder_rewrite']()
							: m['ai_editor.placeholder_generate']()}
					class="h-7 min-w-0 flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground"
				/>
				<Button
					type="submit"
					size="icon-sm"
					class="size-7 rounded-full"
					aria-label={m['ai_editor.send']()}
					disabled={asking || (!selection && !request.trim())}><ArrowUp /></Button
				>
			</form>
		{/if}
	</div>
</Tooltip.Provider>
