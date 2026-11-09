<script lang="ts">
	import { beforeNavigate, goto } from '$app/navigation';
	import { page } from '$app/state';
	import { untrack } from 'svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import { Skeleton } from '$lib/components/ui/skeleton/index.js';
	import * as Alert from '$lib/components/ui/alert/index.js';
	import * as AlertDialog from '$lib/components/ui/alert-dialog/index.js';
	import RichEditor from '$lib/components/shared/RichEditor.svelte';
	import EmptyState from '$lib/components/shared/EmptyState.svelte';
	import { createWikiPage, deleteWikiPage, getWikiPage, listWikiPages, updateWikiPage } from '$lib/api/wiki';
	import type { WikiPage, WikiPageSummary, WikiScope } from '$lib/types/wiki';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/paraglide/messages.js';
	import { getLocale } from '$lib/paraglide/runtime.js';
	import { FileText, Pencil, Plus, Trash2 } from '@lucide/svelte';
	import { draftChanged, type WikiDraft } from './wiki-draft';

	let {
		slug,
		scope,
		canWrite,
		emptyDescription
	}: {
		slug: string;
		scope: WikiScope;
		/** Whether to offer editing; the API enforces access regardless. */
		canWrite: boolean;
		emptyDescription: string;
	} = $props();

	type Mode = 'view' | 'edit' | 'create';

	let pages = $state<WikiPageSummary[]>([]);
	let listState = $state<'loading' | 'ready' | 'denied' | 'failed'>('loading');
	let current = $state<WikiPage | null>(null);
	let pageState = $state<'idle' | 'loading' | 'missing' | 'failed'>('idle');
	let mode = $state<Mode>('view');
	let baseline = $state<WikiDraft>({ title: '', content: '' });
	let draft = $state<WikiDraft>({ title: '', content: '' });
	// What the editor starts from; the editor then owns the text until saved.
	let editorSeed = $state('');
	let saving = $state(false);
	let conflict = $state(false);
	let titleError = $state(false);
	let deleteOpen = $state(false);
	// An action waiting for the user to confirm discarding their draft.
	let pendingDiscard = $state<(() => void) | null>(null);
	let listRequest = 0;
	let pageRequest = 0;

	const selectedId = $derived(page.url.searchParams.get('page'));
	const dirty = $derived(mode !== 'view' && draftChanged(baseline, draft));
	const scopeKey = $derived(`${slug}/${scope.kind}/${scope.id}`);

	async function loadList() {
		const request = ++listRequest;
		listState = 'loading';
		try {
			const result = await listWikiPages(slug, scope);
			if (request !== listRequest) return;
			pages = result;
			listState = 'ready';
		} catch (error) {
			if (request !== listRequest) return;
			const status = (error as { status?: number }).status;
			listState = status === 403 || status === 404 ? 'denied' : 'failed';
		}
	}

	async function loadPage(id: string) {
		const request = ++pageRequest;
		pageState = 'loading';
		try {
			const result = await getWikiPage(slug, scope, id);
			if (request !== pageRequest) return;
			current = result;
			pageState = 'idle';
		} catch (error) {
			if (request !== pageRequest) return;
			current = null;
			pageState = (error as { status?: number }).status === 404 ? 'missing' : 'failed';
		}
	}

	$effect(() => {
		void scopeKey;
		pages = [];
		current = null;
		mode = 'view';
		void loadList();
	});

	// Open the requested page, or the most recently updated one. Only the URL
	// and list readiness drive this; local edits must not re-trigger it.
	$effect(() => {
		if (listState !== 'ready') return;
		const id = selectedId;
		untrack(() => {
			if (!id) {
				if (pages.length > 0) void openPage(pages[0].id, true);
				else current = null;
				return;
			}
			mode = 'view';
			conflict = false;
			if (current?.id !== id) void loadPage(id);
		});
	});

	function openPage(id: string, replaceState = false) {
		const url = new URL(page.url);
		url.searchParams.set('page', id);
		return goto(url, { replaceState, noScroll: true, keepFocus: true });
	}

	// Leaving with unsaved changes asks first. Closing or reloading the tab can
	// only use the browser's own prompt.
	beforeNavigate((navigation) => {
		if (!dirty) return;
		navigation.cancel();
		const target = navigation.to?.url;
		if (navigation.type !== 'leave' && target) pendingDiscard = () => void goto(target);
	});

	/** Runs action now, or after the user agrees to drop their draft. */
	function guard(action: () => void) {
		if (dirty) pendingDiscard = action;
		else action();
	}

	function discardAndContinue() {
		const action = pendingDiscard;
		pendingDiscard = null;
		mode = 'view';
		conflict = false;
		draft = { ...baseline };
		action?.();
	}

	function startCreate() {
		guard(() => {
			baseline = { title: '', content: '' };
			draft = { title: '', content: '' };
			editorSeed = '';
			titleError = false;
			conflict = false;
			mode = 'create';
		});
	}

	function startEdit() {
		if (!current) return;
		baseline = { title: current.title, content: current.content };
		draft = { ...baseline };
		editorSeed = current.content;
		titleError = false;
		conflict = false;
		mode = 'edit';
	}

	function cancelEdit() {
		guard(() => {
			mode = 'view';
			conflict = false;
		});
	}

	function summaryOf(saved: WikiPage): WikiPageSummary {
		const { content: _content, created_by: _createdBy, ...summary } = saved;
		return summary;
	}

	async function save() {
		if (saving || mode === 'view') return;
		if (!draft.title.trim()) {
			titleError = true;
			return;
		}
		saving = true;
		try {
			if (mode === 'create') {
				const created = await createWikiPage(slug, scope, { title: draft.title, content: draft.content });
				pages = [summaryOf(created), ...pages];
				current = created;
				baseline = { ...draft };
				mode = 'view';
				appToast.success(m['wiki.toast.created']());
				await openPage(created.id);
			} else if (current) {
				const saved = await updateWikiPage(slug, scope, current.id, {
					title: draft.title,
					content: draft.content,
					version: current.version
				});
				current = saved;
				pages = [summaryOf(saved), ...pages.filter((p) => p.id !== saved.id)];
				baseline = { ...draft };
				mode = 'view';
				appToast.success(m['wiki.toast.saved']());
			}
		} catch (error) {
			if ((error as { status?: number }).status === 409) conflict = true;
			else appToast.error(m['wiki.toast.save_failed']());
		} finally {
			saving = false;
		}
	}

	async function loadLatest() {
		if (!current) return;
		const id = current.id;
		mode = 'view';
		conflict = false;
		draft = { ...baseline };
		await loadPage(id);
		if (current) pages = pages.map((p) => (p.id === id ? summaryOf(current as WikiPage) : p));
	}

	async function confirmDelete() {
		const target = current;
		deleteOpen = false;
		if (!target) return;
		try {
			await deleteWikiPage(slug, scope, target.id);
			pages = pages.filter((p) => p.id !== target.id);
			current = null;
			appToast.success(m['wiki.toast.deleted']());
			const url = new URL(page.url);
			url.searchParams.delete('page');
			await goto(url, { replaceState: true, noScroll: true, keepFocus: true });
		} catch {
			appToast.error(m['wiki.toast.delete_failed']());
		}
	}

	function onkeydown(event: KeyboardEvent) {
		if (mode !== 'view' && (event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 's') {
			event.preventDefault();
			void save();
		}
	}

	function formatDate(value: string) {
		return new Date(value).toLocaleString(getLocale(), { dateStyle: 'medium', timeStyle: 'short' });
	}
</script>

<svelte:window {onkeydown} />

{#if listState === 'loading'}
	<div role="status" aria-label={m['common.loading']()} class="space-y-3 p-6">
		<Skeleton class="h-5 w-48" />
		<Skeleton class="h-4 w-72" />
		<Skeleton class="h-4 w-64" />
	</div>
{:else if listState === 'denied'}
	<EmptyState title={m['wiki.title']()} description={m['wiki.not_found']()} />
{:else if listState === 'failed'}
	<EmptyState title={m['wiki.load_failed']()} action={{ label: m['wiki.retry'](), onclick: loadList }} />
{:else if pages.length === 0 && mode !== 'create'}
	<div data-testid="wiki-empty">
		<EmptyState
			title={m['wiki.empty_title']()}
			description={canWrite ? emptyDescription : m['wiki.empty_readonly']()}
			action={canWrite ? { label: m['wiki.create_first'](), onclick: startCreate } : undefined}
		/>
	</div>
{:else}
	<div class="flex min-h-0 flex-1 flex-col md:flex-row">
		<nav
			aria-label={m['wiki.pages']()}
			class="flex max-h-48 shrink-0 flex-col border-b border-[var(--app-border)] md:max-h-none md:w-64 md:border-r md:border-b-0"
		>
			<div class="flex items-center justify-between px-4 py-2">
				<h2 class="text-xs font-medium tracking-wide text-[var(--color-text-tertiary)] uppercase">{m['wiki.pages']()}</h2>
				{#if canWrite}
					<Button variant="ghost" size="icon-sm" aria-label={m['wiki.new_page']()} title={m['wiki.new_page']()} onclick={startCreate}>
						<Plus size={14} />
					</Button>
				{/if}
			</div>
			<ul class="min-h-0 flex-1 overflow-y-auto px-2 pb-2">
				{#each pages as item (item.id)}
					<li>
						<a
							href="?page={item.id}"
							onclick={(event) => {
								// Same URL: no navigation happens, so leave the editor here.
								if (item.id !== selectedId || mode === 'view') return;
								event.preventDefault();
								guard(() => {
									mode = 'view';
									conflict = false;
								});
							}}
							aria-current={item.id === selectedId && mode !== 'create' ? 'page' : undefined}
							class="flex items-center gap-2 rounded-md px-2 py-1.5 text-sm {item.id === selectedId && mode !== 'create'
								? 'bg-[var(--color-bg-hover)] text-[var(--color-text-primary)]'
								: 'text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]'}"
						>
							<FileText size={14} class="shrink-0 text-[var(--color-text-tertiary)]" />
							<span class="truncate">{item.title}</span>
						</a>
					</li>
				{/each}
			</ul>
		</nav>

		<section class="min-h-0 min-w-0 flex-1 overflow-y-auto">
			{#if mode !== 'view'}
				<form
					class="mx-auto flex max-w-3xl flex-col gap-4 px-4 py-6 sm:px-8"
					onsubmit={(event) => {
						event.preventDefault();
						void save();
					}}
				>
					{#if conflict}
						<Alert.Root variant="destructive" data-testid="wiki-conflict">
							<Alert.Title>{m['wiki.conflict_title']()}</Alert.Title>
							<Alert.Description>
								<p>{m['wiki.conflict_desc']()}</p>
								<Button variant="outline" size="sm" class="mt-2" onclick={loadLatest}>{m['wiki.load_latest']()}</Button>
							</Alert.Description>
						</Alert.Root>
					{/if}
					<div class="flex flex-col gap-1.5">
						<Label for="wiki-title">{m['wiki.title_label']()}</Label>
						<Input
							id="wiki-title"
							bind:value={draft.title}
							maxlength={200}
							placeholder={m['wiki.title_placeholder']()}
							aria-invalid={titleError || undefined}
							aria-describedby={titleError ? 'wiki-title-error' : undefined}
							oninput={() => (titleError = false)}
						/>
						{#if titleError}
							<p id="wiki-title-error" class="text-xs text-[var(--color-error)]">{m['wiki.title_required']()}</p>
						{/if}
					</div>
					{#key `${mode}/${current?.id ?? 'new'}`}
						<RichEditor
							content={editorSeed}
							workspaceSlug={slug}
							placeholder={m['wiki.content_placeholder']()}
							ariaLabel={m['wiki.content_label']()}
							hideUploadButtons
							minHeight="320px"
							onupdate={(html) => (draft.content = html)}
						/>
					{/key}
					<div class="flex items-center justify-end gap-2">
						{#if dirty}
							<span class="mr-auto text-xs text-[var(--color-text-tertiary)]">{m['wiki.unsaved']()}</span>
						{/if}
						<Button variant="outline" onclick={cancelEdit} disabled={saving}>{m['common.cancel']()}</Button>
						<Button type="submit" disabled={saving || conflict}>{saving ? m['wiki.saving']() : m['wiki.save']()}</Button>
					</div>
				</form>
			{:else if pageState === 'loading' && !current}
				<div role="status" aria-label={m['common.loading']()} class="mx-auto max-w-3xl space-y-3 px-4 py-6 sm:px-8">
					<Skeleton class="h-7 w-64" />
					<Skeleton class="h-4 w-full" />
					<Skeleton class="h-4 w-5/6" />
				</div>
			{:else if pageState === 'missing'}
				<EmptyState title={m['wiki.page_not_found']()} />
			{:else if pageState === 'failed'}
				<EmptyState
					title={m['wiki.load_failed']()}
					action={{ label: m['wiki.retry'](), onclick: () => selectedId && loadPage(selectedId) }}
				/>
			{:else if current}
				<article class="mx-auto flex max-w-3xl flex-col gap-4 px-4 py-6 sm:px-8">
					<header class="flex items-start justify-between gap-4">
						<div class="min-w-0">
							<h1 class="text-xl font-semibold break-words text-[var(--color-text-primary)]">{current.title}</h1>
							<p class="mt-1 text-xs text-[var(--color-text-tertiary)]">
								{current.updated_by_name
									? m['wiki.updated_by']({ date: formatDate(current.updated_at), name: current.updated_by_name })
									: m['wiki.updated']({ date: formatDate(current.updated_at) })}
							</p>
						</div>
						{#if canWrite}
							<div class="flex shrink-0 items-center gap-1">
								<Button variant="outline" size="sm" onclick={startEdit}><Pencil size={14} />{m['wiki.edit']()}</Button>
								<Button
									variant="ghost"
									size="icon-sm"
									aria-label={m['wiki.delete']()}
									title={m['wiki.delete']()}
									onclick={() => (deleteOpen = true)}><Trash2 size={14} /></Button
								>
							</div>
						{/if}
					</header>
					{#key `${current.id}/${current.version}`}
						<RichEditor content={current.content} editable={false} borderless ariaLabel={current.title} />
					{/key}
				</article>
			{:else}
				<EmptyState title={m['wiki.select_page']()} />
			{/if}
		</section>
	</div>
{/if}

<AlertDialog.Root open={pendingDiscard !== null} onOpenChange={(open) => !open && (pendingDiscard = null)}>
	<AlertDialog.Content>
		<AlertDialog.Header>
			<AlertDialog.Title>{m['wiki.leave_title']()}</AlertDialog.Title>
			<AlertDialog.Description>{m['wiki.leave_desc']()}</AlertDialog.Description>
		</AlertDialog.Header>
		<AlertDialog.Footer>
			<AlertDialog.Cancel>{m['wiki.stay']()}</AlertDialog.Cancel>
			<AlertDialog.Action variant="destructive" onclick={discardAndContinue} data-testid="wiki-discard"
				>{m['wiki.discard']()}</AlertDialog.Action
			>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>

<AlertDialog.Root bind:open={deleteOpen}>
	<AlertDialog.Content>
		<AlertDialog.Header>
			<AlertDialog.Title>{m['wiki.delete_title']()}</AlertDialog.Title>
			<AlertDialog.Description>{m['wiki.delete_desc']({ title: current?.title ?? '' })}</AlertDialog.Description>
		</AlertDialog.Header>
		<AlertDialog.Footer>
			<AlertDialog.Cancel>{m['common.cancel']()}</AlertDialog.Cancel>
			<AlertDialog.Action variant="destructive" onclick={confirmDelete}>{m['wiki.delete']()}</AlertDialog.Action>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>
