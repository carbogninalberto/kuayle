<script lang="ts">
	import { onMount, tick, untrack } from 'svelte';
	import { goto } from '$app/navigation';
	import {
		ArrowUp,
		Square,
		Plus,
		History,
		Copy,
		Check,
		RefreshCw,
		MessageCircle,
		ArrowDown,
		Maximize2
	} from '@lucide/svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import * as Message from '$lib/components/ui/message/index.js';
	import * as Bubble from '$lib/components/ui/bubble/index.js';
	import * as MessageScroller from '$lib/components/ui/message-scroller/index.js';
	import * as InputGroup from '$lib/components/ui/input-group/index.js';
	import { conversations, type Conversation, type AssistantStatus, type MessageStatus } from '$lib/api/conversations';
	import { ConversationState } from './conversation-state.svelte';
	import TaskPanel from './TaskPanel.svelte';
	import ToolCalls from './ToolCalls.svelte';
	import AgentQueue from './AgentQueue.svelte';
	import ToolSources from './ToolSources.svelte';
	import Markdown from './Markdown.svelte';
	import ConversationNav from './ConversationNav.svelte';
	import LiveStatus from './LiveStatus.svelte';
	import DiffPanel from './DiffPanel.svelte';
	import * as Sheet from '$lib/components/ui/sheet/index.js';
	import * as Resizable from '$lib/components/ui/resizable/index.js';
	import { MediaQuery } from 'svelte/reactivity';
	import { authState } from '$lib/features/auth/auth.state.svelte';
	import { m } from '$lib/paraglide/messages.js';

	let {
		slug,
		conversationId,
		embedded = false
	}: { slug: string; conversationId: string | null; embedded?: boolean } = $props();
	// This component is keyed by workspace, conversation and authenticated user.
	const chat = untrack(() => new ConversationState(slug, conversationId ?? ''));
	const draftKey = untrack(() => `assistant-draft:${authState.user?.id}:${slug}:${conversationId ?? 'new'}`);
	let history = $state<Conversation[]>([]);
	let status = $state<AssistantStatus>({ configured: false });
	let initialized = $state(false);
	let text = $state('');
	let search = $state('');
	let showHistory = $state(false);
	// Panes only on wide screens; phones keep the stacked, toggleable list.
	const desktop = new MediaQuery('min-width: 768px');
	const wide = $derived(desktop.current);
	let sending = $state(false);
	let error = $state('');
	let copied = $state<string | null>(null);
	let textarea = $state<HTMLTextAreaElement | null>(null);
	let draftLoaded = $state(false);
	let pending: { id: string; content: string; conversation?: string } | null = null;
	let retryRequest: { message: string; id: string } | null = null;
	let disposed = false;
	let copyTimer: ReturnType<typeof setTimeout> | undefined;
	// Contextual starting prompts; selecting one sends it like a typed message.
	const suggestions = $derived(
		chat.issue
			? [
					m['assistant.suggest_issue_summary']({ identifier: chat.issue.identifier }),
					m['assistant.suggest_issue_next']({ identifier: chat.issue.identifier }),
					m['assistant.suggest_issue_delegate']({ identifier: chat.issue.identifier })
				]
			: [m['assistant.suggest_review'](), m['assistant.suggest_create'](), m['assistant.suggest_teams']()]
	);
	const canSend = $derived(
		initialized &&
			status.configured &&
			text.trim().length > 0 &&
			new TextEncoder().encode(text).length <= 16384 &&
			!sending &&
			!chat.busy &&
			!chat.unavailable &&
			!chat.loading &&
			!chat.failed
	);

	onMount(() => {
		try {
			text = localStorage.getItem(draftKey) ?? '';
			const saved = JSON.parse(localStorage.getItem(`${draftKey}:pending`) ?? 'null');
			if (saved && typeof saved.id === 'string' && saved.content === text) pending = saved;
		} catch {
			/* Storage can be disabled. */
		}
		draftLoaded = true;
		void Promise.all([embedded ? Promise.resolve([]) : conversations.list(slug), conversations.status(slug)])
			.then(([items, provider]) => {
				if (disposed) return;
				history = items;
				status = provider;
				initialized = true;
			})
			.catch(() => {
				if (!disposed) error = m['assistant.error']();
			});
		if (conversationId) void chat.load();
		else chat.loading = false;
		return () => {
			disposed = true;
			chat.dispose();
			clearTimeout(copyTimer);
		};
	});
	$effect(() => {
		if (!draftLoaded) return;
		try {
			if (text) localStorage.setItem(draftKey, text);
			else localStorage.removeItem(draftKey);
		} catch {
			/* Private browsing may deny storage. */
		}
	});

	async function send(event?: SubmitEvent) {
		event?.preventDefault();
		if (!canSend) return;
		sending = true;
		error = '';
		if (!pending || pending.content !== text)
			pending = { id: crypto.randomUUID(), content: text, conversation: pending?.conversation };
		try {
			const id =
				conversationId ?? pending.conversation ?? (await conversations.create(slug, text.trim().slice(0, 100))).id;
			pending.conversation = id;
			try {
				localStorage.setItem(`${draftKey}:pending`, JSON.stringify(pending));
			} catch {
				/* Sending still works without storage. */
			}
			const submitted = pending.content;
			const accepted = await conversations.send(slug, id, pending.id, submitted);
			if (disposed) return;
			chat.accept(accepted);
			if (text === submitted) text = '';
			pending = null;
			try {
				if (!text) localStorage.removeItem(draftKey);
				else if (!conversationId) localStorage.setItem(draftKey.replace(/:new$/, `:${id}`), text);
				localStorage.removeItem(`${draftKey}:pending`);
			} catch {
				/* Storage is optional. */
			}
			if (!conversationId) await goto(`/${slug}/assistant?conversation=${encodeURIComponent(id)}`);
			else await chat.load();
			await tick();
			textarea?.focus();
		} catch {
			if (!disposed) error = m['assistant.send_error']();
		} finally {
			sending = false;
		}
	}
	async function stop() {
		if (!conversationId) return;
		try {
			await conversations.cancel(slug, conversationId);
			await chat.load();
		} catch {
			error = m['assistant.error']();
		}
	}
	async function retry(message: string) {
		if (!conversationId || sending) return;
		sending = true;
		error = '';
		if (retryRequest?.message !== message) retryRequest = { message, id: crypto.randomUUID() };
		try {
			const accepted = await conversations.retry(slug, conversationId, message, retryRequest.id);
			chat.accept(accepted);
			retryRequest = null;
			await chat.load();
		} catch {
			error = m['assistant.error']();
		} finally {
			sending = false;
		}
	}
	async function copy(id: string, content: string) {
		try {
			await navigator.clipboard.writeText(content);
			copied = id;
			clearTimeout(copyTimer);
			copyTimer = setTimeout(() => (copied = null), 2000);
		} catch {
			error = m['assistant.error']();
		}
	}
	function statusLabel(value: MessageStatus) {
		switch (value) {
			case 'queued':
				return m['assistant.queued']();
			case 'streaming':
				return m['assistant.streaming']();
			case 'completed':
				return m['assistant.completed']();
			case 'failed':
				return m['assistant.failed']();
			case 'interrupted':
				return m['assistant.interrupted']();
			case 'cancelled':
				return m['assistant.cancelled']();
		}
	}
