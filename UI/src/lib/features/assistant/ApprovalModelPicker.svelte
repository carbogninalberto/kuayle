<script lang="ts">
	import { onMount } from 'svelte';
	import { ChevronDown } from '@lucide/svelte';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import AgentLogo from '$lib/features/agents/AgentLogo.svelte';
	import { agentInfo } from '$lib/features/agents/agents';
	import { agentTasks, type TaskOptions } from '$lib/api/agent-tasks';
	import { conversations, type ApprovalModelChoice } from '$lib/api/conversations';
	import { m } from '$lib/paraglide/messages.js';
	import { choiceFrom, initialSelection, modelsFor, selectHarness, type ModelSelection } from './approval-model';

	// The harness and model of the coding agent an approval starts. It opens on
	// what the assistant proposed, else the configured default, and reports only
	// what the user changed.
	let {
		slug,
		issueId,
		proposed,
		harnessLocked = false,
		onchange,
		disabled = false
	}: {
		slug: string;
		issueId: string;
		proposed: { harness?: string; model?: string };
		/** A follow-up continues on the task's own harness: only its model can change. */
		harnessLocked?: boolean;
		onchange: (choice: ApprovalModelChoice) => void;
		disabled?: boolean;
	} = $props();
	let options = $state<TaskOptions | null>(null),
		initial = $state<ModelSelection | null>(null),
		current = $state<ModelSelection | null>(null);
	const models = $derived(current && options ? modelsFor(options.model_servers, current.harness) : []);
	onMount(() => {
		let disposed = false;
		void (async () => {
			try {
				const chat = await conversations.openIssue(slug, issueId);
				const loaded = await agentTasks.options(slug, chat.id);
				if (disposed) return;
				options = loaded;
				initial = initialSelection(loaded, proposed);
				current = { ...initial };
			} catch {
				// Without the options the card still approves with the default model.
			}
		})();
		return () => (disposed = true);
	});
	function update(next: ModelSelection) {
		current = next;
		if (initial) onchange(choiceFrom(initial, next));
	}
</script>

{#if options && current}
	<div class="flex flex-wrap items-center gap-2">
		{#if !harnessLocked && options.providers.length > 1}
			<DropdownMenu.Root
				><DropdownMenu.Trigger {disabled}
					>{#snippet child({ props })}<Button
							{...props}
							variant="outline"
							size="sm"
							aria-label={m['tasks.harness']()}
							><AgentLogo agent={current!.harness} size="sm" />{agentInfo(current!.harness).name}<ChevronDown
								size={14}
							/></Button
						>{/snippet}</DropdownMenu.Trigger
				><DropdownMenu.Content
					><DropdownMenu.RadioGroup
						value={current.harness}
						onValueChange={(value) => value && options && update(selectHarness(options, value))}
						>{#each options.providers as provider (provider.id)}<DropdownMenu.RadioItem value={provider.id} closeOnSelect
								>{provider.display_name}</DropdownMenu.RadioItem
							>{/each}</DropdownMenu.RadioGroup
					></DropdownMenu.Content
				></DropdownMenu.Root
			>
		{/if}
		{#if models.length}
			<DropdownMenu.Root
				><DropdownMenu.Trigger {disabled}
					>{#snippet child({ props })}<Button
							{...props}
							variant="outline"
							size="sm"
							aria-label={m['tasks.model']()}>{current!.model || m['tasks.model']()}<ChevronDown size={14} /></Button
						>{/snippet}</DropdownMenu.Trigger
				><DropdownMenu.Content
					><DropdownMenu.RadioGroup
						value={current.model}
						onValueChange={(value) => current && update({ ...current, model: value })}
						>{#each models as value (value)}<DropdownMenu.RadioItem {value} closeOnSelect
								>{value}</DropdownMenu.RadioItem
							>{/each}</DropdownMenu.RadioGroup
					></DropdownMenu.Content
				></DropdownMenu.Root
			>
		{/if}
	</div>
{/if}
