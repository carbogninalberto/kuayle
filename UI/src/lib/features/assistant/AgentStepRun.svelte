<script lang="ts">
	import { untrack } from 'svelte';
	import { RefreshCw } from '@lucide/svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import { agentTasks, type ValidationStep } from '$lib/api/agent-tasks';
	import type { WorkflowStepRun } from '$lib/api/workflows';
	import type { DevMachineLogChunk } from '$lib/types/dev-machine';
	import type { ConversationState } from './conversation-state.svelte';
	import AgentTranscript from './AgentTranscript.svelte';
	import { m } from '$lib/features/i18n/messages';

	// One agent run inside its step: the run's recorded output as a readable
	// session, loaded when the step is opened and streaming while it runs. The
	// run is read under the task that owns it (a reviewer step's run belongs
	// to its review task); the server decides who may read it.
	let {
		chat,
		run,
		active = false,
		concluded = false
	}: {
		chat: ConversationState;
		run: WorkflowStepRun;
		// The step is running and this is the run it is on.
		active?: boolean;
		// The step already shows the run's final message above it (its plan).
		concluded?: boolean;
	} = $props();

	let logs = $state<DevMachineLogChunk[]>([]),
		checks = $state<string[]>([]),
		loading = $state(false),
		more = $state(false),
		error = $state('');
	let dirty = false,
		disposed = false,
		timer: ReturnType<typeof setTimeout> | undefined;
	$effect(() => () => {
		disposed = true;
		clearTimeout(timer);
	});
	// The independent and manual checks write to the same run; they are not
	// the agent's session.
	const session = $derived(logs.filter((line) => !line.service_id || !checks.includes(line.service_id)));

	// Pages through the run's output from the last chunk seen; a page limit
	// keeps a huge run from blocking the view, "load more" continues.
	async function refresh() {
		if (loading) {
			dirty = true;
			return;
		}
		loading = true;
		error = '';
		try {
			const [validation, manual] = await Promise.all([
				agentTasks.validation(chat.slug, run.task_id, run.id).catch(() => null as ValidationStep | null),
				agentTasks.manualChecks(chat.slug, run.task_id, run.id).catch(() => [] as ValidationStep[])
			]);
			if (disposed) return;
			checks = [validation?.id, ...(Array.isArray(manual) ? manual : []).map((step) => step.id)].filter(
				(id): id is string => !!id
			);
			for (let page = 0; page < 20; page++) {
				const result = await agentTasks.logs(chat.slug, run.task_id, run.id, logs.at(-1)?.id ?? 0);
				if (disposed) return;
				const known = new Set(logs.map((line) => line.id));
				logs = [...logs, ...result.logs.filter((line) => !known.has(line.id))];
				more = !!result.next_after;
				if (!result.next_after) break;
			}
		} catch (e) {
			if (disposed) return;
			const status = (e as { status?: number }).status ?? 0;
			// A run that was only just queued has nothing recorded yet.
			if (status === 404 && active) return;
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
		void [
			run.id,
			run.status,
			active,
			chat.taskActivity[run.task_id]?.log_id,
			chat.taskValidationCursor[run.task_id]
		];
		if (timer !== undefined) return;
		timer = setTimeout(() => {
			timer = undefined;
			void untrack(refresh);
		}, 150);
	});
</script>

<div class="flex min-w-0 flex-col gap-3">
	<AgentTranscript chunks={session} {active} lead={!more} summary={run.summary ?? ''} {concluded} />
	{#if more}<Button variant="ghost" size="sm" class="self-start" disabled={loading} onclick={() => void refresh()}
			>{m['tasks.more_logs']()}</Button
		>{/if}
	{#if error}<div class="flex flex-wrap items-center gap-2">
			<p role="alert" class="text-sm text-destructive">{error}</p>
			<Button variant="ghost" size="sm" onclick={() => void refresh()}><RefreshCw />{m['tasks.refresh']()}</Button>
		</div>{/if}
</div>
