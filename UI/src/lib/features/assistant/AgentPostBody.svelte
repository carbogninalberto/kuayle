<script lang="ts">
	import { Button } from '$lib/components/ui/button/index.js';
	import { m } from '$lib/features/i18n/messages';
	import Markdown from './Markdown.svelte';

	let { source }: { source: string } = $props();
	const id = $props.id();
	const previewHeight = 256;
	let expanded = $state(false);
	let contentHeight = $state(0);
	const long = $derived(contentHeight > previewHeight);
</script>

<div
	{id}
	class={['min-w-0', long && !expanded && 'mask-b-from-[calc(100%-1rem)] mask-b-to-100%']}
	class:overflow-clip={!expanded}
	style:max-height={expanded ? undefined : `${previewHeight}px`}
	onfocusin={(event) => {
		// Tabbing to a link below the preview should reveal it.
		if (
			long && !expanded && event.target instanceof HTMLElement &&
			event.target.getBoundingClientRect().bottom > event.currentTarget.getBoundingClientRect().bottom
		) {
			expanded = true;
		}
	}}
>
	<div bind:clientHeight={contentHeight} class="flow-root">
		<Markdown {source} />
	</div>
</div>
{#if long}
	<Button
		variant="link"
		size="xs"
		class="h-auto px-0 py-1 text-muted-foreground"
		aria-controls={id}
		aria-expanded={expanded}
		onclick={() => (expanded = !expanded)}
	>{expanded ? m['assistant.show_less']() : m['assistant.post_show_more']()}</Button>
{/if}
