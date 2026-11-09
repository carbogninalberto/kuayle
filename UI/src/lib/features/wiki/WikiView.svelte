<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { onMount, untrack, type Snippet } from 'svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Skeleton } from '$lib/components/ui/skeleton/index.js';
	import { Switch } from '$lib/components/ui/switch/index.js';
	import * as AlertDialog from '$lib/components/ui/alert-dialog/index.js';
	import * as Popover from '$lib/components/ui/popover';
	import EmptyState from '$lib/components/shared/EmptyState.svelte';
	import SidebarToggle from '$lib/components/layout/SidebarToggle.svelte';
	import VersionHistoryDialog, { type VersionEntry } from '$lib/components/shared/VersionHistoryDialog.svelte';
	import {
		createWikiPage,
		deleteWikiPage,
		getWikiPage,
		getWikiSettings,
		listWikiPages,
		listWikiPageVersions,
		updateWikiPage,
		updateWikiSettings
	} from '$lib/api/wiki';
	import { listMembers } from '$lib/api/members';
	import { listIssues } from '$lib/api/issues';
	import type { WikiPage, WikiPageSummary, WikiScope } from '$lib/types/wiki';
	import type { WorkspaceMember } from '$lib/types/workspace';
	import type { Issue } from '$lib/types/issue';
	import { presenceState } from '$lib/features/presence/presence.state.svelte';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/paraglide/messages.js';
	import {
		BookOpen,
		CalendarDays,
		FileText,
		History,
		Link as LinkIcon,
		MoreHorizontal,
		Plus,
		Trash2
	} from '@lucide/svelte';
	import WikiPageView from './WikiPageView.svelte';
	import type { SaveState } from './wiki-autosave';

	let {
		slug,
		scope,
		canWrite,
		emptyDescription,
		scopeLabel,
		uploadTeamId,
		crumb
	}: {
		slug: string;
		scope: WikiScope;
		/** Whether to offer editing; the API enforces access regardless. */
		canWrite: boolean;
		emptyDescription: string;
		scopeLabel: string;
		/** Team that owns uploads: null for a teamless project, undefined while unknown. */
		uploadTeamId: string | null | undefined;
		/** Breadcrumb to the team or project, before "Wiki". */
		crumb?: Snippet;
	} = $props();

	let pages = $state<WikiPageSummary[]>([]);
	let listState = $state<'loading' | 'ready' | 'denied' | 'failed'>('loading');
	let current = $state<WikiPage | null>(null);
	let pageState = $state<'idle' | 'loading' | 'missing' | 'failed'>('idle');
	let saveState = $state<SaveState>('idle');
	let members = $state<WorkspaceMember[]>([]);
	let issues = $state<Issue[]>([]);
	let registryEnabled = $state(false);
	let registryBusy = $state(false);
	let creating = $state(false);
	let deleteOpen = $state(false);
	let actionsOpen = $state(false);
	let historyOpen = $state(false);
	// Re-mounts the page view after a restore or "load latest".
	let reloadToken = $state(0);
	let justCreated = $state<string | null>(null);
	let listRequest = 0;
	let pageRequest = 0;

	const selectedId = $derived(page.url.searchParams.get('page'));
	const scopeKey = $derived(`${slug}/${scope.kind}/${scope.id}`);
	const documents = $derived(pages.filter((p) => !p.registry_date));
	const registry = $derived(
		pages.filter((p) => p.registry_date).sort((a, b) => (b.registry_date ?? '').localeCompare(a.registry_date ?? ''))
	);
	const uploadUrl = $derived(
		uploadTeamId === undefined
			? undefined
			: uploadTeamId
				? `/api/workspaces/${slug}/upload?team_id=${uploadTeamId}`
				: `/api/workspaces/${slug}/upload`
	);

	async function loadList(showLoading = true) {
		const request = ++listRequest;
		if (showLoading) listState = 'loading';
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

	// Members and recent issues feed @mentions and issue references, as in
	// the issue editor. Failures only limit suggestions.
	async function loadContext() {
		const filter: Record<string, string> = scope.kind === 'team' ? { team: scope.id } : { project: scope.id };
		const [memberList, issueList, settings] = await Promise.allSettled([
			listMembers(slug),
			listIssues(slug, { ...filter, per_page: '100', sort: 'updated_at', order: 'desc' }),
			getWikiSettings(slug, scope)
		]);
		if (memberList.status === 'fulfilled') members = memberList.value ?? [];
		if (issueList.status === 'fulfilled') issues = issueList.value.data ?? [];
		registryEnabled = settings.status === 'fulfilled' && settings.value.registry_enabled;
	}

	$effect(() => {
		void scopeKey;
		untrack(() => {
			pages = [];
			current = null;
			void loadList();
			void loadContext();
		});
	});

	// Open the requested page, or the most recently updated document.
	$effect(() => {
		if (listState !== 'ready') return;
		const id = selectedId;
		untrack(() => {
			if (!id) {
				const first = documents[0] ?? registry[0];
				if (first) void openPage(first.id, true);
				else current = null;
				return;
			}
			if (current?.id !== id) void loadPage(id);
		});
	});

	function openPage(id: string, replaceState = false) {
		const url = new URL(page.url);
		url.searchParams.set('page', id);
		return goto(url, { replaceState, noScroll: true, keepFocus: true });
	}

	function summaryOf(saved: WikiPage): WikiPageSummary {
		const { content: _content, created_by: _createdBy, ...summary } = saved;
		return summary;
	}

	function onPageSaved(saved: WikiPage) {
		if (current?.id === saved.id) current = { ...current, ...saved };
		pages = [summaryOf(saved), ...pages.filter((p) => p.id !== saved.id)];
	}

	async function createPage() {
		if (creating) return;
		creating = true;
		try {
			const created = await createWikiPage(slug, scope, { title: m['wiki.untitled'](), content: '' });
			pages = [summaryOf(created), ...pages];
			current = created;
			justCreated = created.id;
			await openPage(created.id);
		} catch (error) {
			appToast.apiError(error, m['wiki.toast.create_failed']());
		} finally {
			creating = false;
		}
	}

	async function reloadCurrent() {
		if (!current) return;
		await loadPage(current.id);
		if (current) onPageSaved(current);
		reloadToken++;
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
		} catch (error) {
			appToast.apiError(error, m['wiki.toast.delete_failed']());
		}
	}

	async function setRegistry(enabled: boolean) {
		if (registryBusy) return;
		registryBusy = true;
		const previous = registryEnabled;
		registryEnabled = enabled;
		try {
			registryEnabled = (await updateWikiSettings(slug, scope, { registry_enabled: enabled })).registry_enabled;
		} catch (error) {
			registryEnabled = previous;
			appToast.apiError(error, m['wiki.toast.registry_failed']());
		} finally {
			registryBusy = false;
		}
	}

	async function loadHistory(): Promise<VersionEntry[]> {
		if (!current) return [];
		const revisions = await listWikiPageVersions(slug, scope, current.id);
		const entries: VersionEntry[] = [];
		for (const revision of [...revisions].reverse()) {
			const author = revision.edited_by_name ?? m['issue.versions.unknown']();
			const at = revision.updated_at;
			if (revision.old_title !== revision.new_title)
				entries.push({
					id: `${revision.id}/title`,
					field: 'title',
					old_value: revision.old_title,
					new_value: revision.new_title,
					author,
					created_at: at
				});
			if (revision.old_content !== revision.new_content)
				entries.push({
					id: `${revision.id}/body`,
					field: 'body',
					old_value: revision.old_content,
					new_value: revision.new_content,
					author,
					created_at: at
				});
		}
		return entries;
	}

	async function restore(entry: VersionEntry, value: string) {
		if (!current) return;
		try {
			const saved = await updateWikiPage(slug, scope, current.id, {
				...(entry.field === 'title' ? { title: value } : { content: value }),
				version: current.version
			});
			current = saved;
			onPageSaved(saved);
			reloadToken++;
			appToast.success(m['wiki.toast.restored']());
		} catch (error) {
			appToast.apiError(error, m['wiki.toast.restore_failed']());
			throw error;
		}
	}

	async function copyLink() {
		try {
			await navigator.clipboard.writeText(window.location.href);
			appToast.success(m['wiki.toast.link_copied']());
		} catch {
			appToast.error(m['wiki.toast.link_copy_failed']());
		}
	}

	// Pages added, renamed or removed elsewhere refresh the list; the open page
	// follows its own changes. A privacy refresh carries no details.
	onMount(() => {
		const onWikiChanged = (event: Event) => {
			const detail = (event as CustomEvent<{ team_id?: string | null; project_id?: string | null }>).detail;
			const id = scope.kind === 'team' ? detail?.team_id : detail?.project_id;
			if (id === scope.id) void loadList(false);
		};
		const onRefresh = (event: Event) => {
			const detail = (event as CustomEvent<{ slug?: string }>).detail;
			if (detail?.slug && detail.slug !== slug) return;
			void loadList(false);
		};
		window.addEventListener('ws:wiki-changed', onWikiChanged);
		window.addEventListener('app:refresh', onRefresh);
		return () => {
			window.removeEventListener('ws:wiki-changed', onWikiChanged);
			window.removeEventListener('app:refresh', onRefresh);
		};
	});

	const saveLabel = $derived(
		saveState === 'pending'
			? m['wiki.save_state.pending']()
			: saveState === 'saving'
				? m['wiki.saving']()
				: saveState === 'saved'
					? m['wiki.save_state.saved']()
					: saveState === 'error'
						? m['wiki.save_state.error']()
						: ''
	);
	const iconButton =
		'rounded p-1.5 text-[var(--color-text-tertiary)] transition-colors hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]';
	const menuItem =
		'flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] transition-colors hover:bg-[var(--color-bg-hover)]';
