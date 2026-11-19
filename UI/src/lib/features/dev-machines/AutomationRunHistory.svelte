<script lang="ts">
	import { onMount } from 'svelte';
	import { Button } from '$lib/components/ui/button';
	import { automations, type AgentAutomationRun } from '$lib/api/automations';
	import { taskLabel } from '$lib/features/assistant/task-labels';
	import { m } from '$lib/features/i18n/messages';
	import { appToast } from '$lib/features/toast/toast';

	let { slug, ruleId, statusLabel }: {
		slug: string;
		ruleId: string;
		statusLabel: (status: AgentAutomationRun['status']) => string;
	} = $props();
	let runs = $state<AgentAutomationRun[]>([]);
	let busy = $state(false);
	let more = $state(false);
	let failed = $state(false);

	async function load(append = false) {
		if (busy) return;
		busy = true;
		failed = false;
		try {
			const page = await automations.runs(slug, ruleId, append ? runs.at(-1)?.id : undefined);
			runs = append ? [...runs, ...page] : page;
			more = page.length === 50;
		} catch (error) {
			failed = true;
			appToast.apiError(error, m['assistant_custom_tools.runs']());
		} finally {
			busy = false;
		}
	}

	async function replay(run: AgentAutomationRun) {
		if (busy) return;
		busy = true;
		try {
			const updated = await automations.replay(slug, run.id);
			runs = runs.map(item => item.id === run.id ? { ...item, ...updated, error: updated.error, can_replay: false } : item);
		} catch (error) {
			appToast.apiError(error, m['assistant.retry']());
		} finally {
			busy = false;
		}
	}

	onMount(() => { void load(); });
</script>

<section aria-label={m['assistant_custom_tools.runs']()} class="space-y-2 border-t pt-3" aria-busy={busy}>
	<div class="flex items-center justify-between gap-2">
		<h3 class="text-sm font-medium">{m['assistant_custom_tools.runs']()}</h3>
		<Button size="sm" variant="ghost" disabled={busy} onclick={() => void load()}>{m['common.refresh']()}</Button>
	</div>
	{#each runs as run (run.id)}
		{@const workflow = run.workflow_name ?? (run.task_id ? m['workflows.standard']() : m['workflows.choose_default']())}
		{@const version = run.task_version ?? run.pinned_version}
		<div class="space-y-1 border-b pb-2 text-xs last:border-b-0">
			<div class="flex flex-wrap items-baseline justify-between gap-1">
				<a class="font-medium hover:underline" href={'/' + slug + '/issue/' + encodeURIComponent(run.issue_identifier)}>{run.issue_identifier} · {run.issue_title}</a>
				<time datetime={run.created_at} class="text-muted-foreground">{new Date(run.created_at).toLocaleString()}</time>
			</div>
			<p>{statusLabel(run.status)}</p>
			<p>{[workflow, version ? m['workflows.version']({ version }) : '', run.task_step].filter(Boolean).join(' · ')}</p>
			{#if run.task_id}
				<p>{m['workflows.task.pick_task']()} {run.task_id}{run.task_state ? ' · ' + taskLabel(run.task_state) : ''}</p>
			{/if}
			<p class="text-muted-foreground">{m['settings.webhooks.attempts']({ count: run.attempts })}{run.replays ? ' · ' + m['assistant.retry']() + ' × ' + run.replays : ''}</p>
			{#if run.error}<p>{run.error}</p>{/if}
			{#if run.can_replay}<Button size="sm" variant="outline" disabled={busy} onclick={() => void replay(run)}>{m['assistant.retry']()}</Button>{/if}
		</div>
	{:else}
		<p class="text-xs text-muted-foreground">{busy ? m['assistant.loading']() : failed ? m['assistant.error']() : m['assistant_custom_tools.runs_empty']()}</p>
	{/each}
	{#if more}<Button size="sm" variant="outline" disabled={busy} onclick={() => void load(true)}>{m['issuePicker.load_more']()}</Button>{/if}
</section>
