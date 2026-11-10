<script lang="ts">
	import { untrack } from 'svelte';
	import { RefreshCw } from '@lucide/svelte';
	import * as Breadcrumb from '$lib/components/ui/breadcrumb/index.js';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import SelectField from '$lib/components/shared/SelectField.svelte';
	import AgentLogo from '$lib/features/agents/AgentLogo.svelte';
	import { agentInfo } from '$lib/features/agents/agents';
	import { agentTasks, type AgentTask, type ValidationStep } from '$lib/api/agent-tasks';
	import type { AgentRun, DevMachineLogChunk } from '$lib/types/dev-machine';
	import type { ConversationState } from './conversation-state.svelte';
	import AgentTranscript from './AgentTranscript.svelte';
	import TaskLiveThread from './TaskLiveThread.svelte';
	import { taskLabel } from './task-labels';
	import { taskTone, toneBadge } from './task-tone';
	import { m } from '$lib/features/i18n/messages';

	// One coding task's agent as the centre conversation: where it is (a
	// breadcrumb back to the assistant), the readable session of its current
	// iteration, streaming as output arrives, and the live thread with the
	// agent (its questions, your replies). Replies are written in the page's
	// one composer, which aims at this agent while this view is open.
	let {
		chat,
		task,
		backHref,
		onback
	}: {
		chat: ConversationState;
		task: AgentTask;
		backHref: string;
		onback: () => void;
	} = $props();
	const identifier = $derived(chat.issue?.identifier ?? '');
	const agent = $derived(agentInfo(task.provider).name);
	const active = $derived(
		['queued', 'preparing', 'running', 'validating', 'publishing', 'cancelling'].includes(task.state)
	);
	const tone = $derived(taskTone(task.state));

	let runs = $state<AgentRun[]>([]),
		logs = $state<DevMachineLogChunk[]>([]),
		checks = $state<string[]>([]),
		loading = $state(false),
		more = $state(false),
		error = $state('');
	let selectedRun = $state(untrack(() => task.run_id));
	let seenRun = untrack(() => task.run_id),
		dirty = false,
		disposed = false,
		timer: ReturnType<typeof setTimeout> | undefined;
	$effect(() => () => {
		disposed = true;
		clearTimeout(timer);
	});
	// A new iteration replaces the view's run; an earlier one stays selectable.
	$effect(() => {
		if (task.run_id === seenRun) return;
		seenRun = task.run_id;
		untrack(() => (selectedRun = task.run_id));
	});
	$effect(() => {
		void selectedRun;
		untrack(() => {
			logs = [];
			checks = [];
			more = false;
		});
	});
	// The independent and manual checks write to the same run; they are not
	// the agent's session.
	const session = $derived(logs.filter((line) => !line.service_id || !checks.includes(line.service_id)));
	// Runs carry no iteration number: an earlier one is named by its start.
	const runLabel = (run: AgentRun) =>
		run.id === task.run_id
			? m['tasks.attempt']({ number: task.generation })
			: `${agentInfo(run.provider_id).name} · ${new Date(run.created_at).toLocaleString()}`;
	const iterationLabel = $derived.by(() => {
		const run = runs.find((item) => item.id === selectedRun);
		return run ? runLabel(run) : m['tasks.attempt']({ number: task.generation });
	});

	// Pages through the run's output from the last chunk seen; a page limit
	// keeps a huge run from blocking the view, "load more" continues.
	async function refresh() {
		if (loading) {
			dirty = true;
			return;
		}
		const run = selectedRun;
		if (!run) return;
		loading = true;
		error = '';
		try {
			const [attempts, validation, manual] = await Promise.all([
				agentTasks.runs(chat.slug, task.id),
				agentTasks.validation(chat.slug, task.id, run).catch(() => null as ValidationStep | null),
				agentTasks.manualChecks(chat.slug, task.id, run).catch(() => [] as ValidationStep[])
			]);
			if (disposed || selectedRun !== run) return;
			runs = attempts.runs;
			checks = [validation?.id, ...(Array.isArray(manual) ? manual : []).map((step) => step.id)].filter(
				(id): id is string => !!id
			);
			for (let page = 0; page < 20; page++) {
				const result = await agentTasks.logs(chat.slug, task.id, run, logs.at(-1)?.id ?? 0);
				if (disposed || selectedRun !== run) return;
				const known = new Set(logs.map((line) => line.id));
				logs = [...logs, ...result.logs.filter((line) => !known.has(line.id))];
				more = !!result.next_after;
				if (!result.next_after) break;
			}
		} catch (e) {
			if (disposed) return;
			const status = (e as { status?: number }).status ?? 0;
			error = status === 404 ? m['tasks.attempt_missing']() : m['tasks.load_error']();
		} finally {
			if (!disposed) {
				loading = false;
				if (dirty) {
					dirty = false;
					void refresh();
				}
			}
		}
	}
	// Live: new output (task.activity), state changes and check updates
	// refresh, throttled from the first event so a stream keeps flowing.
	$effect(() => {
		void [task.id, task.state, selectedRun, chat.taskActivity[task.id]?.log_id, chat.taskValidationCursor[task.id]];
		if (timer !== undefined) return;
		timer = setTimeout(() => {
			timer = undefined;
			void refresh();
		}, 150);
	});
