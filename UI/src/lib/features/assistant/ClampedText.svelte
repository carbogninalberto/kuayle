<script lang="ts">
	import { Button } from '$lib/components/ui/button/index.js';
	import { m } from '$lib/features/i18n/messages';

	// Long plain text (a prompt) shown by its first lines, with the rest one
	// click away in place. The text is untrusted and only ever escaped.
	let { text, lines = 3, class: className = '' }: { text: string; lines?: 3 | 6; class?: string } = $props();
	let open = $state(false),
		cut = $state(false),
		body = $state<HTMLParagraphElement | null>(null);
	const clamp = { 3: 'line-clamp-3', 6: 'line-clamp-6' };
	// Whether the clamp hides anything is only known once it is laid out.
	$effect(() => {
		void text;
		const node = body;
		if (!node || open) return;
		const measure = () => (cut = node.scrollHeight > node.clientHeight + 1);
		measure();
		const observer = new ResizeObserver(measure);
		observer.observe(node);
		return () => observer.disconnect();
	});
</script>

<div class="min-w-0">
	<p bind:this={body} class="break-words whitespace-pre-wrap {open ? '' : clamp[lines]} {className}">{text}</p>
	{#if cut || open}<Button
			variant="link"
			size="xs"
			class="h-auto px-0 py-0.5 text-muted-foreground"
			aria-expanded={open}
			onclick={() => (open = !open)}
			>{open ? m['tasks.transcript_show_less']() : m['sharedComponents.create_issue.expand']()}</Button
		>{/if}
</div>
