<script lang="ts">
	import type { RunUsage } from '$lib/types/dev-machine';
	import { getLocale, m } from '$lib/features/i18n/messages';
	import { usageParts, type UsageKind } from './run-usage';

	// What a run, or a task over all its runs, spent as its harness reported
	// it: one quiet line, and nothing when the harness reported nothing.
	let { usage, label }: { usage: RunUsage | null | undefined; label: string } = $props();

	const text: Record<UsageKind, (value: string) => string> = {
		input: (count) => m['tasks.usage_input']({ count }),
		output: (count) => m['tasks.usage_output']({ count }),
		cache_read: (count) => m['tasks.usage_cache_read']({ count }),
		cache_write: (count) => m['tasks.usage_cache_write']({ count }),
		cost: (cost) => m['tasks.usage_cost']({ cost })
	};
	const parts = $derived(usageParts(usage, getLocale()).map((part) => text[part.kind](part.value)));
</script>

{#if parts.length}<p class="text-xs break-words text-muted-foreground tabular-nums" data-testid="usage-line">
		{label} · {parts.join(' · ')}
	</p>{/if}
