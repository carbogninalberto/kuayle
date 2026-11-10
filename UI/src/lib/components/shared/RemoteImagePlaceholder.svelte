<script lang="ts">
	import { ImageOff } from '@lucide/svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import { m } from '$lib/features/i18n/messages';

	// Stands in for an image from another host until the reader asks for it.
	// Mounted next to the image by remote-images.action.ts.
	let { host, onload, onalways }: { host: string; onload: () => void; onalways: () => void } = $props();

	const label = $derived(m['sharedComponents.remote_image.blocked']({ host }));
</script>

<span
	role="group"
	aria-label={label}
	class="inline-flex max-w-full flex-wrap items-center gap-x-2 gap-y-1 rounded-md border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-2 py-1 align-middle text-xs leading-normal font-normal text-[var(--color-text-secondary)]"
>
	<ImageOff size={14} class="shrink-0 text-[var(--color-text-tertiary)]" aria-hidden="true" />
	<span class="max-w-full min-w-0 truncate">{label}</span>
	<Button variant="outline" size="xs" onclick={onload}>{m['sharedComponents.remote_image.load']()}</Button>
	<Button variant="ghost" size="xs" class="max-w-full" onclick={onalways}>
		<span class="truncate">{m['sharedComponents.remote_image.always']({ host })}</span>
	</Button>
</span>
