<script lang="ts">
	import type { IssueStatus } from '$lib/types/issue';
	import { Circle, CircleDot, Loader, Eye, CheckCircle2, XCircle } from 'lucide-svelte';

	let { status, size = 16 }: { status: IssueStatus; size?: number } = $props();

	const icons: Record<IssueStatus, any> = {
		backlog: Circle,
		todo: CircleDot,
		in_progress: Loader,
		in_review: Eye,
		done: CheckCircle2,
		cancelled: XCircle
	};

	const colors: Record<IssueStatus, string> = {
		backlog: 'text-[var(--color-text-tertiary)]',
		todo: 'text-[var(--color-text-secondary)]',
		in_progress: 'text-yellow-500',
		in_review: 'text-blue-500',
		done: 'text-[var(--color-success)]',
		cancelled: 'text-[var(--color-text-tertiary)]'
	};
</script>

<span class={colors[status]}>
	<svelte:component this={icons[status]} {size} />
</span>
