<script lang="ts">
	import { CircleAlert } from '@lucide/svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import * as Popover from '$lib/components/ui/popover/index.js';
	import type { PipelineDefinition, WorkflowIssue } from '$lib/api/workflows';
	import { m } from '$lib/features/i18n/messages';
	import { pipelineIssueLabel } from './pipeline-labels';

	// What keeps the pipeline from being saved, in the editor's header: each
	// issue named by the phase it is about, and a click takes the editor to it.
	let {
		issues,
		pipeline,
		onpick
	}: { issues: WorkflowIssue[]; pipeline: PipelineDefinition; onpick: (issue: WorkflowIssue) => void } = $props();

	let open = $state(false);
	const phaseName = (id: string) => pipeline.phases.find((phase) => phase.id === id)?.label.trim() || id;
</script>

<Popover.Root bind:open>
	<Popover.Trigger>
		{#snippet child({ props })}<Button
				{...props}
				variant="ghost"
				size="sm"
				class="px-1 text-destructive hover:text-destructive sm:px-2.5"
				data-testid="pipeline-problems-trigger"
				><CircleAlert aria-hidden="true" /><span class="sr-only sm:not-sr-only"
					>{m['workflows.editor.problems']({ count: issues.length })}</span
				></Button
			>{/snippet}
	</Popover.Trigger>
	<Popover.Content align="end" class="w-80 p-0" data-testid="pipeline-problems">
		<p class="border-b px-3 py-2 text-sm font-medium">{m['workflows.editor.problems_title']()}</p>
		<ul class="max-h-72 overflow-y-auto p-1" role="list">
			{#each issues as issue, i (i)}
				<li>
					<Button
						variant="plain"
						class="h-auto w-full flex-col items-start gap-0.5 rounded-md px-2 py-1.5 text-left whitespace-normal hover:bg-muted"
						onclick={() => {
							open = false;
							onpick(issue);
						}}
					>
						{#if issue.node_id}<span class="text-xs font-medium">{phaseName(issue.node_id)}</span>{/if}
						<span class="text-xs text-muted-foreground">{pipelineIssueLabel(issue)}</span>
					</Button>
				</li>
			{/each}
		</ul>
	</Popover.Content>
</Popover.Root>
