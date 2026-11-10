<script lang="ts">
	import { m } from '$lib/features/i18n/messages';
	import WikiReferenceBody from './WikiReferenceBody.svelte';
	import { wikiReferences } from './wiki-reference-store';
	import { wikiReferenceHref, type WikiReference } from './wiki-references';

	// A reference to a wiki page outside editor content (an issue's attached
	// pages, a chat message): the same chip as an editor mention. Pass
	// `reference` when the caller already resolved it; otherwise the chip
	// resolves `pageId` itself, through the reader's access.
	let { slug, pageId, reference }: { slug: string; pageId: string; reference?: WikiReference } = $props();

	let resolved = $state<WikiReference | null>(null);
	$effect(() => {
		const id = pageId;
		if (reference) return;
		resolved = null;
		void wikiReferences.resolve(slug, id).then((found) => {
			if (id === pageId) resolved = found;
		});
	});
	const shown = $derived(reference ?? resolved);
	const href = $derived(shown ? wikiReferenceHref(slug, shown) : null);
</script>

{#if shown && href}
	<a
		class="mention mention-wiki"
		{href}
		title={m['wiki.reference.open']({ title: shown.title ?? '' })}
		data-testid="wiki-reference"><WikiReferenceBody reference={shown} /></a
	>
{:else}
	<span
		class="mention mention-wiki {shown ? 'mention-unavailable' : ''}"
		title={shown ? m['wiki.reference.unavailable_hint']() : undefined}
		data-testid="wiki-reference"><WikiReferenceBody reference={shown} /></span
	>
{/if}
