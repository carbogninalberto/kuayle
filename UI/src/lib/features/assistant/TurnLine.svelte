<script lang="ts">
	import type { Snippet } from 'svelte';
	import { ChevronRight } from '@lucide/svelte';
	import StepStatus from './StepStatus.svelte';
	import type { StepStatus as Status } from './agent-timeline';
	import { getLocale, m } from '$lib/features/i18n/messages';

	// One line of a task's thread (KUA-286): who worked (a role, or a step
	// and who ran it), what it ended with, how long it took, the tokens it
	// worked through and how it went. It is the inside of a disclosure's
	// trigger, so the line stays one control; what it opens to (the model,
	// the harness, the session, the transcript) is the caller's.
	let {
		role,
		who = '',
		summary = '',
		duration = '',
		startedAt,
		tokens,
		status,
		visit = 1,
		marks
	}: {
		role: string;
		who?: string;
		summary?: string;
		duration?: string;
		// When it started, for the duration's time element.
		startedAt?: string;
		tokens?: number;
		status?: Status;
		visit?: number;
		// What the event itself says besides its text (a verdict, a question).
		marks?: Snippet;
	} = $props();

	const compact = $derived(new Intl.NumberFormat(getLocale(), { notation: 'compact', maximumFractionDigits: 1 }));
</script>

<span class="min-w-0 flex-1 truncate text-sm"
	><span class="font-medium">{role}</span>{#if who}<span class="text-muted-foreground">{` · ${who}`}</span
		>{/if}{#if summary}<span class="text-muted-foreground" data-summary>{` — ${summary}`}</span>{/if}</span
>
{@render marks?.()}
{#if visit > 1}<span class="shrink-0 text-xs text-muted-foreground tabular-nums">×{visit}</span>{/if}
{#if duration}<time datetime={startedAt} class="shrink-0 text-xs text-muted-foreground tabular-nums">{duration}</time>{/if}
{#if tokens !== undefined}<span class="hidden shrink-0 text-xs text-muted-foreground tabular-nums sm:inline" data-tokens
		>{m['tasks.row_tokens']({ count: compact.format(tokens) })}</span
	>{/if}
{#if status}<StepStatus {status} />{/if}
<ChevronRight
	size={14}
	aria-hidden="true"
	class="shrink-0 text-muted-foreground/60 transition-transform group-aria-expanded:rotate-90 motion-reduce:transition-none"
/>
