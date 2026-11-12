<script lang="ts">
	import { ChevronRight } from '@lucide/svelte';
	import * as Collapsible from '$lib/components/ui/collapsible/index.js';
	import * as Sheet from '$lib/components/ui/sheet/index.js';
	import * as Tabs from '$lib/components/ui/tabs/index.js';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { agentTasks, type RunPrompt } from '$lib/api/agent-tasks';
	import type { WorkflowStepRun } from '$lib/api/workflows';
	import { agentInfo } from '$lib/features/agents/agents';
	import ClampedText from './ClampedText.svelte';
	import { formatSize, promptSections, runLabels } from './run-prompt';
	import { getLocale, m } from '$lib/features/i18n/messages';

	// The prompt a step's agent received, opened from the step's line: one
	// section per source, in the order Kuayle assembled it, named by where it
	// came from and with its size, so two sources that contradict each other
	// are both on screen. A long section shows its first lines and expands in
	// place. The text is what was sent to an agent and is only ever escaped.
	let {
		slug,
		runs,
		title,
		onclose
	}: {
		slug: string;
		// The step's recorded runs, oldest first; the newest is shown first.
		runs: WorkflowStepRun[];
		// The step's name, as its line shows it.
		title: string;
		onclose: () => void;
	} = $props();

	let chosen = $state('');
	const run = $derived(runs.find((item) => item.id === chosen) ?? runs.at(-1));
	let prompt = $state.raw<RunPrompt | null>(null),
		loading = $state(true),
		failed = $state(false);
	$effect(() => {
		const current = run;
		if (!current) return;
		let stale = false;
		loading = true;
		failed = false;
		prompt = null;
		agentTasks
			.runPrompt(slug, current.task_id, current.id)
			.then((loaded) => {
				if (!stale) prompt = loaded;
			})
			.catch(() => {
				if (!stale) failed = true;
			})
			.finally(() => {
				if (!stale) loading = false;
			});
		return () => (stale = true);
	});
	const sections = $derived(promptSections(prompt));
	let closed = $state<Record<string, boolean>>({});
	const names: Record<string, () => string> = {
		task: m['workflows.task.pick_task'],
		workflow_step: m['prompt.source_workflow_step'],
		plan: m['tasks.plan'],
		follow_up: m['prompt.source_follow_up'],
		profile: m['prompt.source_profile'],
		contract: m['prompt.source_contract'],
		project: m['prompt.source_project'],
		notebook: m['notebook.title'],
		handoff: m['prompt.source_handoff'],
		issue: m['insights.issue'],
		related_work: m['prompt.source_related_work'],
		messages: m['assistant.messages'],
		attachments: m['prompt.source_attachments'],
		memory: m['lessons.title']
	};
	// A step resumed within a minute has runs that start at the same time:
	// they are named by the step and their place in it, the time on hover.
	const labels = $derived(runLabels(title, runs));
	const started = (item: WorkflowStepRun) =>
		new Date(item.started_at ?? item.created_at).toLocaleString(getLocale(), { dateStyle: 'medium', timeStyle: 'short' });
</script>

<Sheet.Root open onOpenChange={(open) => !open && onclose()}>
	<Sheet.Content side="right" class="w-full gap-0 data-[side=right]:w-full data-[side=right]:sm:max-w-2xl" data-testid="run-prompt">
		<Sheet.Header class="border-b px-4 py-3 pr-12 sm:px-6">
			<Sheet.Title class="truncate">{m['tasks.transcript_from_you']()} · {title}</Sheet.Title>
			<Sheet.Description class="flex min-h-5 flex-wrap items-center gap-1.5 text-xs">
				{#if run}{[agentInfo(run.provider).name, run.model].filter(Boolean).join(' · ')}{/if}
				{#if prompt?.worktree_read_only}<Badge variant="outline" class="h-5 px-1.5 text-[11px] font-normal"
						>{m['prompt.read_only']()}</Badge
					>{/if}
			</Sheet.Description>
			{#if runs.length > 1}<Tabs.Root value={run?.id ?? ''} onValueChange={(value) => (chosen = value)} class="pt-1">
					<Tabs.List>
						{#each runs as item, index (item.id)}<Tabs.Trigger value={item.id} title={started(item)}
								>{labels[index]}</Tabs.Trigger
							>{/each}
					</Tabs.List>
				</Tabs.Root>{/if}
		</Sheet.Header>
		<div class="min-h-0 flex-1 overflow-y-auto px-4 pb-6 sm:px-6" aria-busy={loading}>
			{#if failed}<p role="alert" class="py-4 text-sm text-destructive">{m['tasks.load_error']()}</p>
			{:else if !loading && !sections.length}<p class="py-4 text-sm text-muted-foreground">
					{m['prompt.not_recorded']()}
				</p>
			{:else}<div class="divide-y">
					{#each sections as section (section.key)}
						<Collapsible.Root
							bind:open={() => !closed[section.key], (open) => (closed[section.key] = !open)}
							data-source={section.source}
						>
							<h3>
								<Collapsible.Trigger
									class="group flex w-full min-w-0 cursor-pointer items-center gap-2 py-2.5 text-left outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
								>
									<ChevronRight
										size={14}
										aria-hidden="true"
										class="shrink-0 text-muted-foreground/60 transition-transform group-aria-expanded:rotate-90 motion-reduce:transition-none"
									/>
									<span class="min-w-0 truncate text-sm font-medium">{names[section.source]?.() ?? section.source}</span>
									{#if section.channel === 'instructions_file'}<Badge
											variant="outline"
											class="h-5 shrink-0 px-1.5 text-[11px] font-normal text-muted-foreground"
											>{m['prompt.instructions_file']()}</Badge
										>{/if}
									{#if section.truncated}<Badge variant="secondary" class="h-5 shrink-0 px-1.5 text-[11px] font-normal"
											>{m['prompt.truncated']()}</Badge
										>{/if}
									<span class="ml-auto shrink-0 text-xs text-muted-foreground tabular-nums">{formatSize(section.bytes)}</span>
								</Collapsible.Trigger>
							</h3>
							<Collapsible.Content>
								<div class="pb-3 pl-[22px]">
									<ClampedText text={section.text} lines={6} class="font-mono text-xs leading-5" />
								</div>
							</Collapsible.Content>
						</Collapsible.Root>
					{/each}
				</div>{/if}
		</div>
	</Sheet.Content>
</Sheet.Root>
