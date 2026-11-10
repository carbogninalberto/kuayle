<script lang="ts">
	import { onDestroy, onMount } from 'svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import * as Alert from '$lib/components/ui/alert/index.js';
	import RichEditor from '$lib/components/shared/RichEditor.svelte';
	import { getWikiPage, reworkWikiSelection, updateWikiPage } from '$lib/api/wiki';
	import type { WikiPage, WikiScope } from '$lib/types/wiki';
	import type { WorkspaceMember } from '$lib/types/workspace';
	import type { Issue } from '$lib/types/issue';
	import { presenceState } from '$lib/features/presence/presence.state.svelte';
	import { authState } from '$lib/features/auth/auth.state.svelte';
	import { reworkDescriptionSelection } from '$lib/features/issues/description-rework';
	import { appToast } from '$lib/features/toast/toast';
	import { formatRelativeTime } from '$lib/utils/format';
	import { m, getLocale } from '$lib/features/i18n/messages';
	import { ChevronRight, FileText, Image as ImageIcon, Paperclip } from '@lucide/svelte';
	import { WikiAutosave, type SaveState } from './wiki-autosave';
	import { wikiFiles, wikiOutline } from './wiki-content';
	import WikiBacklinks from './WikiBacklinks.svelte';

	// One wiki page laid out like the full issue page: title and description
	// editor on the left, property cards on the right. Edits autosave like an
	// issue description; the page version guards against overwriting others.
	let {
		slug,
		scope,
		page: initialPage,
		canWrite,
		members,
		issues,
		uploadUrl,
		scopeLabel,
		editTitle = false,
		saveState = $bindable<SaveState>('idle'),
		onsaved,
		onreload
	}: {
		slug: string;
		scope: WikiScope;
		page: WikiPage;
		canWrite: boolean;
		members: WorkspaceMember[];
		issues: Issue[];
		/** Undefined until the owning team is known; uploads stay off until then. */
		uploadUrl?: string;
		scopeLabel: string;
		/** Opens the title for editing, e.g. on a page just created. */
		editTitle?: boolean;
		saveState?: SaveState;
		onsaved?: (page: WikiPage) => void;
		/** Asks the parent to load the latest version from scratch. */
		onreload?: () => void;
	} = $props();

	// The parent re-keys this component per page; the prop seeds local state.
	// svelte-ignore state_referenced_locally
	let page = $state<WikiPage>(initialPage);
	// svelte-ignore state_referenced_locally
	let titleValue = $state(initialPage.title);
	// svelte-ignore state_referenced_locally
	let editingTitle = $state(editTitle && canWrite);
	// svelte-ignore state_referenced_locally
	let liveContent = $state(initialPage.content);
	let conflict = $state(false);
	let detailsExpanded = $state(true);
	let outlineExpanded = $state(true);
	let filesExpanded = $state(true);
	let backlinksExpanded = $state(true);
	let editorHost = $state<HTMLElement | null>(null);
	let lastLocalSave = 0;
	let disposed = false;

	// svelte-ignore state_referenced_locally
	const autosave = new WikiAutosave<WikiPage>(initialPage, {
		save: (changes, version) => updateWikiPage(slug, scope, page.id, { ...changes, version }),
		onstate: (state) => {
			saveState = state;
			conflict = state === 'conflict';
		},
		onsaved: (saved) => {
			lastLocalSave = Date.now();
			page = saved;
			onsaved?.(saved);
		},
		onerror: (error) => appToast.apiError(error, m['wiki.toast.save_failed']())
	});

	const outline = $derived(wikiOutline(liveContent));
	const files = $derived(wikiFiles(liveContent));
	const descriptionCursors = $derived(presenceState.getCursorsForField('description'));
	const createdBy = $derived(members.find((member) => member.user_id === page.created_by)?.name ?? null);

	function saveTitle() {
		editingTitle = false;
		const title = titleValue.trim();
		if (!title) {
			titleValue = page.title;
			return;
		}
		titleValue = title;
		autosave.edit({ title });
		void autosave.flush();
	}

	// Navigation keeps focus where it was, which can win over autofocus on a
	// page just created; focus the title once the page has settled.
	function focusTitle(node: HTMLInputElement) {
		const timer = setTimeout(() => {
			if (document.activeElement !== node) node.focus();
		}, 50);
		return () => clearTimeout(timer);
	}

	function onContent(html: string) {
		liveContent = html;
		autosave.edit({ content: html });
	}

	function rework(selectedText: string): Promise<string> {
		// The server reworks against the saved page, so send pending edits first.
		return reworkDescriptionSelection(async () => {
			await autosave.flush();
			return reworkWikiSelection(slug, scope, page.id, selectedText);
		});
	}

	function jumpTo(index: number) {
		const heading = editorHost?.querySelectorAll('h1, h2, h3, h4, h5, h6')[index];
		heading?.scrollIntoView({ behavior: 'smooth', block: 'start' });
	}

	// Someone else saved: follow along unless there are local edits, which
	// then meet the version check and show the conflict notice.
	async function onWikiChanged(event: Event) {
		const detail = (event as CustomEvent<{ id?: string; version?: number; updated_by?: string | null }>).detail;
		if (detail?.id !== page.id || (detail.version ?? 0) <= page.version) return;
		if (detail.updated_by === authState.user?.id && Date.now() - lastLocalSave < 2000) return;
		if (autosave.dirty) return;
		try {
			const fresh = await getWikiPage(slug, scope, page.id);
			if (disposed || autosave.dirty) return;
			autosave.follow(fresh);
			page = fresh;
			liveContent = fresh.content;
			if (!editingTitle) titleValue = fresh.title;
			onsaved?.(fresh);
		} catch {
			/* the list refresh reports a page that disappeared */
		}
	}

	function onPresenceJoin(e: Event) {
		presenceState.handleJoin((e as CustomEvent).detail);
	}
	function onPresenceLeave(e: Event) {
		presenceState.handleLeave((e as CustomEvent).detail);
	}
	function onPresenceSync(e: Event) {
		presenceState.handleSync((e as CustomEvent).detail);
	}
	function onFocusUpdate(e: Event) {
		presenceState.handleFocusUpdate((e as CustomEvent).detail);
	}
	function onFocusLeave(e: Event) {
		presenceState.handleFocusLeave((e as CustomEvent).detail);
	}
	function onReconnected() {
		presenceState.join(page.id, members);
	}
	function onBeforeUnload(event: BeforeUnloadEvent) {
		if (!autosave.dirty) return;
		void autosave.flush();
		event.preventDefault();
	}

	const listeners: [string, (event: Event) => void][] = [
		['ws:wiki-changed', onWikiChanged],
		['ws:presence.join', onPresenceJoin],
		['ws:presence.leave', onPresenceLeave],
		['ws:presence.sync', onPresenceSync],
		['ws:focus.update', onFocusUpdate],
		['ws:focus.leave', onFocusLeave],
		['ws:reconnected', onReconnected]
	];

	onMount(() => {
		for (const [name, listener] of listeners) window.addEventListener(name, listener);
		window.addEventListener('beforeunload', onBeforeUnload);
		presenceState.join(page.id, members);
	});

	onDestroy(() => {
		disposed = true;
		for (const [name, listener] of listeners) window.removeEventListener(name, listener);
		window.removeEventListener('beforeunload', onBeforeUnload);
		presenceState.leave();
		void autosave.flush();
	});

	$effect(() => {
		if (members.length > 0) presenceState.setMembers(members);
	});

	function formatSize(size: number): string {
		if (!size) return '';
		if (size < 1024 * 1024) return `${Math.max(1, Math.round(size / 1024))} KB`;
		return `${(size / (1024 * 1024)).toFixed(1)} MB`;
	}
