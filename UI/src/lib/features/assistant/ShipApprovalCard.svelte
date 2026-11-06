<script lang="ts">
	import { ExternalLink, ShieldCheck } from '@lucide/svelte';
	import * as Item from '$lib/components/ui/item/index.js';
	import type { ShipPayload } from '$lib/api/conversations';
	import { m } from '$lib/paraglide/messages.js';
	import MergeHeadChange from './MergeHeadChange.svelte';
	import ReleaseNotesField from './ReleaseNotesField.svelte';
	import ShipSteps from './ShipSteps.svelte';

	// The pending ship_pull_request card: one approval for every step listed,
	// what the merge covers, and the release notes, editable until approved.
	let {
		id,
		payload,
		notes = $bindable(),
		disabled = false
	}: { id: string; payload: ShipPayload; notes?: string; disabled?: boolean } = $props();
</script>

<Item.Title
	>{m['assistant.ship_title']({
		identifier: payload.identifier,
		number: payload.pull_request,
		tag: payload.release.tag,
		repository: payload.repository
	})}</Item.Title
>
<Item.Description class="line-clamp-none">{m['assistant.ship_description']({ title: payload.title })}</Item.Description>
<ShipSteps {payload} />
{#if payload.merge.head_change}<MergeHeadChange
		change={payload.merge.head_change}
		publishedCommit={payload.merge.published_commit}
	/>{/if}
<p class="flex items-start gap-1 text-xs text-muted-foreground">
	<ShieldCheck size={12} class="mt-0.5 shrink-0" />{payload.release.previous_tag
		? m['assistant.ship_release_range']({ previous: payload.release.previous_tag })
		: m['assistant.release_first']()}
</p>
<ReleaseNotesField {id} draft={payload.release.notes} bind:notes {disabled} />
<a
	class="inline-flex items-center gap-1 text-xs underline underline-offset-2"
	href={payload.merge.pull_request_url}
	target="_blank"
	rel="noopener noreferrer"><ExternalLink size={12} />{m['tasks.open_pull_request']()}</a
>