</script>

<svelte:head
	>{#if !embedded}<title>{m['assistant.title']()} · Kuayle</title>{/if}</svelte:head
>
<div class="flex h-full min-h-0 min-w-0 flex-1 flex-col bg-background text-foreground">
	<header class="flex h-14 shrink-0 items-center gap-3 border-b px-4 md:px-6">
		<MessageCircle size={19} class="text-muted-foreground" />
		<svelte:element this={embedded ? 'h3' : 'h1'} class="min-w-0 flex-1 truncate text-sm font-semibold"
			>{m['assistant.title']()}</svelte:element
		>
		{#if embedded}
			<Button
				variant="ghost"
				size="icon-sm"
				href="/{slug}/assistant?conversation={encodeURIComponent(conversationId ?? '')}"
				aria-label={m['assistant.open_full']()}><Maximize2 size={16} /></Button
			>
		{:else}
			<Button
				variant="ghost"
				size="icon-sm"
				class="md:hidden"
				aria-label={m['assistant.history']()}
				aria-expanded={showHistory}
				onclick={() => (showHistory = !showHistory)}><History size={17} /></Button
			>
			<Button variant="outline" size="sm" href="/{slug}/assistant"><Plus size={15} />{m['assistant.new']()}</Button>
		{/if}
	</header>
	{#snippet sidebar(inPane: boolean)}
			<aside
				class={inPane
					? 'flex h-full min-w-0 flex-col gap-3 overflow-y-auto bg-muted/20 p-3'
					: `max-h-56 shrink-0 flex-col gap-3 overflow-y-auto border-b bg-muted/20 p-3 md:flex md:max-h-none md:w-56 md:border-r md:border-b-0 ${showHistory ? 'flex' : 'hidden'}`}
				aria-label={m['assistant.history']()}
			>
				<p class="px-1 pt-1 text-xs font-medium text-muted-foreground">{m['assistant.history']()}</p>
				<Input
					bind:value={search}
					aria-label={m['assistant.search']()}
					placeholder={m['assistant.search']()}
					class="h-8"
				/>
				<ConversationNav {slug} {conversationId} bind:history {search} />
				<AgentQueue {slug} />
			</aside>
	{/snippet}
	{#snippet chatSection()}
		<section class="flex min-h-0 min-w-0 flex-1 flex-col" aria-label={m['assistant.conversation']()}>
			{#if conversationId && !chat.loading && !chat.unavailable}
				<div class="flex shrink-0 items-center gap-2 px-5 pt-3 text-xs text-muted-foreground">
					{#if chat.issue}<a
							class="shrink-0 underline underline-offset-2"
							href="/{slug}/issue/{encodeURIComponent(chat.issue.identifier)}">{chat.issue.identifier}</a
						>{/if}
					<span class="min-w-0 flex-1 truncate">{chat.issue?.title ?? chat.conversation?.title}</span>
					<LiveStatus connected={chat.connected} busy={chat.busy} offline={chat.failed || chat.unavailable} />
				</div>
			{/if}
			<div class="min-h-0 flex-1">
				<MessageScroller.Provider autoScroll>
					<MessageScroller.Root>
						<MessageScroller.Viewport aria-label={m['assistant.messages']()}>
							{#if chat.hasMore}<div class="mx-auto w-full max-w-3xl px-5 pt-3 md:px-8">
									<Button variant="ghost" class="w-full" disabled={chat.loadingOlder} onclick={() => void chat.older()}
										>{chat.loadingOlder ? m['assistant.loading']() : m['assistant.older']()}</Button
									>
								</div>{/if}
							<MessageScroller.Content class="mx-auto w-full max-w-3xl gap-8 px-5 py-6 md:px-8">
								{#if chat.issue && !chat.loading && !chat.unavailable}<TaskPanel {chat} />{/if}
								{#if chat.loading}
									<p class="text-sm text-muted-foreground" role="status">{m['assistant.loading']()}</p>
								{:else if chat.unavailable}
									<p role="alert" class="text-sm text-muted-foreground">{m['assistant.unavailable']()}</p>
								{:else if chat.messages.length === 0 && !Object.keys(chat.liveTasks).length}
									<div class="my-auto flex flex-col items-start gap-3 py-16">
										<div class="rounded-xl border bg-muted/40 p-3"><MessageCircle size={24} /></div>
										<h2 class="text-2xl font-semibold tracking-tight">{m['assistant.welcome']()}</h2>
										<p class="max-w-md text-sm leading-6 text-muted-foreground">{m['assistant.intro']()}</p>
										{#if status?.configured}
											<!-- Official AI Elements Suggestion composition: one click sends the prompt. -->
											<div class="flex max-w-full gap-2 overflow-x-auto pb-1">
												{#each suggestions as suggestion (suggestion)}
													<Button
														variant="outline"
														size="sm"
														class="shrink-0"
														disabled={sending}
														onclick={() => {
															text = suggestion;
															void send();
														}}>{suggestion}</Button
													>
												{/each}
											</div>
										{/if}
									</div>
								{/if}
								{#each chat.messages as message (message.id)}
									<MessageScroller.Item messageId={message.id}>
										<Message.Root align={message.role === 'user' ? 'end' : 'start'}>
											<Message.Content>
												<Message.Header
													>{message.role === 'user'
														? m['assistant.you']()
														: m['assistant.title']()}{#if message.model}<span class="ml-2 font-normal"
															>· {message.model}</span
														>{/if}</Message.Header
												>
												{#if message.role === 'assistant'}<ToolCalls
														calls={chat.tools[message.id] ?? []}
														slug={chat.slug}
														approvals={chat.approvals}
														onresolve={(approval, decision) => chat.resolveApproval(approval, decision)}
													/><ToolSources calls={chat.tools[message.id] ?? []} slug={chat.slug} />{/if}
												<Bubble.Root variant={message.role === 'user' ? 'muted' : 'ghost'}>
													<Bubble.Content>
														{#if message.role === 'user'}<p class="whitespace-pre-wrap break-words">
																{message.content}
															</p>
														{:else}<Markdown source={message.content} />{/if}
													</Bubble.Content>
												</Bubble.Root>
												<Message.Footer class="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
													<time datetime={message.created_at}
														>{new Date(message.created_at).toLocaleTimeString([], {
															hour: '2-digit',
															minute: '2-digit'
														})}</time
													>
													{#if message.role === 'assistant'}<span
															role={message.status === 'streaming' ? 'status' : undefined}
															>{statusLabel(message.status)}</span
														>{/if}
													{#if message.content}<Button
															variant="ghost"
															size="icon-sm"
															aria-label={m['assistant.copy']()}
															onclick={() => void copy(message.id, message.content)}
															>{#if copied === message.id}<Check size={13} />{:else}<Copy size={13} />{/if}</Button
														>{/if}
													{#if ['failed', 'interrupted', 'cancelled'].includes(message.status) && message.id === chat.messages.at(-1)?.id}
														<Button variant="ghost" size="sm" disabled={sending} onclick={() => void retry(message.id)}
															><RefreshCw size={13} />{m['assistant.retry']()}</Button
														>
													{/if}
												</Message.Footer>
											</Message.Content>
										</Message.Root>
									</MessageScroller.Item>
								{/each}
							</MessageScroller.Content>
						</MessageScroller.Viewport>
						<MessageScroller.Button aria-label={m['assistant.latest']()}
							><ArrowDown size={16} /><span class="sr-only">{m['assistant.latest']()}</span></MessageScroller.Button
						>
					</MessageScroller.Root>
				</MessageScroller.Provider>
			</div>
			<div class="mx-auto w-full max-w-3xl shrink-0 px-4 pt-2 pb-4 md:px-8 md:pb-6">
				{#if error || chat.failed}<p role="alert" class="mb-2 text-sm text-destructive">
						{error || m['assistant.error']()}
					</p>{/if}
				{#if initialized && !status.configured}<p class="mb-3 text-sm text-muted-foreground">
						{m['assistant.configure']()} <a class="underline" href="/{slug}/settings/ai">{m['assistant.settings']()}</a>
					</p>{/if}
				<form onsubmit={send}>
					<InputGroup.Root>
						<InputGroup.Textarea
							bind:ref={textarea}
							bind:value={text}
							aria-label={m['assistant.message']()}
							placeholder={m['assistant.placeholder']()}
							maxlength={16000}
							disabled={!initialized || !status.configured || chat.unavailable}
							class="max-h-48 min-h-10"
							onkeydown={(event) => {
								if (event.key === 'Enter' && !event.shiftKey && !event.isComposing) {
									event.preventDefault();
									void send();
								}
							}}
						/>
						<InputGroup.Addon align="block-end" class="justify-between">
							<span class="truncate text-xs text-muted-foreground">{status.model ?? m['assistant.no_provider']()}</span>
							{#if chat.busy}<InputGroup.Button
									size="icon-sm"
									aria-label={m['assistant.stop']()}
									onclick={() => void stop()}><Square size={14} /></InputGroup.Button
								>
							{:else}<InputGroup.Button
									type="submit"
									variant="default"
									size="icon-sm"
									disabled={!canSend}
									aria-label={m['assistant.send']()}><ArrowUp size={16} /></InputGroup.Button
								>{/if}
						</InputGroup.Addon>
					</InputGroup.Root>
					<p class="mt-2 text-center text-[11px] text-muted-foreground">
						{sending ? m['assistant.sending']() : m['assistant.hint']()}
					</p>
				</form>
			</div>
		</section>
	{/snippet}
	{#snippet conversationPane()}
		{#if chat.diffView && wide && !embedded}
			<!-- The diff opens beside the chat; the chat keeps the left side. -->
			<Resizable.PaneGroup direction="horizontal" autoSaveId="kuayle-assistant-diff" class="min-h-0 flex-1">
				<Resizable.Pane minSize={30} class="flex min-w-0">{@render chatSection()}</Resizable.Pane>
				<Resizable.Handle withHandle aria-label={m['tasks.diff_resize']()} />
				<Resizable.Pane defaultSize={45} minSize={25} class="min-w-0 border-l"
					><DiffPanel
						slug={chat.slug}
						taskId={chat.diffView.taskId}
						runId={chat.diffView.runId}
						title={chat.diffView.title}
						onclose={() => (chat.diffView = null)}
					/></Resizable.Pane
				>
			</Resizable.PaneGroup>
		{:else}
			{@render chatSection()}
			<Sheet.Root open={!!chat.diffView} onOpenChange={(open) => !open && (chat.diffView = null)}>
				<Sheet.Content side="right" class="w-full p-0 sm:max-w-2xl">
					{#if chat.diffView}<DiffPanel
							slug={chat.slug}
							taskId={chat.diffView.taskId}
							runId={chat.diffView.runId}
							title={chat.diffView.title}
							onclose={() => (chat.diffView = null)}
						/>{/if}
				</Sheet.Content>
			</Sheet.Root>
		{/if}
	{/snippet}
	<div class="flex min-h-0 min-w-0 flex-1 flex-col md:flex-row">
		{#if !embedded && wide}
			<!-- Desktop: the conversation list is resizable; its width is remembered. -->
			<Resizable.PaneGroup direction="horizontal" autoSaveId="kuayle-assistant-sidebar" class="min-h-0 flex-1">
				<Resizable.Pane defaultSize={20} minSize={12} maxSize={40} class="min-w-0">{@render sidebar(true)}</Resizable.Pane>
				<Resizable.Handle withHandle aria-label={m['assistant.resize_sidebar']()} />
				<Resizable.Pane minSize={40} class="flex min-w-0">{@render conversationPane()}</Resizable.Pane>
			</Resizable.PaneGroup>
		{:else}
			{#if !embedded}{@render sidebar(false)}{/if}
			{@render conversationPane()}
		{/if}
	</div>
</div>