</script>

<div class="flex h-full min-w-0 flex-col">
	<div
		class="flex min-h-[49px] shrink-0 items-center justify-between gap-2 border-b border-[var(--app-border)] px-3 sm:px-4"
	>
		<div class="flex min-w-0 items-center gap-3">
			<SidebarToggle />
			<nav aria-label={m['wiki.breadcrumb']()} class="flex min-w-0 items-center gap-1.5 text-xs">
				{#if crumb}
					{@render crumb()}
					<span class="shrink-0 text-[var(--color-text-tertiary)]">&rsaquo;</span>
				{/if}
				<span
					class="flex shrink-0 items-center gap-1.5 {current
						? 'text-[var(--color-text-tertiary)]'
						: 'font-medium text-[var(--color-text-primary)]'}"
				>
					<BookOpen size={13} class="shrink-0" />
					{m['wiki.title']()}
				</span>
				{#if current}
					<span class="shrink-0 text-[var(--color-text-tertiary)]">&rsaquo;</span>
					<span class="truncate font-medium text-[var(--color-text-primary)]">{current.title}</span>
				{/if}
			</nav>
		</div>
		{#if current}
			<div class="flex shrink-0 items-center gap-0.5">
				{#if saveLabel}
					<span
						role="status"
						class="mr-2 text-[11px] {saveState === 'error'
							? 'text-[var(--color-error)]'
							: 'text-[var(--color-text-tertiary)]'}"
						data-testid="wiki-save-state">{saveLabel}</span
					>
				{/if}
				<Button
					variant="plain"
					onclick={copyLink}
					class={iconButton}
					title={m['wiki.copy_link']()}
					aria-label={m['wiki.copy_link']()}
				>
					<LinkIcon size={14} />
				</Button>
				<VersionHistoryDialog
					bind:open={historyOpen}
					dialogTitle={m['wiki.history_title']({ title: current.title })}
					intro={m['wiki.history_intro']()}
					empty={m['wiki.history_empty']()}
					fieldName={(field) => (field === 'title' ? m['issue.versions.title_field']() : m['wiki.content_field']())}
					load={loadHistory}
					onrestore={canWrite ? restore : undefined}
					triggerClass={iconButton}
				>
					{#snippet triggerContent()}
						<History size={14} aria-label={m['issue.versions.button']()} />
					{/snippet}
				</VersionHistoryDialog>
				{#if canWrite}
					<Popover.Root bind:open={actionsOpen}>
						<Popover.Trigger>
							{#snippet child({ props })}
								<Button
									{...props}
									variant="plain"
									type="button"
									class={iconButton}
									title={m['wiki.page_actions']()}
									aria-label={m['wiki.page_actions']()}
								>
									<MoreHorizontal size={14} />
								</Button>
							{/snippet}
						</Popover.Trigger>
						<Popover.Content class="w-56 max-w-[calc(100vw-1rem)] p-1" align="end">
							<Button
								variant="plain"
								type="button"
								class="{menuItem} text-red-500"
								onclick={() => {
									actionsOpen = false;
									deleteOpen = true;
								}}
							>
								<Trash2 size={14} />
								{m['wiki.delete']()}
							</Button>
						</Popover.Content>
					</Popover.Root>
				{/if}
				{#if presenceState.activeViewers.length > 0}
					<div class="ml-1 flex items-center -space-x-1.5 border-l border-[var(--app-border)] pl-2">
						{#each presenceState.activeViewers.slice(0, 5) as viewer (viewer.user_id)}
							<div
								class="relative flex h-6 w-6 items-center justify-center rounded-full border-2 border-[var(--color-bg)] text-[8px] font-medium text-white"
								style="background-color: {viewer.color};"
								title={m['wiki.viewing']({ name: viewer.name })}
							>
								{viewer.name.charAt(0).toUpperCase()}
							</div>
						{/each}
					</div>
				{/if}
			</div>
		{/if}
	</div>

	{#if listState === 'loading'}
		<div role="status" aria-label={m['common.loading']()} class="space-y-3 p-6">
			<Skeleton class="h-5 w-48" />
			<Skeleton class="h-4 w-72" />
			<Skeleton class="h-4 w-64" />
		</div>
	{:else if listState === 'denied'}
		<EmptyState title={m['wiki.title']()} description={m['wiki.not_found']()} />
	{:else if listState === 'failed'}
		<EmptyState title={m['wiki.load_failed']()} action={{ label: m['wiki.retry'](), onclick: () => loadList() }} />
	{:else if pages.length === 0 && !registryEnabled}
		<div data-testid="wiki-empty">
			<EmptyState
				title={m['wiki.empty_title']()}
				description={canWrite ? emptyDescription : m['wiki.empty_readonly']()}
				action={canWrite ? { label: m['wiki.create_first'](), onclick: createPage } : undefined}
			/>
		</div>
	{:else}
		<div class="flex min-h-0 flex-1 flex-col md:flex-row">
			<nav
				aria-label={m['wiki.pages']()}
				class="flex max-h-56 shrink-0 flex-col overflow-y-auto border-b border-[var(--app-border)] md:max-h-none md:w-60 md:border-r md:border-b-0"
			>
				{#snippet pageLink(item: WikiPageSummary, Icon: typeof FileText)}
					<li>
						<a
							href="?page={item.id}"
							aria-current={item.id === selectedId ? 'page' : undefined}
							class="flex items-center gap-2 rounded-md px-2 py-1.5 text-sm {item.id === selectedId
								? 'bg-[var(--color-bg-hover)] text-[var(--color-text-primary)]'
								: 'text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]'}"
						>
							<Icon size={14} class="shrink-0 text-[var(--color-text-tertiary)]" />
							<span class="truncate">{item.title}</span>
						</a>
					</li>
				{/snippet}
				<div class="flex items-center justify-between px-4 pt-3 pb-1">
					<h2 class="text-[11px] font-medium tracking-wider text-[var(--color-text-tertiary)] uppercase">
						{m['wiki.pages']()}
					</h2>
					{#if canWrite}
						<Button
							variant="ghost"
							size="icon-sm"
							aria-label={m['wiki.new_page']()}
							title={m['wiki.new_page']()}
							disabled={creating}
							onclick={createPage}
						>
							<Plus size={14} />
						</Button>
					{/if}
				</div>
				<ul class="px-2 pb-2">
					{#each documents as item (item.id)}
						{@render pageLink(item, FileText)}
					{:else}
						<li class="px-2 py-1 text-xs text-[var(--color-text-tertiary)]">{m['wiki.no_pages']()}</li>
					{/each}
				</ul>

				{#if registryEnabled || registry.length > 0 || canWrite}
					<section aria-labelledby="wiki-registry-heading" class="border-t border-[var(--app-border)] pb-2">
						<div class="flex items-center justify-between px-4 pt-3 pb-1">
							<h2
								id="wiki-registry-heading"
								class="text-[11px] font-medium tracking-wider text-[var(--color-text-tertiary)] uppercase"
							>
								{m['wiki.registry']()}
							</h2>
							{#if canWrite}
								<Switch
									checked={registryEnabled}
									disabled={registryBusy}
									aria-label={m['wiki.registry_toggle']()}
									onCheckedChange={(value) => setRegistry(value)}
								/>
							{/if}
						</div>
						{#if registry.length > 0}
							<ul class="px-2">
								{#each registry as item (item.id)}
									{@render pageLink(item, CalendarDays)}
								{/each}
							</ul>
						{:else}
							<p class="px-4 text-xs text-[var(--color-text-tertiary)]">
								{registryEnabled ? m['wiki.registry_empty']() : m['wiki.registry_off']()}
							</p>
						{/if}
					</section>
				{/if}
			</nav>

			<section class="flex min-h-0 min-w-0 flex-1 flex-col">
				{#if pageState === 'loading' && !current}
					<div
						role="status"
						aria-label={m['common.loading']()}
						class="mx-auto w-full max-w-[840px] space-y-3 px-4 py-6 sm:px-10"
					>
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
					{#key `${current.id}/${reloadToken}/${canWrite}`}
						<WikiPageView
							{slug}
							{scope}
							page={current}
							{canWrite}
							{members}
							{issues}
							{uploadUrl}
							{scopeLabel}
							editTitle={justCreated === current.id}
							bind:saveState
							onsaved={onPageSaved}
							onreload={reloadCurrent}
						/>
					{/key}
				{:else}
					<EmptyState
						title={m['wiki.select_page']()}
						action={canWrite ? { label: m['wiki.new_page'](), onclick: createPage } : undefined}
					/>
				{/if}
			</section>
		</div>
	{/if}
</div>

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
