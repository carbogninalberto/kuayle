<script lang="ts">
	import type { Snippet } from 'svelte';

	let { label, path, children }: { label: string; path: string; children: Snippet } = $props();
</script>

<figure class="overflow-hidden rounded-xl border border-white/10 bg-[#101015] shadow-2xl shadow-black/40">
	<div class="flex items-center gap-2 border-b border-white/10 bg-white/[0.03] px-3 py-2" aria-hidden="true">
		<span class="flex gap-1.5"><span class="size-2.5 rounded-full bg-white/15"></span><span class="size-2.5 rounded-full bg-white/15"></span><span class="size-2.5 rounded-full bg-white/15"></span></span>
		<span class="ml-2 truncate font-mono text-[0.7rem] text-muted-foreground">{path}</span>
	</div>
	<div aria-hidden="true" class="select-none">
		{@render children()}
	</div>
	<figcaption class="sr-only">{label}</figcaption>
</figure>
