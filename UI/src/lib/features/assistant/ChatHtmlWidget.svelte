<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import { m } from '$lib/features/i18n/messages';
	import { widgetDocument } from './rich-blocks';

	let { html, onfail }: { html: string; onfail: () => void } = $props();

	// A widget does nothing until the reader asks for it: the frame is created
	// only after the explicit run, so a generated widget never spends DNS or
	// frames before consent.
	let running = $state(false);

	// The srcdoc load fires once. A second load means the widget tried to
	// navigate its own frame; the page's frame-src blocks the request, and this
	// teardown is the backstop that shows the source instead.
	let loads = 0;
	let failed = false;
	const srcdoc = $derived(widgetDocument(html));

	function fail() {
		if (failed) return;
		failed = true;
		onfail();
	}

	function onload() {
		loads += 1;
		if (loads > 1) fail();
	}
</script>

<p class="text-xs text-muted-foreground mb-1">{m['assistant.rich_widget_caption']()}</p>

{#if running}
	<iframe
		title={m['assistant.rich_widget_title']()}
		sandbox="allow-scripts"
		referrerpolicy="no-referrer"
		loading="lazy"
		{srcdoc}
		class="h-[360px] w-full rounded-lg border border-border bg-background"
		onload={onload}
	></iframe>
{:else}
	<div
		class="flex h-[360px] w-full flex-col items-center justify-center gap-3 rounded-lg border border-border bg-background"
	>
		<p class="text-sm font-medium">{m['assistant.rich_widget_title']()}</p>
		<Button variant="outline" size="sm" onclick={() => (running = true)}>
			{m['assistant.rich_widget_run']()}
		</Button>
	</div>
{/if}
