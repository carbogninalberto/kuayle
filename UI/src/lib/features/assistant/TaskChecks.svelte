<script lang="ts">
	import { untrack } from 'svelte';
	import { CircleAlert, CircleCheck, CircleDashed, CircleMinus, CirclePause, RefreshCw } from '@lucide/svelte';
	import * as AlertDialog from '$lib/components/ui/alert-dialog/index.js';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { agentTasks, type AgentTask } from '$lib/api/agent-tasks';
	import { workflows } from '$lib/api/workflows';
	import { authState } from '$lib/features/auth/auth.state.svelte';
	import { appToast } from '$lib/features/toast/toast';
	import { pendingDecision } from '$lib/features/workflows/workflow-graph';
	import type { ConversationState } from './conversation-state.svelte';
	import ClampedText from './ClampedText.svelte';
	import { useTaskRun } from './task-run.svelte';
	import { pipelineChecks, type CheckNote, type CheckRow, type CheckState } from './task-checks';
	import { getLocale, m } from '$lib/features/i18n/messages';

	// The Checks tab of a task on a pipeline (KUA-286): each check of each
	// phase, how it stands for the head, Kuayle's reason and since when, as
	// the room says (task-checks). The task's creator approves the head where
	// a phase waits for it, through the workflow decision the tick takes, and
	// waives a required check that blocks, as a waiver posted to the room
	// through the creator's own session route; the server decides whether
	// either is allowed and the tick acts on it. Mounted per task.
	let { chat, task }: { chat: ConversationState; task: AgentTask } = $props();
	const run = useTaskRun(untrack(() => chat), () => task);
	const pipeline = $derived(run.pipeline);
	const room = $derived(chat.taskRooms[task.id]);
	const phase = $derived(task.workflow_node || run.view?.node);
	const done = $derived(task.state === 'completed' || run.view?.status === 'done');
	const checks = $derived(pipeline && room ? pipelineChecks(pipeline, room, phase, done) : null);
	const groups = $derived.by(() => {
		const byPhase = new Map<string, CheckRow[]>();
		for (const row of checks?.rows ?? []) byPhase.set(row.phase.id, [...(byPhase.get(row.phase.id) ?? []), row]);
		return [...byPhase.values()];
	});
	const short = $derived(checks?.head.slice(0, 7) ?? '');

	// The server decides who may approve or waive; this offers it to the
	// task's creator while the task can still use it.
	const mine = $derived(!!authState.user && task.creator_id === authState.user.id);
	const active = $derived(!['completed', 'cancelled', 'cancelling', 'failed'].includes(task.state));
	const decision = $derived(run.view ? pendingDecision(run.view) : null);
	const approvable = (row: CheckRow) =>
		row.check.fact === 'approved' && row.state !== 'met' && row.phase.id === phase && decision?.kind === 'approval';
	const waivable = (row: CheckRow) =>
		mine && active && !!row.check.required && (row.state === 'unmet' || row.state === 'waiting' || row.state === 'unknown');

	let busy = $state(false);
	let disposed = false;
	$effect(() => () => (disposed = true));
	async function approve() {
		if (!decision || busy) return;
		busy = true;
		try {
			await workflows.decide(chat.slug, task.id, decision.sequence, 'approve');
			appToast.success(m['workflows.task.decided']());
		} catch (error) {
			appToast.apiError(error, m['workflows.task.decision_failed']());
		} finally {
			busy = false;
			if (!disposed) void run.load();
		}
	}

	// A waiver is asked for once: it holds for this head only.
	let waiving = $state<CheckRow | null>(null);
	let confirming = $state(false);
	let request: { check: string; id: string } | null = null;
	function askWaiver(row: CheckRow) {
		waiving = row;
		confirming = true;
	}
	async function waive() {
		const row = waiving;
		confirming = false;
		if (!row || busy) return;
		busy = true;
		// A retry of the same waiver is the same post.
		if (request?.check !== row.check.id) request = { check: row.check.id, id: crypto.randomUUID() };
		try {
			const message = await agentTasks.postRoom(chat.slug, task.id, {
				kind: 'waiver',
				data: { check: row.check.id },
				request_id: request.id
			});
			chat.acceptRoomMessages(task.id, [message]);
			request = null;
			appToast.success(m['tasks.room_sent']({ identifier: chat.issue?.identifier ?? '' }));
		} catch (error) {
			appToast.apiError(error, m['tasks.check_waive_failed']());
		} finally {
			busy = false;
		}
	}

	const stateLabel: Record<CheckState, () => string> = {
		met: m['tasks.check_met'],
		unmet: m['tasks.check_unmet'],
		waiting: m['tasks.check_waiting'],
		unknown: m['tasks.check_unknown'],
		waived: m['tasks.check_waived']
	};
	const stateTone: Record<CheckState, string> = {
		met: 'text-[var(--status-success)]',
		unmet: 'text-[var(--status-warning)]',
		waiting: 'text-[var(--status-running)]',
		unknown: 'text-muted-foreground',
		waived: 'text-muted-foreground'
	};
	const verdictLabel: Record<string, () => string> = {
		approve: m['tasks.review_verdict_approve'],
		request_changes: m['tasks.review_verdict_request_changes'],
		comment: m['tasks.review_verdict_comment']
	};
	function noteText(note: CheckNote): string {
		switch (note.kind) {
			case 'no_verdict':
				return m['tasks.check_no_verdict']({ role: note.role });
			case 'old_verdict':
				return m['tasks.check_old_verdict']({ role: note.role, commit: note.commit });
			case 'not_approved':
				return m['tasks.check_not_approved']({ role: note.role, verdict: verdictLabel[note.verdict]?.() ?? note.verdict });
			case 'merges_first':
				return m['tasks.fact_overlap']({ issues: note.issues });
			case 'waived':
				return m['tasks.check_waived_note']({ commit: note.commit || '—' });
		}
	}
	const when = (at: string) => new Date(at).toLocaleString(getLocale(), { dateStyle: 'medium', timeStyle: 'short' });
	const heading = 'text-xs font-medium text-muted-foreground';
