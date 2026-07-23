<script lang="ts">
	import type { DevMachineStatus } from '$lib/types/dev-machine';
	import { Badge } from '$lib/components/ui/badge';

	let { status }: { status: DevMachineStatus } = $props();

	const label = $derived(status.replaceAll('_', ' '));
	const style = $derived(
		status === 'running'
			? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-400'
			: status === 'failed' || status === 'expired'
				? 'border-red-500/30 bg-red-500/10 text-red-400'
				: status === 'paused' || status === 'stopped'
					? 'border-amber-500/30 bg-amber-500/10 text-amber-400'
					: status === 'destroyed'
						? 'border-[var(--app-border)] bg-[var(--color-bg-tertiary)] text-[var(--color-text-tertiary)]'
						: 'border-blue-500/30 bg-blue-500/10 text-blue-400'
	);
</script>

<Badge variant="outline" class="rounded-full px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide {style}">{label}</Badge>
