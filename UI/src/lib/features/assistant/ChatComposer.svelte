<script lang="ts">
	import { onMount, tick, untrack } from 'svelte';
	import { goto } from '$app/navigation';
	import {
		ArrowUp,
		Bot,
		ChevronDown,
		CornerDownRight,
		MessageCircle,
		Play,
		Plus,
		Square,
		X,
		Zap
	} from '@lucide/svelte';
	import { conversations, type AssistantStatus } from '$lib/api/conversations';
	import { agentTasks, type AgentTask, type LiveSendMode, type MergedPullRequest } from '$lib/api/agent-tasks';
	import { authState } from '$lib/features/auth/auth.state.svelte';
	import { agentInfo } from '$lib/features/agents/agents';
	import { getErrorMessage } from '$lib/features/toast/toast';
	import { m } from '$lib/features/i18n/messages';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import type { ConversationState } from './conversation-state.svelte';
	import { assistantDraftKey } from './chat-draft';
	import { Input } from '$lib/components/ui/input/index.js';
	import * as InputGroup from '$lib/components/ui/input-group/index.js';
	import * as AlertDialog from '$lib/components/ui/alert-dialog/index.js';
	import * as Tooltip from '$lib/components/ui/tooltip/index.js';
	import { Kbd, KbdGroup } from '$lib/components/ui/kbd/index.js';
	import MergedWorkDialog from './MergedWorkDialog.svelte';
	import { mergedWork } from './merged-work';
	import { ComposerAttachments } from './composer-attachments.svelte';
	import ComposerAttachmentList from './ComposerAttachments.svelte';
	import ComposerVoice from './ComposerVoice.svelte';
	import ComposerContext from './ComposerContext.svelte';
	import { appendTranscript } from '$lib/features/voice/voice-rules';
	import { ATTACHMENT_TYPES } from './attachment-rules';
	import ApprovalStack from './ApprovalStack.svelte';
	import MentionList from '$lib/components/shared/mention/MentionList.svelte';
	import type { MentionItem } from '$lib/components/shared/mention/mention.extension';
	import { searchWikiReferences } from '$lib/api/wiki';
	import { wikiReferences } from '$lib/features/wiki/wiki-reference-store';
	import {
		chatReferenceToken,
		isWikiPageDrag,
		mentionQueryAt,
		mentionQueryMayMatch,
		wikiPageDragData,
		type WikiReference
	} from '$lib/features/wiki/wiki-references';
	import { answerTo, sendOptions } from './agent-conversation';

	let {
		chat,
		conversationId,
		status,
		initialized,
		targetTask,
		messageable,
		chooseTarget,
		reveal,
		followLatest,
		sending = $bindable(false),
		error = $bindable(''),
		wide = false
	}: {
		chat: ConversationState;
		conversationId: string | null;
		status: AssistantStatus;
		initialized: boolean;
		targetTask?: AgentTask;
		messageable: AgentTask[];
		chooseTarget: (task: string | null) => void;
		reveal: () => void;
		followLatest: () => void;
		sending?: boolean;
		error?: string;
		// As wide as the column instead of the reading width (a task's room
		// with the side panel closed).
		wide?: boolean;
	} = $props();
	const slug = $derived(chat.slug);
	const draftKey = untrack(() => assistantDraftKey(authState.user?.id, slug, conversationId));
	let text = $state('');
	let notice = $state('');
	let textarea = $state<HTMLTextAreaElement | null>(null);
	const options = $derived(targetTask && sendOptions(targetTask, authState.user?.id));
	const toAgent = $derived(!!options?.open);
	// A task on a pipeline is written to in its room (KUA-252), which
	// everyone working on it reads: text only, and no "now" to send it with.
	// The task says it runs one; a room already loaded says so too.
	const room = $derived(toAgent && !!targetTask && (!!targetTask.pipeline || !!chat.taskRooms[targetTask.id]));
	const now = $derived(room ? null : (options?.now ?? null));
	const question = $derived(
		!room && targetTask && chat.agentQuestion?.taskId === targetTask.id ? chat.agentQuestion : null
	);
	const agentBody = $derived(question ? answerTo(question.body, text.trim()) : text.trim());
	let confirming = $state(false),
		merged = $state<MergedPullRequest | null>(null);
	const mod = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform) ? '⌘' : 'Ctrl';
	const nowLabel = $derived(now === 'interrupt' ? m['tasks.live_interrupt_send']() : m['tasks.live_start_send']());
	const agentName = (task: AgentTask) => agentInfo(task.provider).name;
	const targetLabel = $derived(
		room
			? m['tasks.room_target']({ identifier: chat.issue?.identifier ?? '' })
			: toAgent && targetTask
				? m['assistant.target_agent']({ identifier: chat.issue?.identifier ?? '', agent: agentName(targetTask) })
				: m['assistant.target_assistant']()
	);
	$effect(() => {
		void chat.composerFocus;
		if (!chat.composerFocus) return;
		untrack(() => {
			// On narrow screens the panel is a drawer over the composer.
			reveal();
			void tick().then(() => textarea?.focus());
		});
	});
	let draftLoaded = $state(false);
	let pending: { id: string; content: string; attachments: string[]; conversation?: string } | null = null;
	// A new chat is created when its first image is attached, so the image can
	// be uploaded to it before the first message is sent.
	let draftConversation: string | null = null;
	let creating: Promise<string> | null = null;
	let fileInput = $state<HTMLInputElement | null>(null);
	let dragging = $state(false);

	// Wiki page references. The composer is plain text, so a page chosen with
	// "@" or dragged in from a wiki tree goes in as a token naming the page;
	// sent messages show it as the usual reference chip.
	let pageMention = $state<{ from: number; query: string } | null>(null);
	let pageSuggestions = $state<WikiReference[]>([]);
	let pageIndex = $state(0);
	let pageMenu = $state({ x: 0, y: 0, height: 0 });
	const pageItems = $derived<MentionItem[]>(
		pageSuggestions.map((page) => ({
			kind: 'wiki',
			id: page.id,
			title: page.title ?? '',
			scope: page.scope_name ?? ''
		}))
	);
	function trackPageMention() {
		const found = textarea ? mentionQueryAt(textarea.value, textarea.selectionStart) : null;
		if (found && textarea && !pageMention) {
			const rect = textarea.getBoundingClientRect();
			pageMenu = { x: rect.left, y: rect.bottom + 4, height: rect.height };
		}
		if (found?.query !== pageMention?.query) pageIndex = 0;
		pageMention = found;
	}
	// Queries of the "@" being typed that found no page: text that continues
	// one of them is ordinary prose after an "@", not a search.
	let emptyPageQueries: { from: number; queries: string[] } | null = null;
	$effect(() => {
		const mention = pageMention;
		if (!mention) {
			pageSuggestions = [];
			emptyPageQueries = null;
			return;
		}
		if (emptyPageQueries?.from !== mention.from) emptyPageQueries = { from: mention.from, queries: [] };
		const empty = emptyPageQueries.queries;
		if (!mentionQueryMayMatch(mention.query.trim(), empty)) {
			pageSuggestions = [];
			return;
		}
		let stale = false;
		const timer = setTimeout(
			() =>
				void searchWikiReferences(slug, mention.query)
					.then((found) => {
						if (found.length === 0) empty.push(mention.query.trim());
						if (!stale) pageSuggestions = found;
					})
					.catch(() => {
						if (!stale) pageSuggestions = [];
					}),
			mention.query ? 120 : 0
		);
		return () => {
			stale = true;
			clearTimeout(timer);
		};
	});
	/** Puts a page reference in the draft, replacing [from, to) of it. */
	function insertPageReference(reference: WikiReference, from: number, to: number) {
		const token = chatReferenceToken(reference);
		const before = text.slice(0, from);
		const lead = before && !/\s$/.test(before) ? ' ' : '';
		text = `${before}${lead}${token} ${text.slice(to).replace(/^ /, '')}`;
		const caret = before.length + lead.length + token.length + 1;
		pageMention = null;
		void tick().then(() => {
			textarea?.focus();
			textarea?.setSelectionRange(caret, caret);
		});
	}
	function choosePage(item: MentionItem | undefined) {
		const reference = pageSuggestions.find((page) => page.id === item?.id);
		if (!reference || !pageMention || !textarea) return;
		insertPageReference(reference, pageMention.from, textarea.selectionStart);
	}
	async function dropPage(pageId: string) {
		const reference = await wikiReferences.resolve(slug, pageId);
		if (!reference.accessible) return;
		const at = textarea?.selectionStart ?? text.length;
		insertPageReference(reference, at, at);
	}

	async function ensureConversation(fallbackTitle = m['assistant.image_title']()) {
		const existing = conversationId ?? pending?.conversation ?? draftConversation;
		if (existing) return existing;
		creating ??= conversations
			.create(slug, text.trim().slice(0, 100) || fallbackTitle)
			.then((chat) => (draftConversation = chat.id))
			.finally(() => {
				creating = null;
			});
		return creating;
	}
	const attachments = untrack(() => new ComposerAttachments(slug, () => ensureConversation()));
	const sameIds = (a: string[], b: string[]) => a.length === b.length && a.every((id, i) => id === b[i]);
	function imageFiles(list: FileList | null | undefined) {
		return Array.from(list ?? []).filter((file) => file.type.startsWith('image/'));
	}
	const composerDisabled = $derived(chat.unavailable || (!toAgent && (!initialized || !status.configured)));
	// The room takes no attachments.
	const attachable = $derived(!composerDisabled && !room);
	const agentTooLong = $derived(
		toAgent && (room ? new TextEncoder().encode(agentBody).length > 16384 : [...agentBody].length > 4000)
	);
	const canSend = $derived(
		(!!text.trim() || (!room && attachments.ids.length > 0)) &&
			!attachments.uploading &&
			!attachments.failed &&
			!sending &&
			!chat.unavailable &&
			(toAgent
				? !agentTooLong
				: initialized &&
					status.configured &&
					!chat.busy &&
					!chat.loading &&
					!chat.failed &&
					new TextEncoder().encode(text).length <= 16384)
	);

	let disposed = false;
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
		return () => {
			disposed = true;
			attachments.dispose();
		};
	});
	// A view can ask for a message, such as a review request from an agent's
	// view; the user still reads, edits and sends it — to the assistant. The
	// composer takes focus once it is on screen again.
	let draftFocus = $state(false);
	$effect(() => {
		const draft = chat.composerDraft;
		if (draft === null) return;
		untrack(() => {
			chat.composerDraft = null;
			chat.composerTarget = null;
			text = draft;
			reveal();
			draftFocus = true;
		});
	});
	$effect(() => {
		if (!draftFocus || !textarea?.isConnected) return;
		const target = textarea;
		untrack(() => (draftFocus = false));
		void tick().then(() => target.focus());
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

	const targetId = $derived(targetTask?.id);
	$effect(() => {
		void targetId;
		confirming = false;
		merged = null;
		notice = '';
	});
	function sendNow() {
		if (!canSend || !now || !targetTask) return;
		if (options?.confirm) confirming = true;
		else void sendToAgent(targetTask, now);
	}
	// A post sent again after it failed keeps its request id while its text
	// is the same, so the room keeps one of them.
	let roomRequest: { body: string; id: string } | null = null;
	async function postToRoom(task: AgentTask) {
		if (!canSend) return;
		const typed = text,
			body = agentBody;
		if (roomRequest?.body !== body) roomRequest = { body, id: crypto.randomUUID() };
		const request = roomRequest.id;
		sending = true;
		error = '';
		notice = '';
		followLatest();
		try {
			const message = await agentTasks.postRoom(slug, task.id, { kind: 'say', body, request_id: request });
			chat.acceptRoomMessages(task.id, [message]);
			if (roomRequest?.id === request) roomRequest = null;
			if (disposed) return;
			if (text === typed) text = '';
			if (targetTask?.id !== task.id) return;
			notice = m['tasks.room_sent']({ identifier: chat.issue?.identifier ?? '' });
			await tick();
			textarea?.focus();
		} catch (e) {
			if (disposed || targetTask?.id !== task.id) return;
			error = getErrorMessage(e, m['tasks.live_send_error']());
		} finally {
			sending = false;
		}
	}
	async function sendToAgent(task: AgentTask, mode?: LiveSendMode, confirmMerged = false) {
		confirming = false;
		if (!canSend) return;
		const typed = text,
			ids = attachments.ids,
			answered = question;
		sending = true;
		error = '';
		notice = '';
		try {
			const message = await agentTasks.sendLiveMessage(slug, task.id, agentBody, mode, confirmMerged, ids);
			chat.recordAgentSend(message);
			if (disposed) return;
			if (text === typed) text = '';
			attachments.consume(ids);
			if (answered && chat.agentQuestion?.id === answered.id) chat.agentQuestion = null;
			if (mode) {
				// Delivery has succeeded even if refreshing the task state fails.
				void agentTasks
					.get(slug, task.id)
					.then((updated) => {
						if (!disposed) chat.acceptTask(updated, false, chat.liveTasks[task.id] ?? null);
					})
					.catch(() => {});
			}
			if (targetTask?.id !== task.id) return;
			notice = m['assistant.sent_to_agent']({ identifier: chat.issue?.identifier ?? '', agent: agentName(task) });
			await tick();
			textarea?.focus();
		} catch (e) {
			if (disposed || targetTask?.id !== task.id) return;
			const refused = mode === 'start' ? mergedWork(e) : null;
			if (refused) merged = refused;
			else error = getErrorMessage(e, m['tasks.live_send_error']());
		} finally {
			sending = false;
		}
	}

	async function send(event?: SubmitEvent) {
		event?.preventDefault();
		if (!canSend) return;
		if (toAgent && targetTask) return room ? postToRoom(targetTask) : sendToAgent(targetTask);
		sending = true;
		error = '';
		notice = '';
		followLatest();
		const ids = attachments.ids;
		if (!pending || pending.content !== text || !sameIds(pending.attachments, ids))
			pending = {
				id: crypto.randomUUID(),
				content: text,
				attachments: ids,
				conversation: pending?.conversation ?? draftConversation ?? undefined
			};
		try {
			const id = await ensureConversation();
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
			attachments.consume(pending.attachments);
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

	export function suggest(value: string) {
		chooseTarget(null);
		text = value;
		void send();
	}
</script>

<div class="shrink-0 px-3 pt-1 pb-[calc(0.75rem+env(safe-area-inset-bottom))] max-md:max-h-[calc(100%-3rem)] max-md:overflow-y-auto md:px-6 md:pb-[calc(1rem+env(safe-area-inset-bottom))]" data-testid={toAgent ? 'agent-composer' : 'assistant-composer'}>
	<div class="mx-auto w-full {wide ? '' : 'max-w-3xl'}">
		{#if error || chat.failed}<p role="alert" class="mb-2 text-sm text-destructive">
				{error || m['assistant.error']()}
			</p>{/if}
		{#if initialized && !status.configured && !toAgent}<p class="mb-3 text-sm text-muted-foreground">
				{m['assistant.configure']()} <a class="underline" href="/{slug}/settings/ai">{m['assistant.settings']()}</a>
			</p>{/if}
		{#if attachments.notice}<p role="alert" class="mb-2 text-sm text-destructive">{attachments.notice}</p>{/if}
		<ApprovalStack {chat} focusComposer={() => textarea?.focus({ preventScroll: true })} />
		<form
			onsubmit={send}
			aria-label={m['assistant.composer']()}
			ondragover={(event) => {
				if (isWikiPageDrag(event.dataTransfer) && !chat.unavailable) {
					event.preventDefault();
					dragging = true;
					return;
				}
				if (!attachable || !event.dataTransfer?.types.includes('Files')) return;
				event.preventDefault();
				dragging = true;
			}}
			ondragleave={(event) => {
				if (!(event.currentTarget as HTMLElement).contains(event.relatedTarget as Node | null)) dragging = false;
			}}
			ondrop={(event) => {
				dragging = false;
				const pageId = wikiPageDragData(event.dataTransfer);
				if (pageId) {
					event.preventDefault();
					if (!chat.unavailable) void dropPage(pageId);
					return;
				}

				const files = imageFiles(event.dataTransfer?.files);
				if (!attachable || !files.length) return;
				event.preventDefault();
				attachments.add(files);
			}}
		>
			<Input
				variant="plain"
				bind:ref={fileInput}
				type="file"
				accept={ATTACHMENT_TYPES.join(',')}
				multiple
				class="sr-only"
				tabindex={-1}
				aria-hidden="true"
				onchange={(event) => {
					const input = event.currentTarget as HTMLInputElement;
					attachments.add(Array.from(input.files ?? []));
					input.value = '';
				}}
			/>
			<InputGroup.Root
				class="rounded-2xl bg-background shadow-sm {composerDisabled
					? ''
					: 'has-disabled:bg-background has-disabled:opacity-100 dark:has-disabled:bg-background'} {dragging
					? 'ring-2 ring-ring'
					: ''}"
			>
				{#if question}<InputGroup.Addon
						align="block-start"
						class="gap-1.5 text-xs font-normal"
						data-testid="agent-question"
					>
						<CornerDownRight size={13} class="shrink-0" aria-hidden="true" />
						<span class="min-w-0 flex-1 truncate">{question.body}</span>
						<InputGroup.Button
							size="icon-xs"
							aria-label={m['common.dismiss']()}
							onclick={() => (chat.agentQuestion = null)}><X /></InputGroup.Button
						>
					</InputGroup.Addon>{/if}
				{#if attachments.items.length}<InputGroup.Addon align="block-start"
						><ComposerAttachmentList {attachments} /></InputGroup.Addon
					>{/if}
				<InputGroup.Textarea
					bind:ref={textarea}
					bind:value={text}
					aria-label={room ? m['tasks.room_composer']() : toAgent ? m['tasks.live_composer']() : m['assistant.message']()}
					aria-invalid={agentTooLong || undefined}
					placeholder={room
						? m['tasks.room_placeholder']({ identifier: chat.issue?.identifier ?? '' })
						: toAgent && targetTask
							? m['assistant.placeholder_agent']({
									identifier: chat.issue?.identifier ?? '',
									agent: agentName(targetTask)
								})
							: m['assistant.placeholder']()}
					maxlength={16000}
					disabled={composerDisabled}
					class="max-h-48 min-h-11"
					oninput={trackPageMention}
					onclick={trackPageMention}
					onkeyup={(event) => {
						if (event.key.startsWith('Arrow') && !(pageMention && pageItems.length)) trackPageMention();
					}}
					onblur={() => (pageMention = null)}
					onkeydown={(event) => {
						// The page menu takes the keys the rich editor's mention menu takes.
						if (pageMention && pageItems.length && !event.isComposing) {
							if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
								event.preventDefault();
								pageIndex = (pageIndex + (event.key === 'ArrowDown' ? 1 : pageItems.length - 1)) % pageItems.length;
								return;
							}
							if (event.key === 'Enter' || event.key === 'Tab') {
								event.preventDefault();
								choosePage(pageItems[pageIndex]);
								return;
							}
							if (event.key === 'Escape') {
								event.preventDefault();
								pageMention = null;
								return;
							}
						}
						if (event.key === 'Enter' && !event.shiftKey && !event.isComposing) {
							event.preventDefault();
							if ((event.metaKey || event.ctrlKey) && now) sendNow();
							else void send();
						}
					}}
					onpaste={(event) => {
						const files = imageFiles(event.clipboardData?.files);
						if (!attachable || !files.length) return;
						event.preventDefault();
						attachments.add(files);
					}}
				/>
				<InputGroup.Addon align="block-end" class="justify-between gap-2 max-md:flex-wrap pointer-coarse:flex-wrap">
					<div class="flex min-w-0 items-center gap-1">
						<InputGroup.Button
							size="icon-sm"
							class="attachment-button"
							aria-label={m['assistant.attach']()}
							title={m['assistant.attach']()}
							disabled={!attachable}
							onclick={() => fileInput?.click()}><Plus size={16} /></InputGroup.Button
						>
						{#if messageable.length || toAgent}
							<DropdownMenu.Root>
								<DropdownMenu.Trigger>
									{#snippet child({ props })}
										<InputGroup.Button
											{...props}
											size="sm"
											variant={toAgent ? 'secondary' : 'outline'}
											class="h-7 max-w-full min-w-0 gap-1 rounded-full px-2.5 text-xs transition-[width,background-color] duration-150 motion-reduce:transition-none {toAgent
												? 'w-56'
												: 'w-28'}"
											title={targetLabel}
											aria-label={m['assistant.target_label']({ target: targetLabel })}
											>{#if toAgent}<Bot size={13} />{:else}<MessageCircle size={13} />{/if}<span class="truncate"
												>{targetLabel}</span
											><ChevronDown size={12} class="shrink-0" /></InputGroup.Button
										>
									{/snippet}
								</DropdownMenu.Trigger>
								<DropdownMenu.Content align="start" class="touch-controls w-64">
									<DropdownMenu.Label class="text-xs font-normal text-muted-foreground"
										>{m['assistant.target_heading']()}</DropdownMenu.Label
									>
									<DropdownMenu.CheckboxItem checked={!toAgent} onCheckedChange={() => chooseTarget(null)}
										>{m['assistant.target_assistant']()}</DropdownMenu.CheckboxItem
									>
									{#each messageable as task (task.id)}<DropdownMenu.CheckboxItem
											checked={toAgent && targetTask?.id === task.id}
											onCheckedChange={() => chooseTarget(task.id)}
											><span class="truncate"
												>{m['assistant.target_agent']({
													identifier: chat.issue?.identifier ?? '',
													agent: agentName(task)
												})}</span
											></DropdownMenu.CheckboxItem
										>{/each}
								</DropdownMenu.Content>
							</DropdownMenu.Root>
						{:else}
							<span class="flex h-7 items-center gap-1 rounded-full border px-2.5 text-xs text-muted-foreground"
								><MessageCircle size={13} aria-hidden="true" />{m['assistant.target_assistant']()}</span
							>
						{/if}
					</div>
					<div class="ms-auto flex max-w-full shrink-0 flex-wrap justify-end items-center gap-1">
						<ComposerContext {chat} />
						<ComposerVoice
							{slug}
							ensureConversation={() => ensureConversation(m['voice.conversation_title']())}
							disabled={composerDisabled}
							ontranscript={(transcript) => {
								text = appendTranscript(text, transcript);
								textarea?.focus();
							}}
						/>
						<Tooltip.Provider delayDuration={300}>
							{#if now}<Tooltip.Root>
									<Tooltip.Trigger>
										{#snippet child({ props })}<InputGroup.Button
												{...props}
												size="icon-sm"
												disabled={!canSend}
												class="rounded-full"
												aria-label={nowLabel}
												aria-keyshortcuts="Control+Enter Meta+Enter"
												onclick={sendNow}
											>
												{#if now === 'interrupt'}<Zap />{:else}<Play />{/if}
											</InputGroup.Button>{/snippet}
									</Tooltip.Trigger>
									<Tooltip.Content>{nowLabel}<KbdGroup><Kbd>{mod}</Kbd><Kbd>↵</Kbd></KbdGroup></Tooltip.Content>
								</Tooltip.Root>{/if}
							{#if chat.busy && !toAgent}<InputGroup.Button
									size="icon-sm"
									aria-label={m['assistant.stop']()}
									onclick={() => void stop()}><Square size={14} /></InputGroup.Button
								>
							{:else}<Tooltip.Root
									><Tooltip.Trigger
										>{#snippet child({ props })}<InputGroup.Button
												{...props}
												type="submit"
												variant="default"
												size="icon-sm"
												class="rounded-full"
												disabled={!canSend}
												aria-label={toAgent ? m['tasks.live_send']() : m['assistant.send']()}
												><ArrowUp size={16} /></InputGroup.Button
											>{/snippet}</Tooltip.Trigger
									><Tooltip.Content
										>{toAgent ? m['tasks.live_send']() : m['assistant.send']()}<Kbd>↵</Kbd></Tooltip.Content
									></Tooltip.Root
								>{/if}
						</Tooltip.Provider>
					</div>
				</InputGroup.Addon>
			</InputGroup.Root>
			<p class="mt-1.5 flex min-w-0 justify-center gap-1 text-center text-[11px] text-muted-foreground">
				{#if notice}<span role="status" class="truncate">{notice}</span>{:else}
					<span class="truncate"
						>{toAgent ? '' : `${status.model ?? m['assistant.no_provider']()} · `}{sending
							? m['assistant.sending']()
							: attachments.uploading
								? m['assistant.attachment_wait']()
								: dragging
									? m['assistant.attachment_drop']()
									: m['assistant.hint']()}</span
					>{/if}
			</p>
		</form>
		{#if pageMention && pageItems.length}
			<MentionList
				items={pageItems}
				selectedIndex={pageIndex}
				position={pageMenu}
				anchorHeight={pageMenu.height}
				onselect={choosePage}
				onclose={() => (pageMention = null)}
			/>
		{/if}
	</div>
</div>

{#if targetTask}
	<MergedWorkDialog
		bind:pullRequest={merged}
		onconfirm={() => targetTask && void sendToAgent(targetTask, 'start', true)}
	/>
	<AlertDialog.Root bind:open={confirming}>
		<AlertDialog.Content>
			<AlertDialog.Header>
				<AlertDialog.Title>{m['tasks.live_start_confirm_title']()}</AlertDialog.Title>
				<AlertDialog.Description
					>{m['tasks.live_start_confirm_body']({ reason: targetTask.reason })}</AlertDialog.Description
				>
			</AlertDialog.Header>
			<AlertDialog.Footer>
				<AlertDialog.Cancel>{m['common.cancel']()}</AlertDialog.Cancel>
				<AlertDialog.Action disabled={!canSend} onclick={() => targetTask && void sendToAgent(targetTask, 'start')}
					>{m['tasks.live_start_send']()}</AlertDialog.Action
				>
			</AlertDialog.Footer>
		</AlertDialog.Content>
	</AlertDialog.Root>
{/if}
