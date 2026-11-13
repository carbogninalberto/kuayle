<script lang="ts">
	import { onDestroy } from 'svelte';
	import { Check, Copy } from '@lucide/svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/features/i18n/messages';
	import { cn } from '$lib/utils.js';
	import { copyText } from './message-text';

	// Copies one chat message's whole text. It sits at the top-right of its
	// bubble, which must reserve end padding for it, and stays out of the way
	// until the message is hovered or the button focused; where nothing can
	// hover (touch) it is always shown. The name stays "Copy message"; the
	// result is announced.
	let {
		text,
		class: className = ''
	}: {
		// The text to copy, or how to produce it when the button is pressed.
		text: string | (() => string);
		class?: string;
	} = $props();
	let copied = $state(false);
	let timer: ReturnType<typeof setTimeout> | undefined;
	onDestroy(() => clearTimeout(timer));

	async function copy() {
		const done = await copyText(typeof text === 'function' ? text() : text);
		if (!done) return void appToast.error(m['assistant.copy_failed']());
		copied = true;
		clearTimeout(timer);
		timer = setTimeout(() => (copied = false), 2000);
	}
</script>

<Button
	variant="ghost"
	size="icon-xs"
	data-copied={copied || undefined}
	aria-label={m['assistant.copy']()}
	title={m['assistant.copy']()}
	class={cn(
		'absolute end-1 top-1 z-10 text-muted-foreground opacity-0 transition-opacity group-focus-within/message:opacity-100 group-hover/message:opacity-100 focus-visible:opacity-100 data-[copied]:opacity-100 pointer-coarse:opacity-100',
		className
	)}
	onclick={() => void copy()}
	>{#if copied}<Check aria-hidden="true" />{:else}<Copy aria-hidden="true" />{/if}</Button
><span aria-live="polite" class="sr-only">{copied ? m['tasks.copied']() : ''}</span>
