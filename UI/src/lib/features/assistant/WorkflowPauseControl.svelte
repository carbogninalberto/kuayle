<script lang="ts">
	import { Button } from '$lib/components/ui/button/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import { LoaderCircle, Pause, Play } from '@lucide/svelte';
	import { agentTasks, type AgentTask, type WorkflowPauseMode } from '$lib/api/agent-tasks';
	import { m } from '$lib/features/i18n/messages';
	import { canResume, workflowPause } from './workflow-pause';
	// Pauses and resumes a task's workflow, for its creator: a running
	// workflow offers Pause (now, or after this step), a paused one Resume.
	// It renders nothing for a task that can do neither.
	let {
		slug,
		task,
		onchange,
		onerror,
		size = 'sm'
	}: {
		slug: string;
		task: AgentTask;
		// The task as the server returned it after the pause or resume.
		onchange?: (task: AgentTask) => void;
		onerror?: (error: unknown) => void;
		size?: 'sm' | 'xs';
	} = $props();
	const pause = $derived(workflowPause(task));
	let busy = $state(false);
	async function run(request: () => Promise<AgentTask>) {
		if (busy) return;
		busy = true;
		try {
			onchange?.(await request());
		} catch (e) {
			onerror?.(e);
		} finally {
			busy = false;
		}
	}
	const pauseWith = (mode: WorkflowPauseMode) => run(() => agentTasks.pause(slug, task.id, mode));
</script>

{#if pause === 'pausable'}
	<DropdownMenu.Root>
		<DropdownMenu.Trigger>
			{#snippet child({ props })}
				<Button {...props} variant="outline" {size} disabled={busy} data-testid="workflow-pause"
					>{#if busy}<LoaderCircle class="animate-spin motion-reduce:animate-none" />{:else}<Pause />{/if}{m[
						'tasks.pause'
					]()}</Button
				>
			{/snippet}
		</DropdownMenu.Trigger>
		<DropdownMenu.Content align="end" class="min-w-44">
			<DropdownMenu.Item onclick={() => void pauseWith('now')}>{m['tasks.pause_now']()}</DropdownMenu.Item>
			<DropdownMenu.Item onclick={() => void pauseWith('after_step')}
				>{m['tasks.pause_after_step']()}</DropdownMenu.Item
			>
		</DropdownMenu.Content>
	</DropdownMenu.Root>
{:else if pause !== 'none'}
	<Button
		{size}
		disabled={busy || !canResume(task)}
		data-testid="workflow-resume"
		onclick={() => void run(() => agentTasks.resume(slug, task.id))}
		>{#if busy}<LoaderCircle class="animate-spin motion-reduce:animate-none" />{:else}<Play />{/if}{m[
			'tasks.resume'
		]()}</Button
	>
{/if}
