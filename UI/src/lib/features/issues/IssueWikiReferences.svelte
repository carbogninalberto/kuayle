<script lang="ts">
	import { untrack } from 'svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import { attachIssueWikiReference, detachIssueWikiReference, listIssueWikiReferences } from '$lib/api/wiki';
	import type { WikiReference } from '$lib/types/wiki';
	import WikiReferenceChip from '$lib/features/wiki/WikiReferenceChip.svelte';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/features/i18n/messages';
	import { BookOpen, X } from '@lucide/svelte';

	// Wiki pages attached to an issue as references. A page is attached by
	// dragging it from a wiki tree onto the issue (the issue page calls
	// attach); pages this reader cannot see show as placeholders, which
	// anyone who may edit the issue can still remove.
	let { slug, identifier, dropping = false }: { slug: string; identifier: string; dropping?: boolean } = $props();

	let references = $state<WikiReference[]>([]);
	let request = 0;

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
		<h3 class="flex items-center gap-2 text-sm font-medium text-[var(--color-text-secondary)]">
			<BookOpen size={14} class="shrink-0" />
			{m['wiki.reference.pages']()}
		</h3>
		{#if references.length > 0}
			<ul class="mt-1.5 flex flex-wrap gap-1.5">
				{#each references as reference (reference.id)}
					<li class="group/reference flex max-w-full items-center gap-0.5">
						<WikiReferenceChip {slug} pageId={reference.id} {reference} />
						<Button
							variant="plain"
							onclick={() => detach(reference.id)}
							class="shrink-0 rounded p-0.5 text-[var(--color-text-tertiary)] opacity-0 transition-opacity group-focus-within/reference:opacity-100 group-hover/reference:opacity-100 hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]"
							title={m['wiki.reference.detach']()}
							aria-label={m['wiki.reference.detach']()}
						>
							<X size={12} />
						</Button>
					</li>
				{/each}
			</ul>
		{/if}
		{#if dropping}
			<p class="mt-1.5 text-xs text-[var(--color-text-tertiary)]" role="status">{m['wiki.reference.drop_hint']()}</p>
		{/if}
	</section>
{/if}
