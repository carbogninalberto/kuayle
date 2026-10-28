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
		Maximize2,
		ImagePlus
	} from '@lucide/svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import * as Message from '$lib/components/ui/message/index.js';
	import * as Bubble from '$lib/components/ui/bubble/index.js';
	import * as MessageScroller from '$lib/components/ui/message-scroller/index.js';
	import * as InputGroup from '$lib/components/ui/input-group/index.js';
	import { conversations, type Conversation, type AssistantStatus, type MessageStatus } from '$lib/api/conversations';
	import { ConversationState } from './conversation-state.svelte';
	import { ComposerAttachments } from './composer-attachments.svelte';
	import ComposerAttachmentList from './ComposerAttachments.svelte';
	import ComposerVoice from './ComposerVoice.svelte';
	import { appendTranscript } from '$lib/features/voice/voice-rules';
	import MessageAttachments from './MessageAttachments.svelte';
	import { ATTACHMENT_TYPES } from './attachment-rules';
	import TaskPanel from './TaskPanel.svelte';
	import ToolCalls from './ToolCalls.svelte';
	import AgentQueue from './AgentQueue.svelte';
	import ToolSources from './ToolSources.svelte';
	import Markdown from './Markdown.svelte';
	import ConversationNav from './ConversationNav.svelte';
	import LiveStatus from './LiveStatus.svelte';
	import DiffPanel from './DiffPanel.svelte';
	import * as Sheet from '$lib/components/ui/sheet/index.js';
	import * as Select from '$lib/components/ui/select/index.js';
	import { listProjects } from '$lib/api/projects';
	import type { Project } from '$lib/types/project';
	import { listQuery, newChatProject, resolveProjectFilter, type ProjectFilter } from './project-filter';
	import * as Resizable from '$lib/components/ui/resizable/index.js';
	import { MediaQuery } from 'svelte/reactivity';
	import { authState } from '$lib/features/auth/auth.state.svelte';
	import { appToast } from '$lib/features/toast/toast';
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
	// The list's project filter is remembered per person and workspace; a new
	// chat started under a project filter belongs to that project.
	const filterKey = untrack(() => `assistant-project-filter:${authState.user?.id}:${slug}`);
	let projects = $state<Project[]>([]);
	let projectFilter = $state<ProjectFilter>(
		untrack(() => {
			try {
				return (localStorage.getItem(filterKey) as ProjectFilter | null) ?? 'all';
			} catch {
				return 'all';
			}
		})
	);
	const projectOptions = $derived([
		{ value: 'all', label: m['assistant.all_projects']() },
		{ value: 'none', label: m['assistant.no_project']() },
		...projects.map((project) => ({ value: project.id, label: project.name }))
	]);
	function loadHistory(filter: ProjectFilter) {
		return conversations.list(slug, listQuery(filter)).then((items) => {
			if (!disposed && projectFilter === filter) history = items;
		});
	}
	function chooseProject(next: string) {
		if (next === projectFilter) return;
		projectFilter = next;
		try {
			if (next === 'all') localStorage.removeItem(filterKey);
			else localStorage.setItem(filterKey, next);
		} catch {
			/* The filter still applies without storage. */
		}
		void loadHistory(next).catch((error) => appToast.apiError(error, m['assistant.error']()));
	}
	let showHistory = $state(false);
	// Panes only on wide screens; phones keep the stacked, toggleable list.
	const desktop = new MediaQuery('min-width: 768px');
	const wide = $derived(desktop.current);
	let sending = $state(false);
	let error = $state('');
	let copied = $state<string | null>(null);
	let textarea = $state<HTMLTextAreaElement | null>(null);
	let draftLoaded = $state(false);
	let pending: { id: string; content: string; attachments: string[]; conversation?: string } | null = null;
	// A new chat is created when its first image is attached, so the image can
	// be uploaded to it before the first message is sent.
	let draftConversation: string | null = null;
	let fileInput = $state<HTMLInputElement | null>(null);
	let dragging = $state(false);
	async function ensureConversation(fallbackTitle = m['assistant.image_title']()) {
		const existing = conversationId ?? pending?.conversation ?? draftConversation;
		if (existing) return existing;
		draftConversation = (
			await conversations.create(slug, text.trim().slice(0, 100) || fallbackTitle, newChatProject(projectFilter))
		).id;
		return draftConversation;
	}
	const attachments = untrack(() => new ComposerAttachments(slug, () => ensureConversation()));
	const sameIds = (a: string[], b: string[]) => a.length === b.length && a.every((id, i) => id === b[i]);
	function imageFiles(list: FileList | null | undefined) {
		return Array.from(list ?? []).filter((file) => file.type.startsWith('image/'));
	}
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
			(text.trim().length > 0 || attachments.ids.length > 0) &&
			new TextEncoder().encode(text).length <= 16384 &&
			!attachments.uploading &&
			!attachments.failed &&
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
			// Images are not restored after a reload, so only a text-only send resumes.
			if (saved && typeof saved.id === 'string' && saved.content === text && !saved.attachments?.length)
				pending = { ...saved, attachments: [] };
		} catch {
			/* Storage can be disabled. */
		}
		draftLoaded = true;
		const saved = projectFilter;
		void Promise.all([
			embedded ? Promise.resolve([]) : conversations.list(slug, listQuery(saved)),
			conversations.status(slug),
			embedded ? Promise.resolve([]) : listProjects(slug).catch(() => [])
		])
			.then(([items, provider, visible]) => {
				if (disposed) return;
				history = items;
				status = provider;
				projects = visible;
				initialized = true;
				// A remembered project that is no longer visible falls back to all.
				const resolved = resolveProjectFilter(saved, visible);
				if (!embedded && resolved !== saved) chooseProject(resolved);
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
	// A task card can ask for a message, such as a review request; the user
	// still reads, edits and sends it.
	$effect(() => {
		const draft = chat.composerDraft;
		if (draft === null) return;
		untrack(() => {
			chat.composerDraft = null;
			text = draft;
			void tick().then(() => textarea?.focus());
		});
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
		const ids = attachments.ids;
		if (!pending || pending.content !== text || !sameIds(pending.attachments, ids))
			pending = {
				id: crypto.randomUUID(),
				content: text,
				attachments: ids,
				conversation: pending?.conversation ?? draftConversation ?? undefined
			};
		try {
			const id =
				conversationId ??
				pending.conversation ??
				(
					await conversations.create(
						slug,
						text.trim().slice(0, 100) || m['assistant.image_title'](),
						newChatProject(projectFilter)
					)
				).id;
			pending.conversation = id;
			try {
				localStorage.setItem(`${draftKey}:pending`, JSON.stringify(pending));
			} catch {
				/* Sending still works without storage. */
			}
			const submitted = pending.content;
			const accepted = await conversations.send(slug, id, pending.id, submitted, pending.attachments);
			if (disposed) return;
			chat.accept(accepted);
			if (text === submitted) text = '';
			if (sameIds(attachments.ids, pending.attachments)) attachments.clear();
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
		<!-- relative: visually hidden (absolute) labels inside the list are
			clipped by this scroller instead of stretching the page. -->
		<aside
			class={inPane
				? 'relative flex h-full min-w-0 flex-col gap-3 overflow-y-auto bg-muted/20 p-3'
				: `relative max-h-56 shrink-0 flex-col gap-3 overflow-y-auto border-b bg-muted/20 p-3 md:flex md:max-h-none md:w-56 md:border-r md:border-b-0 ${showHistory ? 'flex' : 'hidden'}`}
			aria-label={m['assistant.history']()}
		>
			<p class="px-1 pt-1 text-xs font-medium text-muted-foreground">{m['assistant.history']()}</p>
			{#if projects.length > 0 || projectFilter !== 'all'}
				<Select.Root type="single" value={projectFilter} onValueChange={chooseProject} items={projectOptions}>
					<Select.Trigger class="w-full" aria-label={m['assistant.project_filter']()}>
						<span class="truncate">{projectOptions.find((o) => o.value === projectFilter)?.label ?? ''}</span>
					</Select.Trigger>
					<Select.Content>
						{#each projectOptions as option (option.value)}
							<Select.Item value={option.value} label={option.label}>{option.label}</Select.Item>
						{/each}
					</Select.Content>
				</Select.Root>
			{/if}
			<Input
				bind:value={search}
				aria-label={m['assistant.search']()}
				placeholder={m['assistant.search']()}
				class="h-8"
			/>
			<ConversationNav
				{slug}
				{conversationId}
				bind:history
				{search}
				{projects}
				{projectFilter}
				onrenamed={(updated) => {
					if (chat.conversation?.id === updated.id) chat.conversation = { ...chat.conversation, title: updated.title };
				}}
			/>
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
												{#if message.attachments?.length}<MessageAttachments
														{slug}
														conversationId={message.conversation_id}
														attachments={message.attachments}
													/>{/if}
												{#if message.role !== 'user' || message.content}
													<Bubble.Root variant={message.role === 'user' ? 'muted' : 'ghost'}>
														<Bubble.Content>
															{#if message.role === 'user'}<p class="whitespace-pre-wrap break-words">
																	{message.content}
																</p>
															{:else}<Markdown source={message.content} />{/if}
														</Bubble.Content>
													</Bubble.Root>
												{/if}
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
				{#if attachments.notice}<p role="alert" class="mb-2 text-sm text-destructive">{attachments.notice}</p>{/if}
				<form
					onsubmit={send}
					aria-label={m['assistant.composer']()}
					ondragover={(event) => {
						if (!status.configured || chat.unavailable || !event.dataTransfer?.types.includes('Files')) return;
						event.preventDefault();
						dragging = true;
					}}
					ondragleave={(event) => {
						if (!(event.currentTarget as HTMLElement).contains(event.relatedTarget as Node | null)) dragging = false;
					}}
					ondrop={(event) => {
						dragging = false;
						const files = imageFiles(event.dataTransfer?.files);
						if (!files.length) return;
						event.preventDefault();
						attachments.add(files);
					}}
				>
					<input
						bind:this={fileInput}
						type="file"
						accept={ATTACHMENT_TYPES.join(',')}
						multiple
						class="sr-only"
						tabindex="-1"
						aria-hidden="true"
						onchange={(event) => {
							const input = event.currentTarget as HTMLInputElement;
							attachments.add(Array.from(input.files ?? []));
							input.value = '';
						}}
					/>
					<InputGroup.Root class={dragging ? 'ring-2 ring-ring' : ''}>
						{#if attachments.items.length}<InputGroup.Addon align="block-start"
								><ComposerAttachmentList {attachments} /></InputGroup.Addon
							>{/if}
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
							onpaste={(event) => {
								const files = imageFiles(event.clipboardData?.files);
								if (!files.length) return;
								event.preventDefault();
								attachments.add(files);
							}}
						/>
						<InputGroup.Addon align="block-end" class="justify-between">
							<div class="flex min-w-0 items-center gap-1">
								<InputGroup.Button
									size="icon-sm"
									aria-label={m['assistant.attach']()}
									disabled={!initialized || !status.configured || chat.unavailable}
									onclick={() => fileInput?.click()}><ImagePlus size={16} /></InputGroup.Button
								>
								<ComposerVoice
									{slug}
									ensureConversation={() => ensureConversation(m['voice.conversation_title']())}
									disabled={!initialized || !status.configured || chat.unavailable}
									ontranscript={(transcript) => {
										text = appendTranscript(text, transcript);
										textarea?.focus();
									}}
								/>
								<span class="truncate text-xs text-muted-foreground"
									>{status.model ?? m['assistant.no_provider']()}</span
								>
							</div>
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
						{sending
							? m['assistant.sending']()
							: attachments.uploading
								? m['assistant.attachment_wait']()
								: dragging
									? m['assistant.attachment_drop']()
									: m['assistant.hint']()}
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
				<Resizable.Pane defaultSize={20} minSize={12} maxSize={40} class="min-w-0"
					>{@render sidebar(true)}</Resizable.Pane
				>
				<Resizable.Handle withHandle aria-label={m['assistant.resize_sidebar']()} />
				<Resizable.Pane minSize={40} class="flex min-w-0">{@render conversationPane()}</Resizable.Pane>
			</Resizable.PaneGroup>
		{:else}
			{#if !embedded}{@render sidebar(false)}{/if}
			{@render conversationPane()}
		{/if}
	</div>
</div>
