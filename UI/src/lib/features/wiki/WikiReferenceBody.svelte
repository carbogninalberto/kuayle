<script lang="ts">
	import { FileText, Lock } from '@lucide/svelte';
	import { m } from '$lib/features/i18n/messages';
	import type { WikiReference } from './wiki-references';

	// The inside of a wiki reference chip: an icon, the page title and its
	// team or project. A page the reader cannot see shows a placeholder; its
	// title never reaches the browser. `reference` is null while resolving.
	let { reference }: { reference: WikiReference | null } = $props();
</script>

<span class="mention-status-icon" aria-hidden="true">
	{#if reference && !reference.accessible}<Lock size={12} />{:else}<FileText size={12} />{/if}
</span>
{#if !reference}
	<span class="mention-wiki-title" aria-hidden="true">…</span>
{:else if reference.accessible}
	<span class="mention-wiki-title">{reference.title}</span>
	{#if reference.scope_name}<span class="mention-wiki-scope">{reference.scope_name}</span>{/if}
{:else}
	<span class="mention-wiki-title">{m['wiki.reference.unavailable']()}</span>
{/if}
