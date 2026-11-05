<script lang="ts" module>
	// The server refuses longer notes; the card says so before approving.
	export const maxReleaseNotes = 20000;
</script>

<script lang="ts">
	import { GitBranch, ShieldCheck, TriangleAlert } from '@lucide/svelte';
	import * as Item from '$lib/components/ui/item/index.js';
	import { Textarea } from '$lib/components/ui/textarea/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import type { ReleasePayload } from '$lib/api/conversations';
	import { m } from '$lib/paraglide/messages.js';

	// The pending create_release card: what is tagged, what the gate and the
	// secret scan found, and the notes, editable until approved. `notes` is the
	// edited text (undefined while it is the assistant's draft); the server
	// checks it again before publishing it.
	let {
		id,
		payload,
		notes = $bindable(),
		disabled = false
	}: { id: string; payload: ReleasePayload; notes?: string; disabled?: boolean } = $props();

	const value = $derived(notes ?? payload.notes);
	const length = $derived([...value].length);
	const inputId = $derived(`release-notes-${id}`);
</script>

<Item.Title>{m['assistant.release_title']({ tag: payload.tag, repository: payload.repository })}</Item.Title>
<Item.Description class="line-clamp-none"
	>{m['assistant.release_description']({
		commit: payload.target_commit.slice(0, 7),
		branch: payload.branch,
		checks: payload.checks_passed
	})}</Item.Description
>
<p class="flex items-start gap-1 text-xs text-muted-foreground" data-testid="release-scan">
	<ShieldCheck size={12} class="mt-0.5 shrink-0" />{payload.previous_tag
		? m['assistant.release_range']({
				previous: payload.previous_tag,
				commits: payload.commits,
				files: payload.scanned_files
			})
		: m['assistant.release_first']()}
</p>
{#if payload.unscanned_files?.length}
	<p class="flex items-start gap-1 text-xs break-all text-[var(--status-warning)]">
		<TriangleAlert size={12} class="mt-0.5 shrink-0" />{m['assistant.release_unscanned']({
			files: payload.unscanned_files.join(', ')
		})}
	</p>
{/if}
<div class="mt-1 flex items-center justify-between gap-2">
	<Label for={inputId} class="text-xs font-medium text-muted-foreground">{m['assistant.release_notes_label']()}</Label>
	{#if notes !== undefined && notes !== payload.notes}
		<Button size="sm" variant="ghost" class="h-6 px-2 text-xs" {disabled} onclick={() => (notes = undefined)}
			>{m['assistant.release_notes_reset']()}</Button
		>
	{/if}
</div>
<Textarea
	id={inputId}
	{value}
	{disabled}
	rows={12}
	spellcheck="true"
	class="max-h-96 min-h-48 font-mono text-xs"
	aria-describedby="{inputId}-count"
	oninput={(event) => (notes = event.currentTarget.value)}
/>
<p
	id="{inputId}-count"
	class="text-xs {length > maxReleaseNotes || !value.trim() ? 'text-destructive' : 'text-muted-foreground'}"
>
	{m['assistant.release_notes_count']({ count: length, max: maxReleaseNotes })}
</p>
{#if payload.compare_url}<a
		class="inline-flex items-center gap-1 text-xs underline underline-offset-2"
		href={payload.compare_url}
		target="_blank"
		rel="noopener noreferrer"><GitBranch size={12} />{m['assistant.release_compare']()}</a
	>{/if}