</script>

{#snippet stateIcon(state: CheckState)}
	<span role="img" aria-label={stateLabel[state]()} class="inline-flex shrink-0 {stateTone[state]}">
		{#if state === 'met'}<CircleCheck size={15} />{:else if state === 'unmet'}<CircleAlert size={15} />{:else if state === 'waiting'}<CirclePause
				size={15}
			/>{:else if state === 'waived'}<CircleMinus size={15} />{:else}<CircleDashed size={15} />{/if}
	</span>
{/snippet}

<div class="min-w-0 space-y-4 px-4 py-3" data-testid="task-checks">
	{#if !pipeline || !checks}
		{#if run.failed}<div class="flex flex-wrap items-center gap-2">
				<p role="alert" class="text-sm text-destructive">{m['workflows.task.load_failed']()}</p>
				<Button variant="ghost" size="sm" onclick={() => void run.load()}><RefreshCw />{m['tasks.refresh']()}</Button>
			</div>{:else}<p class="text-xs text-muted-foreground" role="status">{m['assistant.loading']()}</p>{/if}
	{:else if !checks.rows.length}
		<p class="text-xs text-muted-foreground">{m['tasks.checks_none']()}</p>
	{:else}
		{#if short}<p class="text-xs text-muted-foreground">{m['tasks.checks_head']({ commit: short })}</p>{/if}
		{#each groups as rows (rows[0].phase.id)}
			{@const group = rows[0].phase}
			<section class="min-w-0 space-y-2" aria-labelledby="checks-{group.id}" data-testid="checks-phase-{group.id}">
				<h3 id="checks-{group.id}" class="flex min-w-0 items-center gap-2 {heading}">
					<span class="min-w-0 truncate">{group.label}</span>
					{#if group.id === phase && !done}<Badge variant="outline" class="h-5 px-1.5 text-[11px] font-normal"
							>{m['tasks.checks_current']()}</Badge
						>{/if}
				</h3>
				<ul class="divide-y rounded-lg border">
					{#each rows as row (row.check.id)}
						<li class="min-w-0 space-y-1.5 px-3 py-2" data-testid="check-{row.check.id}" data-state={row.state}>
							<div class="flex min-w-0 items-center gap-2">
								{@render stateIcon(row.state)}
								<span class="min-w-0 truncate text-sm font-medium">{row.check.id}</span>
								<code class="min-w-0 truncate text-xs text-muted-foreground"
									>{row.check.fact}{row.check.role ? ` · ${row.check.role}` : ''}</code
								>
								<!-- Read out once, from the icon. -->
								<span class="ml-auto shrink-0 text-xs {stateTone[row.state]}" aria-hidden="true">{stateLabel[row.state]()}</span>
							</div>
							<p class="text-xs text-muted-foreground">
								{row.check.required ? m['workflows.pipeline_required']() : m['workflows.pipeline_advisory']()}{#if row.since}{` · ${m[
										'tasks.check_since'
									]({ time: when(row.since) })}`}{/if}
							</p>
							{#if row.note}<p class="text-xs break-words">{noteText(row.note)}</p>{/if}
							{#if row.reason}<ClampedText text={row.reason} class="text-xs text-muted-foreground" />{/if}
							{#if approvable(row) || waivable(row)}<div class="flex flex-wrap gap-2 pt-0.5">
									{#if approvable(row)}<Button size="sm" disabled={busy} onclick={() => void approve()}
											>{m['workflows.task.approve']()}</Button
										>{/if}
									{#if waivable(row)}<Button
											size="sm"
											variant="outline"
											disabled={busy}
											aria-label={m['tasks.check_waive_named']({ check: row.check.id })}
											onclick={() => askWaiver(row)}>{m['tasks.check_waive']()}</Button
										>{/if}
								</div>{/if}
						</li>
					{/each}
				</ul>
			</section>
		{/each}
	{/if}
</div>

<AlertDialog.Root bind:open={confirming}>
	<AlertDialog.Content>
		<AlertDialog.Header>
			<AlertDialog.Title>{m['tasks.check_waive_title']({ check: waiving?.check.id ?? '' })}</AlertDialog.Title>
			<AlertDialog.Description
				>{m['tasks.check_waive_desc']({ phase: waiving?.phase.label ?? '', commit: short || '—' })}</AlertDialog.Description
			>
		</AlertDialog.Header>
		<AlertDialog.Footer>
			<AlertDialog.Cancel>{m['common.cancel']()}</AlertDialog.Cancel>
			<AlertDialog.Action disabled={busy} onclick={() => void waive()}>{m['tasks.check_waive']()}</AlertDialog.Action>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>
