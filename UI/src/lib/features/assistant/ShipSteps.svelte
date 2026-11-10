<script lang="ts">
	import { Circle, CircleAlert, CircleCheck, CircleMinus, ExternalLink, LoaderCircle } from '@lucide/svelte';
	import type { ShipPayload, ShipStep } from '$lib/api/conversations';
	import { m } from '$lib/features/i18n/messages';

	// The steps of a ship_pull_request chain in the order they run: what each
	// does, its status and what happened. Listed on the pending card so one
	// approval covers every step, then followed as the server records progress.
	let { payload }: { payload: ShipPayload } = $props();

	function label(step: ShipStep) {
		switch (step.step) {
			case 'merge':
				return m['assistant.ship_step_merge']({
					number: payload.pull_request,
					method: payload.merge.merge_method ?? 'squash',
					commit: payload.merge.commit.slice(0, 7)
				});
			case 'tests':
				return m['assistant.ship_step_tests']({ branch: payload.release.branch });
			case 'release':
				return m['assistant.ship_step_release']({ tag: payload.release.tag });
			case 'build':
				return m['assistant.ship_step_build']();
			case 'upgrade':
				return m['assistant.ship_step_upgrade']({
					current: payload.upgrade?.current_version ?? '',
					latest: payload.upgrade?.latest_version ?? ''
				});
		}
	}
	const statuses: Record<ShipStep['status'], () => string> = {
		pending: m['assistant.ship_status_pending'],
		running: m['assistant.ship_status_running'],
		done: m['assistant.ship_status_done'],
		failed: m['assistant.ship_status_failed'],
		skipped: m['assistant.ship_status_skipped']
	};
	const tone: Record<ShipStep['status'], string> = {
		pending: 'text-muted-foreground',
		running: 'text-foreground',
		done: 'text-[var(--status-success)]',
		failed: 'text-destructive',
		skipped: 'text-muted-foreground'
	};
</script>

<ol class="space-y-1 text-xs" data-testid="ship-steps">
	{#each payload.steps as step (step.step)}
		<li class="flex items-start gap-1.5">
			<span class="mt-0.5 shrink-0 {tone[step.status]}" aria-hidden="true"
				>{#if step.status === 'running'}<LoaderCircle
						size={12}
						class="animate-spin"
					/>{:else if step.status === 'done'}<CircleCheck size={12} />{:else if step.status === 'failed'}<CircleAlert
						size={12}
					/>{:else if step.status === 'skipped'}<CircleMinus size={12} />{:else}<Circle size={12} />{/if}</span
			>
			<div class="min-w-0 space-y-0.5">
				<p class="break-words">
					{label(step)} <span class={tone[step.status]}>· {statuses[step.status]()}</span>
				</p>
				{#if step.detail}<p
						class="break-words {step.status === 'failed' ? 'text-destructive' : 'text-muted-foreground'}"
					>
						{step.detail}
					</p>{/if}
				{#if step.url}<a
						class="inline-flex items-center gap-1 underline underline-offset-2"
						href={step.url}
						target="_blank"
						rel="noopener noreferrer"
						><ExternalLink size={12} />{step.step === 'release'
							? m['assistant.release_open']()
							: m['assistant.ship_open_run']()}</a
					>{/if}
			</div>
		</li>
	{/each}
</ol>
