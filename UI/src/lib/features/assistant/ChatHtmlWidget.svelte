<script lang="ts">
	import { m } from '$lib/features/i18n/messages';
	import { widgetDocument } from './rich-blocks';

	let { html, onfail }: { html: string; onfail: () => void } = $props();

	// The srcdoc load fires once. A second load means the widget navigated its
	// own frame; CSP cannot stop that, so tear it down and show the source.
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

<iframe
	title={m['assistant.rich_widget_title']()}
	sandbox="allow-scripts"
	referrerpolicy="no-referrer"
	loading="lazy"
	{srcdoc}
	class="h-[360px] w-full rounded-lg border border-border bg-background"
	onload={onload}
></iframe>
