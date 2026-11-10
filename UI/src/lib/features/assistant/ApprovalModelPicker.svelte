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
	import { agentModelCatalog } from '$lib/api/dev-machines';
	import type { CatalogModel } from '$lib/types/dev-machine';
	import {
		choiceFrom,
		initialSelection,
		selectHarness,
		selectServer,
		serverModels,
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
	const servers = $derived(current && options ? serversFor(options.model_servers, current.harness) : []);
	const server = $derived(servers.find((item) => item.id === current?.server));
	// The agent's own sign-in is offered only when it is what the card opened on.
	const ownSignIn = $derived(initial?.harness === current?.harness && initial?.server === '');
	// An agent no model server runs (OpenCode) picks among its own models: the
	// server's catalog for it. A catalog of the workspace's model servers is
	// left to the server picker above.
	let ownModels = $state<CatalogModel[]>([]);
	let catalogRequest = 0,
		disposed = false;
	async function loadOwnModels(harness: string) {
		const request = ++catalogRequest;
		ownModels = [];
		if (!options || !harness || serversFor(options.model_servers, harness).length) return;
		try {
			const catalog = await agentModelCatalog(slug, harness);
			if (!disposed && request === catalogRequest && catalog.source !== 'workspace') ownModels = catalog.models;
		} catch {
			/* Without the list the card keeps the proposed or default model. */
		}
	}
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
				void loadOwnModels(initial.harness);
			} catch {
				// Without the options the card still approves with the default model.
			}
		})();
		return () => (disposed = true);
	});
	function update(next: ModelSelection) {
		if (next.harness !== current?.harness) void loadOwnModels(next.harness);
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
		{#if servers.length > 1 || (servers.length && !server)}
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
		{/if}
		{#if server}
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
						>{#each serverModels(server) as value (value)}<DropdownMenu.RadioItem {value} closeOnSelect
								>{value}</DropdownMenu.RadioItem
							>{/each}</DropdownMenu.RadioGroup
					></DropdownMenu.Content
				></DropdownMenu.Root
			>
		{:else if !servers.length && ownModels.length}
			<DropdownMenu.Root
				><DropdownMenu.Trigger {disabled}
					>{#snippet child({ props })}<Button
							{...props}
							variant="outline"
							size="sm"
							aria-label={m['tasks.model']()}
							>{current!.model || m['profiles.model_placeholder']()}<ChevronDown size={14} /></Button
						>{/snippet}</DropdownMenu.Trigger
				><DropdownMenu.Content class="max-h-72 overflow-y-auto"
					><DropdownMenu.RadioGroup
						value={current.model}
						onValueChange={(value) => current && update({ ...current, server: '', model: value })}
						>{#if canClearModel}<DropdownMenu.RadioItem value="" closeOnSelect
								>{m['profiles.model_placeholder']()}</DropdownMenu.RadioItem
							>{/if}{#if current.model && !ownModels.some((item) => item.id === current!.model)}<DropdownMenu.RadioItem
								value={current.model}
								closeOnSelect>{current.model}</DropdownMenu.RadioItem
							>{/if}{#each ownModels as item (item.id)}<DropdownMenu.RadioItem value={item.id} closeOnSelect
								>{item.id}</DropdownMenu.RadioItem
							>{/each}</DropdownMenu.RadioGroup
					></DropdownMenu.Content
				></DropdownMenu.Root
			>
		{:else if current.model}
			<!-- No server offers the proposed model: it runs on the agent's own sign-in. -->
			<span class="text-xs text-muted-foreground">{current.model}</span>
		{/if}
	</div>
{/if}
