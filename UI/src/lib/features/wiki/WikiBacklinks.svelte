<script lang="ts">
	import { onMount } from 'svelte';
	import { getWikiBacklinks } from '$lib/api/wiki';
	import type { WikiBacklinks, WikiScope } from '$lib/types/wiki';
	import { m } from '$lib/features/i18n/messages';
	import { FileText, SquareDot } from '@lucide/svelte';
	import { wikiReferenceHref } from './wiki-references';

	// Where a page is referenced: issues (description, comments or attached)
	// and other pages. The server lists only what this reader can see.
	let { slug, scope, pageId }: { slug: string; scope: WikiScope; pageId: string } = $props();

	let links = $state<WikiBacklinks | null>(null);
	let failed = $state(false);
	let request = 0;
	const BACKLINKS_REFRESH_DELAY = 1000;

	async function load() {
		const current = ++request;
		try {
			const loaded = await getWikiBacklinks(slug, scope, pageId);
			if (current !== request) return;
			links = loaded;
			failed = false;
		} catch {
			if (current === request) failed = links === null;
		}
	}

	onMount(() => {
		void load();
		// References appear and disappear as pages, issues and comments change.
		// Those events arrive in bursts (every autosave of any page), so one
		// reload follows the last of them, and none while the tab is hidden.
		let timer: ReturnType<typeof setTimeout> | null = null;
		let pending = false;
		const refresh = (event?: Event) => {
			// Editing this page does not change what references it.
			if (event?.type === 'ws:wiki-changed' && (event as CustomEvent<{ id?: string }>).detail?.id === pageId) return;
			// A page attached to or removed from an issue matters only when it is this one.
			if (
				event?.type === 'ws:issue-wiki-references' &&
				(event as CustomEvent<{ page_id?: string }>).detail?.page_id !== pageId
			)
				return;
			if (document.hidden) {
				pending = true;
				return;
			}
			if (timer) clearTimeout(timer);
			timer = setTimeout(() => {
				timer = null;
				void load();
			}, BACKLINKS_REFRESH_DELAY);
		};
		const shown = () => {
			if (document.hidden || !pending) return;
			pending = false;
			refresh();
		};
		const events = [
			'ws:wiki-changed',
			'ws:issue-updated',
			'ws:issue-wiki-references',
			'ws:comment-created',
			'ws:reconnected'
		];
		for (const name of events) window.addEventListener(name, refresh);
		document.addEventListener('visibilitychange', shown);
		return () => {
			request++;
			if (timer) clearTimeout(timer);
			for (const name of events) window.removeEventListener(name, refresh);
			document.removeEventListener('visibilitychange', shown);
		};
	});

	const row =
		'flex items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-secondary)] transition-colors hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]';
</script>

<div class="px-1.5 pb-2" data-testid="wiki-backlinks">
	{#if failed}
		<p class="px-2 pb-1 text-xs text-[var(--color-text-tertiary)]">{m['wiki.backlinks.failed']()}</p>
	{:else if !links}
		<p class="px-2 pb-1 text-xs text-[var(--color-text-tertiary)]" role="status">{m['common.loading']()}</p>
	{:else if links.issues.length === 0 && links.pages.length === 0}
		<p class="px-2 pb-1 text-xs text-[var(--color-text-tertiary)]">{m['wiki.backlinks.empty']()}</p>
	{:else}
		<ul>
			{#each links.issues as issue (issue.id)}
				<li>
					<a href="/{slug}/issue/{issue.identifier}" class={row}>
						<SquareDot size={14} class="shrink-0 text-[var(--color-text-tertiary)]" />
						<span class="shrink-0 text-xs font-medium text-[var(--color-text-tertiary)]">{issue.identifier}</span>
						<span class="min-w-0 flex-1 truncate">{issue.title}</span>
					</a>
				</li>
			{/each}
			{#each links.pages as page (page.id)}
				<li>
					<a href={wikiReferenceHref(slug, page) ?? '#'} class={row}>
						<FileText size={14} class="shrink-0 text-[var(--color-text-tertiary)]" />
						<span class="min-w-0 flex-1 truncate">{page.title}</span>
						<span class="max-w-[40%] shrink-0 truncate text-xs text-[var(--color-text-tertiary)]">{page.scope_name}</span>
					</a>
				</li>
			{/each}
		</ul>
	{/if}
</div>
