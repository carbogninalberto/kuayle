<script lang="ts">
	import Check from '@lucide/svelte/icons/check';
	import CircleDashed from '@lucide/svelte/icons/circle-dashed';
	import Minus from '@lucide/svelte/icons/minus';
	import type { Mark } from '$lib/data/competitor-types';

	let { mark }: { mark: Mark } = $props();

	const config = $derived(
		{
			yes: { label: 'Yes', icon: Check, tone: 'text-emerald-300 border-emerald-400/30 bg-emerald-400/10' },
			partial: { label: 'In part', icon: CircleDashed, tone: 'text-amber-200 border-amber-300/30 bg-amber-300/10' },
			no: { label: 'No', icon: Minus, tone: 'text-zinc-300 border-white/15 bg-white/5' }
		}[mark]
	);
</script>

<span
	class="inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-xs font-medium whitespace-nowrap {config.tone}"
	data-mark={mark}
>
	<config.icon class="size-3" aria-hidden="true" />
	{config.label}
</span>
