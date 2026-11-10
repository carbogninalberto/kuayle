<script lang="ts" module>
	// The server refuses longer notes; the card says so before approving.
	export const maxReleaseNotes = 20000;

	// Whether notes cannot be approved: empty, or longer than the server allows.
	export function releaseNotesInvalid(notes: string) {
		return !notes.trim() || [...notes].length > maxReleaseNotes;
	}
</script>

<script lang="ts">
	import { Textarea } from '$lib/components/ui/textarea/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { m } from '$lib/features/i18n/messages';

	// Release notes on a pending card (create_release or ship_pull_request),
	// editable until approved. `notes` is the edited text (undefined while it is
	// the assistant's draft); the server checks it again before publishing it.
	let {
		id,
		draft,
		notes = $bindable(),
		disabled = false
	}: { id: string; draft: string; notes?: string; disabled?: boolean } = $props();

	const value = $derived(notes ?? draft);
	const length = $derived([...value].length);
	const inputId = $derived(`release-notes-${id}`);
</script>

<div class="mt-1 flex items-center justify-between gap-2">
	<Label for={inputId} class="text-xs font-medium text-muted-foreground">{m['assistant.release_notes_label']()}</Label>
	{#if notes !== undefined && notes !== draft}
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
<p id="{inputId}-count" class="text-xs {releaseNotesInvalid(value) ? 'text-destructive' : 'text-muted-foreground'}">
	{m['assistant.release_notes_count']({ count: length, max: maxReleaseNotes })}
</p>
