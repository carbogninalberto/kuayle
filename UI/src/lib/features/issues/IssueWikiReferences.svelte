<script lang="ts">
	import { untrack } from 'svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import * as Command from '$lib/components/ui/command/index.js';
	import {
		attachIssueWikiReference,
		detachIssueWikiReference,
		listIssueWikiReferences,
		searchWikiReferences
	} from '$lib/api/wiki';
	import { getCurrentWorkspace } from '$lib/features/workspaces/current-workspace.svelte';
	import { hasPermission } from '$lib/security/permissions';
	import type { Role } from '$lib/security/roles';
	import type { WikiReference } from '$lib/types/wiki';
	import WikiReferenceChip from '$lib/features/wiki/WikiReferenceChip.svelte';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/features/i18n/messages';
	import { BookOpen, FileText, Plus, X } from '@lucide/svelte';

	// Wiki pages attached to an issue as references. A page is attached by
	// searching for it here (openPicker, also reachable from the issue's
	// actions) or by dragging it from a wiki tree onto the issue (the issue
	// page calls attach). Pages this reader cannot see show as placeholders,
	// which anyone who may edit the issue can still remove. The controls
	// follow the role; the server decides.
	let { slug, identifier, dropping = false }: { slug: string; identifier: string; dropping?: boolean } = $props();

	let references = $state<WikiReference[]>([]);
	let request = 0;

	const workspace = getCurrentWorkspace();
	const canUpdate = $derived(
		hasPermission((workspace?.membership?.workspace.current_user_role ?? 'guest') as Role, 'issue:update')
	);

	// The page picker: a search over every wiki page the reader can see.
	let pickerOpen = $state(false);
	let query = $state('');
	let found = $state<WikiReference[]>([]);
	let searching = $state(false);
	const attachedIds = $derived(new Set(references.map((reference) => reference.id)));
	const choices = $derived(found.filter((page) => !attachedIds.has(page.id)));
	// Results arrive after the list is drawn, so the highlighted one is kept
	// on a page that is still listed: Enter always has something to choose.
	let highlighted = $state('');
	$effect(() => {
		if (!choices.some((page) => page.id === highlighted)) highlighted = choices[0]?.id ?? '';
	});

	/** Opens the page search, for people who may edit the issue. */
	export function openPicker() {
		if (canUpdate) pickerOpen = true;
	}

	$effect(() => {
		if (!pickerOpen) {
			query = '';
			found = [];
			return;
		}
		const text = query.trim();
		let stale = false;
		searching = true;
		const timer = setTimeout(
			() =>
				void searchWikiReferences(slug, text, 20)
					.then((pages) => {
						if (!stale) found = Array.isArray(pages) ? pages : [];
					})
					.catch(() => {
						if (!stale) found = [];
					})
					.finally(() => {
						if (!stale) searching = false;
					}),
			text ? 150 : 0
		);
		return () => {
			stale = true;
			clearTimeout(timer);
		};
	});

	function choose(pageId: string) {
		pickerOpen = false;
		void attach(pageId);
	}

	$effect(() => {
		const issue = identifier;
		const current = ++request;
		untrack(() => (references = []));
		listIssueWikiReferences(slug, issue)
			.then((loaded) => {
				if (current === request) references = Array.isArray(loaded) ? loaded : [];
			})
			.catch(() => {
				/* The section stays hidden; attaching reports its own errors. */
			});
	});

	/** Attaches a page to the issue; the server checks access to both. */
	export async function attach(pageId: string) {
		const current = ++request;
		try {
			const attached = await attachIssueWikiReference(slug, identifier, pageId);
			if (current === request) references = Array.isArray(attached) ? attached : [];
		} catch (error) {
			appToast.apiError(error, m['wiki.reference.update_failed']());
		}
	}

	async function detach(pageId: string) {
		try {
			await detachIssueWikiReference(slug, identifier, pageId);
			references = references.filter((reference) => reference.id !== pageId);
		} catch (error) {
			appToast.apiError(error, m['wiki.reference.update_failed']());
		}
	}
</script>

{#if references.length > 0 || dropping}
	<section
		aria-label={m['wiki.reference.pages']()}
		data-testid="issue-wiki-references"
		class="rounded-lg border bg-[var(--color-bg-secondary)]/60 px-3 py-2 {dropping
			? 'border-dashed border-[var(--app-accent)]'
			: 'border-[var(--app-border)]'}"
	>
		<div class="flex items-center gap-2">
			<h3 class="flex min-w-0 flex-1 items-center gap-2 text-sm font-medium text-[var(--color-text-secondary)]">
				<BookOpen size={14} class="shrink-0" />
				{m['wiki.reference.pages']()}
			</h3>
			{#if canUpdate}
				<Button
					variant="plain"
					onclick={openPicker}
					class="shrink-0 rounded p-1 text-[var(--color-text-tertiary)] transition-colors hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]"
					title={m['wiki.reference.add']()}
					aria-label={m['wiki.reference.add']()}
				>
					<Plus size={14} />
				</Button>
			{/if}
		</div>
		{#if references.length > 0}
			<ul class="mt-1.5 flex flex-wrap gap-1.5">
				{#each references as reference (reference.id)}
					<li class="group/reference flex max-w-full items-center gap-0.5">
						<WikiReferenceChip {slug} pageId={reference.id} {reference} />
						{#if canUpdate}
						<Button
							variant="plain"
							onclick={() => detach(reference.id)}
							class="shrink-0 rounded p-0.5 text-[var(--color-text-tertiary)] opacity-0 transition-opacity group-focus-within/reference:opacity-100 group-hover/reference:opacity-100 hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]"
							title={m['wiki.reference.detach']()}
							aria-label={m['wiki.reference.detach']()}
						>
							<X size={12} />
						</Button>
						{/if}
					</li>
				{/each}
			</ul>
		{/if}
		{#if dropping}
			<p class="mt-1.5 text-xs text-[var(--color-text-tertiary)]" role="status">{m['wiki.reference.drop_hint']()}</p>
		{/if}
	</section>
{/if}

<Command.Dialog
	bind:open={pickerOpen}
	bind:value={highlighted}
	shouldFilter={false}
	title={m['wiki.reference.add']()}
	description={m['wiki.reference.search']()}
>
	<Command.Input bind:value={query} placeholder={m['wiki.reference.search']()} />
	<Command.List>
		{#if searching && choices.length === 0}
			<Command.Loading class="py-4 text-center text-xs text-[var(--color-text-tertiary)]">
				{m['common.loading']()}
			</Command.Loading>
		{:else}
			<Command.Empty>{m['common.no_results']()}</Command.Empty>
		{/if}
		{#each choices as page (page.id)}
			<Command.Item value={page.id} onSelect={() => choose(page.id)}>
				<FileText size={14} class="shrink-0 text-[var(--color-text-tertiary)]" />
				<span class="min-w-0 flex-1 truncate">{page.title}</span>
				<span class="max-w-[40%] shrink-0 truncate text-xs text-[var(--color-text-tertiary)]">{page.scope_name}</span>
			</Command.Item>
		{/each}
	</Command.List>
</Command.Dialog>