</script>

<div class="flex min-w-0 flex-col gap-5" data-testid="agent-thread">
	<Breadcrumb.Root aria-label={m['assistant.agent_view_breadcrumb']()}>
		<Breadcrumb.List>
			<Breadcrumb.Item>
				<Breadcrumb.Link
					href={backHref}
					onclick={(event) => {
						if (event.metaKey || event.ctrlKey || event.shiftKey || event.button !== 0) return;
						event.preventDefault();
						onback();
					}}>{m['assistant.title']()}</Breadcrumb.Link
				>
			</Breadcrumb.Item>
			<Breadcrumb.Separator />
			<Breadcrumb.Item>
				<Breadcrumb.Page>{identifier} · {iterationLabel}</Breadcrumb.Page>
			</Breadcrumb.Item>
		</Breadcrumb.List>
	</Breadcrumb.Root>

	<div class="flex min-w-0 items-start gap-2.5">
		<AgentLogo agent={task.provider} size="sm" class="mt-0.5 shrink-0" />
		<div class="min-w-0 flex-1 space-y-0.5">
			<div class="flex min-w-0 items-start justify-between gap-3">
				<p class="min-w-0 text-sm font-medium break-words whitespace-pre-wrap line-clamp-3">{task.prompt}</p>
				<Badge variant="outline" class="shrink-0 {toneBadge[tone]}" role="status">{taskLabel(task.state)}</Badge>
			</div>
			<p class="text-xs break-words text-muted-foreground">
				{agent}{task.model ? ` · ${task.model}` : ''} · {task.reason}
			</p>
		</div>
	</div>

	{#if runs.length > 1}<SelectField
			bind:value={selectedRun}
			aria-label={m['tasks.execution_attempt']()}
			options={runs.map((run) => ({ value: run.id, label: runLabel(run) }))}
		/>{/if}

	<AgentTranscript chunks={session} active={active && selectedRun === task.run_id} />
	{#if more}<Button variant="ghost" size="sm" class="self-start" disabled={loading} onclick={() => void refresh()}
			>{m['tasks.more_logs']()}</Button
		>{/if}
	{#if error}<div class="flex flex-wrap items-center gap-2">
			<p role="alert" class="text-sm text-destructive">{error}</p>
			<Button variant="ghost" size="sm" onclick={() => void refresh()}><RefreshCw />{m['tasks.refresh']()}</Button>
		</div>{/if}

	{#if task.kind !== 'review'}<TaskLiveThread {chat} {task} />{/if}
</div>
