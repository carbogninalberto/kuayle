<script lang="ts">
	import { Button } from '$lib/components/ui/button/index.js';
	import { m } from '$lib/features/i18n/messages';
	import type { ScheduledJob } from '$lib/api/conversations';

	let { job, oncancel }: { job: ScheduledJob; oncancel: () => Promise<void> } = $props();
	let cancelling = $state(false);
	let failed = $state(false);

	async function cancel() {
		cancelling = true;
		failed = false;
		try {
			await oncancel();
		} catch {
			failed = true;
		} finally {
			cancelling = false;
		}
	}
</script>

<section aria-label={m['assistant.scheduled_job']()} class="my-2 max-w-prose rounded-md border px-3 py-2 text-sm">
	<div class="flex items-start justify-between gap-3">
		<p class="min-w-0 whitespace-pre-wrap break-words font-medium">{job.instructions}</p>
		{#if job.state === 'scheduled'}
			<Button variant="outline" size="sm" disabled={cancelling} onclick={cancel}>{m['common.cancel']()}</Button>
		{/if}
	</div>
	{#if job.state === 'scheduled'}
		<p class="mt-1 text-muted-foreground">{m['settings.backups.next_run']({ time: new Date(job.next_run_at).toLocaleString(undefined, { timeZone: job.timezone, year: 'numeric', month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit', timeZoneName: 'short' }) })}</p>
	{/if}
	<p class="mt-1 break-words text-muted-foreground" aria-live="polite">{job.last_outcome}{job.last_error ? ` · ${job.last_error}` : ''}</p>
	{#if failed}<p role="alert" class="mt-1 text-destructive">{m['assistant.error']()}</p>{/if}
</section>
