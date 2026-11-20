<script lang="ts">
	import { onMount } from 'svelte';
	import { ChevronDown } from '@lucide/svelte';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import AgentLogo from '$lib/features/agents/AgentLogo.svelte';
	import { agentInfo } from '$lib/features/agents/agents';
	import { agentTasks, type TaskOptions } from '$lib/api/agent-tasks';
	import { conversations, type ApprovalModelChoice } from '$lib/api/conversations';
	import { m } from '$lib/features/i18n/messages';
	import { serversFor } from '$lib/features/agents/model-choice';
	import ModelCombobox from '$lib/features/agents/ModelCombobox.svelte';
	import { ModelSuggestions, hasModelSuggestions } from '$lib/features/agents/model-suggestions.svelte';
	import {
		choiceFrom,
		initialSelection,
		selectHarness,
		selectServer,
		type ModelSelection
	} from './approval-model';

	// The harness, model server and model of the coding agent an approval starts.
	// It opens on what the assistant proposed, else what the backend starts by
	// default, and reports only what the user changed. As in the task composer,
	// the models are those of the selected server.
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
		/** server: the server the task runs on, preferred for the proposed model. */
		proposed: { harness?: string; model?: string; server?: string; current?: boolean };
		/** A follow-up continues on the task's own harness: only its model can change. */
		harnessLocked?: boolean;
		onchange: (choice: ApprovalModelChoice) => void;
		disabled?: boolean;
	} = $props();
	let options = $state<TaskOptions | null>(null),
		initial = $state<ModelSelection | null>(null),
		current = $state<ModelSelection | null>(null);
	const harness = $derived(current?.harness ?? '');
	const serverId = $derived(current?.server ?? '');
	const servers = $derived(options && harness ? serversFor(options.model_servers, harness) : []);
	const server = $derived(servers.find((item) => item.id === serverId));
	// The agent's own sign-in is offered only when it is what the card opened on.
	const ownSignIn = $derived(initial?.harness === current?.harness && initial?.server === '');
	const suggestions = new ModelSuggestions();
	const showModel = $derived(!!server || (!servers.length && hasModelSuggestions(harness)));
	$effect(() => suggestions.open(slug, showModel ? harness : '', server));
	let disposed = false;
	// "Agent default" can only be chosen where sending no model means exactly
	// that: the card opened without one, or the harness changed (a new harness
	// never inherits the proposed model).
	const canClearModel = $derived(!!initial && (initial.model === '' || initial.harness !== current?.harness));
	onMount(() => {
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
	<div class="flex flex-wrap items-end gap-3">
		{#if !harnessLocked && options.providers.length > 1}
			<div class="grid gap-1.5"><span class="text-xs font-medium text-muted-foreground">{m['tasks.harness']()}</span>
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
			</div>
		{/if}
		{#if servers.length > 1 || (servers.length && !server)}
			<div class="grid gap-1.5"><span class="text-xs font-medium text-muted-foreground">{m['tasks.model_server']()}</span>
			<DropdownMenu.Root
				><DropdownMenu.Trigger {disabled}
					>{#snippet child({ props })}<Button
							{...props}
							variant="outline"
							size="sm"
							aria-label={m['tasks.model_server']()}
							>{server?.name ?? m['tasks.own_sign_in']()}<ChevronDown size={14} /></Button
						>{/snippet}</DropdownMenu.Trigger
				><DropdownMenu.Content
					><DropdownMenu.RadioGroup
						value={current.server}
						onValueChange={(value) => options && current && update(selectServer(options, current, value))}
						>{#if ownSignIn}<DropdownMenu.RadioItem value="" closeOnSelect>{m['tasks.own_sign_in']()}</DropdownMenu.RadioItem
							>{/if}{#each servers as item (item.id)}<DropdownMenu.RadioItem value={item.id} closeOnSelect
								>{item.name}</DropdownMenu.RadioItem
							>{/each}</DropdownMenu.RadioGroup
					></DropdownMenu.Content
				></DropdownMenu.Root
			>
			</div>
		{/if}
		{#if showModel}
			<div class="grid gap-1.5"><span class="text-xs font-medium text-muted-foreground">{m['tasks.model']()}</span>
			<ModelCombobox
				compact
				value={current.model}
				models={suggestions.models}
				status={suggestions.status}
				placeholder={m['profiles.model_placeholder']()}
				aria-label={m['tasks.model']()}
				{disabled}
				allowCustom={!server}
				canClear={!server && canClearModel}
				onselect={(model) => current && update({ ...current, model })}
				onrefresh={() => current && void suggestions.load(slug, current.harness, true, server)}
			/>
			</div>
		{:else if current.model}
			<!-- No server offers the proposed model: it runs on the agent's own sign-in. -->
			<span class="text-xs text-muted-foreground">{current.model}</span>
		{/if}
	</div>
{/if}
