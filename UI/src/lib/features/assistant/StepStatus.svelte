<script lang="ts">
	import { CircleAlert, CircleCheck, CircleDot, CirclePause, CircleSlash, LoaderCircle } from '@lucide/svelte';
	import type { StepStatus } from './agent-timeline';
	import { m } from '$lib/features/i18n/messages';

	// How a step went, as an icon in its colour; the name is read out.
	let { status, size = 15 }: { status: StepStatus; size?: number } = $props();
	const label: Record<StepStatus, () => string> = {
		running: m['workflows.task.status_running'],
		waiting: m['workflows.task.status_waiting'],
		done: m['workflows.task.status_done'],
		changes: m['tasks.review_verdict_request_changes'],
		failed: m['workflows.task.status_failed'],
		stopped: m['assistant.cancelled']
	};
	const tone: Record<StepStatus, string> = {
		running: 'text-[var(--status-running)]',
		waiting: 'text-[var(--status-warning)]',
		done: 'text-[var(--status-success)]',
		changes: 'text-[var(--status-warning)]',
		failed: 'text-[var(--status-error)]',
		stopped: 'text-muted-foreground'
	};
</script>

<span role="img" aria-label={label[status]()} class="inline-flex shrink-0 {tone[status]}">
	{#if status === 'running'}<LoaderCircle {size} class="animate-spin motion-reduce:animate-none" />
	{:else if status === 'waiting'}<CirclePause {size} />
	{:else if status === 'done'}<CircleCheck {size} />
	{:else if status === 'changes'}<CircleDot {size} />
	{:else if status === 'failed'}<CircleAlert {size} />
	{:else}<CircleSlash {size} />{/if}
</span>
