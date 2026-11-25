<script lang="ts">
	import { Badge } from '$lib/components/ui/badge/index.js';
	import type { TaskMessage } from '$lib/api/agent-tasks';
	import type { PipelineDefinition, WorkflowStepRun } from '$lib/api/workflows';
	import { pipelineChecks, type CheckNote, type CheckState } from '$lib/features/assistant/task-checks';
	import { getLocale, m } from '$lib/features/i18n/messages';
	import { factLabel, ownerLabel } from './pipeline-labels';
	import { pipelineRunMembers } from './pipeline-run-members';

	let { pipeline, room, phase, done, runs }: {
		pipeline: PipelineDefinition;
		room: readonly TaskMessage[];
		phase: string | null | undefined;
		done: boolean;
		runs: readonly WorkflowStepRun[];
	} = $props();

	const members = $derived(pipelineRunMembers(pipeline, runs));
	const rows = $derived(pipelineChecks(pipeline, room, phase, done).rows);
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
</script>

<div class="min-w-0 space-y-4">
	<section class="space-y-1.5" aria-label={m['common.members']()}>
		<h4 class="text-xs font-medium text-muted-foreground">{m['common.members']()}</h4>
		<ul class="flex flex-wrap gap-1.5">
			{#each members as member (member.role)}
				<li data-testid="run-member-{member.role}">
					<Badge variant="outline" class="h-auto max-w-full flex-wrap justify-start gap-1.5 py-0.5 font-normal">
						<span class="break-all">{ownerLabel(member.role)}</span>
						{#if member.working}
							<span class="size-1.5 shrink-0 rounded-full bg-[var(--status-running)]" aria-hidden="true"></span>
							<span class="text-[var(--status-running)]">{m['assistant.indicator_running']()}</span>
						{/if}
					</Badge>
				</li>
			{/each}
		</ul>
	</section>
	<section class="space-y-1.5" aria-label={m['workflows.pipeline_editor.checks']()}>
		<h4 class="text-xs font-medium text-muted-foreground">{m['workflows.pipeline_editor.checks']()}</h4>
		{#if rows.length}
			<ul class="divide-y rounded-lg border">
				{#each rows as row (row.check.id)}
					<li class="min-w-0 space-y-1 px-3 py-2" data-testid="run-check-{row.check.id}">
						<div class="flex flex-wrap items-baseline gap-x-2 gap-y-1">
							<span class="min-w-0 flex-1 text-sm font-medium break-words">{row.check.id}</span>
							<Badge variant="outline" class="h-5 px-1.5 font-normal {stateTone[row.state]}">{stateLabel[row.state]()}</Badge>
						</div>
						<p class="text-xs break-words text-muted-foreground">
							{row.phase.label} · {factLabel(row.check.fact)}{row.check.role ? ` · ${row.check.role}` : ''} ·
							{row.check.required ? m['workflows.pipeline_required']() : m['workflows.pipeline_advisory']()}
							{#if row.since}
								· <time datetime={row.since}>{m['tasks.check_since']({ time: when(row.since) })}</time>
							{/if}
						</p>
						{#if row.note}<p class="text-xs break-words">{noteText(row.note)}</p>{/if}
						{#if row.reason}<p class="text-xs break-words whitespace-pre-wrap text-muted-foreground">{row.reason}</p>{/if}
					</li>
				{/each}
			</ul>
		{:else}
			<p class="text-xs text-muted-foreground">{m['tasks.checks_none']()}</p>
		{/if}
	</section>
</div>