</script>

<div class="min-h-0 flex-1 overflow-y-auto md:flex md:overflow-hidden">
	<div class="min-w-0 md:min-h-0 md:flex-1 md:overflow-y-auto">
		<div class="mx-auto max-w-[840px] px-4 py-5 sm:px-10 sm:py-6">
			{#if conflict}
				<Alert.Root variant="destructive" class="mb-4" data-testid="wiki-conflict">
					<Alert.Title>{m['wiki.conflict_title']()}</Alert.Title>
					<Alert.Description>
						<p>{m['wiki.conflict_desc']()}</p>
						<Button variant="outline" size="sm" class="mt-2" onclick={() => onreload?.()}
							>{m['wiki.load_latest']()}</Button
						>
					</Alert.Description>
				</Alert.Root>
			{/if}

			<!-- svelte-ignore a11y_autofocus -->
			<div class="relative">
				{#if editingTitle}
					<Input
						variant="plain"
						type="text"
						bind:value={titleValue}
						maxlength={200}
						aria-label={m['wiki.title_label']()}
						onblur={() => {
							saveTitle();
							presenceState.sendFocusLeave(page.id);
						}}
						onfocus={(e) => {
							presenceState.sendFocus(page.id, 'title', 0);
							(e.currentTarget as HTMLInputElement).select();
						}}
						onkeydown={(e) => {
							if (e.key === 'Enter') (e.currentTarget as HTMLInputElement).blur();
							if (e.key === 'Escape') {
								titleValue = page.title;
								editingTitle = false;
							}
						}}
						autofocus
						{@attach focusTitle}
						class="w-full bg-transparent text-lg font-semibold text-[var(--color-text-primary)] outline-none"
					/>
				{:else if canWrite}
					<h1 class="text-lg font-semibold text-[var(--color-text-primary)]">
						<Button
							variant="plain"
							onclick={() => (editingTitle = true)}
							class="w-full text-left break-words transition-colors hover:text-[var(--color-text-primary)]"
							>{titleValue}</Button
						>
					</h1>
				{:else}
					<h1 class="text-lg font-semibold break-words text-[var(--color-text-primary)]">{titleValue}</h1>
				{/if}
			</div>

			<div class="mt-3" bind:this={editorHost} data-testid="wiki-editor">
				<RichEditor
					content={page.content}
					workspaceSlug={slug}
					editable={canWrite}
					placeholder={m['wiki.content_placeholder']()}
					ariaLabel={m['wiki.content_label']()}
					bubbleMenu={true}
					borderless={true}
					minHeight="240px"
					uploadUrl={canWrite ? uploadUrl : undefined}
					{members}
					{issues}
					onupdate={onContent}
					remoteCursors={descriptionCursors}
					onfocus={() => presenceState.sendFocus(page.id, 'description', 0)}
					onblur={() => {
						presenceState.sendFocusLeave(page.id);
						void autosave.flush();
					}}
					oncursorchange={(pos, anchor) => presenceState.sendFocus(page.id, 'description', pos, anchor)}
					onreworkselection={canWrite ? rework : undefined}
				/>
			</div>
		</div>
	</div>

	<aside
		aria-label={m['wiki.details']()}
		class="w-full space-y-2 border-t border-[var(--app-border)] p-3 md:w-[18.75rem] md:shrink-0 md:overflow-y-auto md:border-t-0"
	>
		{#snippet cardHeader(label: string, expanded: boolean, toggle: () => void)}
			<Button
				variant="plain"
				onclick={toggle}
				aria-expanded={expanded}
				class="flex w-full items-center gap-1.5 px-3 py-2.5 text-[11px] font-medium tracking-wider text-[var(--color-text-tertiary)] uppercase transition-colors hover:text-[var(--color-text-secondary)]"
			>
				<ChevronRight size={12} class="transition-transform {expanded ? 'rotate-90' : ''}" />
				{label}
			</Button>
		{/snippet}

		<div class="rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
			{@render cardHeader(m['wiki.details'](), detailsExpanded, () => (detailsExpanded = !detailsExpanded))}
			{#if detailsExpanded}
				<dl class="space-y-0.5 px-1.5 pb-2">
					{#each [{ label: m['wiki.location'](), value: scopeLabel }, { label: m['wiki.created_by'](), value: createdBy ?? '—' }, { label: m['wiki.created'](), value: formatRelativeTime(page.created_at, getLocale()), title: new Date(page.created_at).toLocaleString(getLocale()) }, { label: m['wiki.updated_by_label'](), value: page.updated_by_name ?? '—' }, { label: m['wiki.updated_label'](), value: formatRelativeTime(page.updated_at, getLocale()), title: new Date(page.updated_at).toLocaleString(getLocale()) }, { label: m['wiki.version'](), value: String(page.version) }] as row (row.label)}
						<div
							class="flex items-center gap-3 rounded-md px-2 py-1.5 transition-colors hover:bg-[var(--color-bg-hover)]"
						>
							<dt class="w-20 shrink-0 text-xs text-[var(--color-text-tertiary)]">{row.label}</dt>
							<dd class="min-w-0 truncate text-sm text-[var(--color-text-primary)]" title={row.title}>{row.value}</dd>
						</div>
					{/each}
				</dl>
			{/if}
		</div>

		<div class="rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
			{@render cardHeader(m['wiki.outline'](), outlineExpanded, () => (outlineExpanded = !outlineExpanded))}
			{#if outlineExpanded}
				<nav aria-label={m['wiki.outline']()} class="px-1.5 pb-2">
					{#if outline.length === 0}
						<p class="px-2 pb-1 text-xs text-[var(--color-text-tertiary)]">{m['wiki.outline_empty']()}</p>
					{:else}
						<ul>
							{#each outline as item (item.index)}
								<li>
									<Button
										variant="plain"
										onclick={() => jumpTo(item.index)}
										class="block w-full truncate rounded-md px-2 py-1 text-left text-sm text-[var(--color-text-secondary)] transition-colors hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]"
										style="padding-left: {0.5 + (item.level - 1) * 0.75}rem">{item.text}</Button
									>
								</li>
							{/each}
						</ul>
					{/if}
				</nav>
			{/if}
		</div>

		<div class="rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
			{@render cardHeader(m['wiki.files'](), filesExpanded, () => (filesExpanded = !filesExpanded))}
			{#if filesExpanded}
				<div class="px-1.5 pb-2">
					{#if files.length === 0}
						<p class="px-2 pb-1 text-xs text-[var(--color-text-tertiary)]">
							{canWrite && uploadUrl ? m['wiki.files_empty_writer']() : m['wiki.files_empty']()}
						</p>
					{:else}
						<ul>
							{#each files as file (file.href)}
								{@const Icon =
									file.kind === 'image' ? ImageIcon : file.name.match(/\.(md|txt|pdf|docx?)$/i) ? FileText : Paperclip}
								<li>
									<a
										href={file.href}
										target="_blank"
										rel="noopener noreferrer"
										download={file.kind === 'file' ? file.name : undefined}
										class="flex items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-secondary)] transition-colors hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]"
									>
										<Icon size={14} class="shrink-0 text-[var(--color-text-tertiary)]" />
										<span class="min-w-0 flex-1 truncate">{file.name}</span>
										{#if file.size}<span class="shrink-0 text-xs text-[var(--color-text-tertiary)]"
												>{formatSize(file.size)}</span
											>{/if}
									</a>
								</li>
							{/each}
						</ul>
					{/if}
				</div>
			{/if}
		</div>

		<div class="rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
			{@render cardHeader(
				m['wiki.backlinks.title'](),
				backlinksExpanded,
				() => (backlinksExpanded = !backlinksExpanded)
			)}
			{#if backlinksExpanded}
				<WikiBacklinks {slug} {scope} pageId={page.id} />
			{/if}
		</div>
	</aside>
</div>
