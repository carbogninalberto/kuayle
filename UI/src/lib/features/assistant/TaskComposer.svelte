<script lang="ts">
	import { onMount } from 'svelte';
	import TestCommandField from '$lib/features/dev-machines/TestCommandField.svelte';
	import { parseTestCommand, formatTestCommand } from '$lib/features/dev-machines/test-command';
	import { ChevronDown } from '@lucide/svelte';
	import * as Dialog from '$lib/components/ui/dialog/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import * as InputGroup from '$lib/components/ui/input-group/index.js';
	import * as ToggleGroup from '$lib/components/ui/toggle-group/index.js';
	import AgentLogo from '$lib/features/agents/AgentLogo.svelte';
	import { preferredServer, serversFor } from '$lib/features/agents/model-choice';
	import { allowedEfforts, effortAfterModelChange } from '$lib/features/agents/model-catalog';
	import { agentModelCatalog } from '$lib/api/dev-machines';
	import type { CatalogModel, ThinkingLevel } from '$lib/types/dev-machine';
	import { Button } from '$lib/components/ui/button/index.js';
	import { agentTasks, type AgentTask, type TaskOptions, type TaskInput } from '$lib/api/agent-tasks';
	import { workflows as workflowsApi, type WorkflowList } from '$lib/api/workflows';
	import SelectField from '$lib/components/shared/SelectField.svelte';
	import { Label } from '$lib/components/ui/label/index.js';
	import { authState } from '$lib/features/auth/auth.state.svelte';
	import { m } from '$lib/features/i18n/messages';
	let {
		slug,
		conversation,
		options,
		task,
		initialPrompt = '',
		onaccepted,
		onclose
	}: {
		slug: string;
		conversation: string;
		options: TaskOptions | null;
		task?: AgentTask;
		initialPrompt?: string;
		onaccepted: (task: AgentTask) => void;
		onclose: () => void;
	} = $props();
	let testCommand = $state('');
	let prompt = $state(''),
		provider = $state(''),
		serverId = $state(''),
		model = $state(''),
		thinking = $state(''),
		saving = $state(false),
		error = $state('');
	let pending: { key: string; id: string } | null = null;
	// The workflow a new task follows: the issue's default, Standard or a
	// custom one. Guests cannot list workflows (nor start tasks), so the
	// chooser stays hidden whenever the list does not load.
	const DEFAULT_WORKFLOW = 'default';
	let workflowList = $state<WorkflowList | null>(null),
		workflowChoice = $state(DEFAULT_WORKFLOW);
	const customWorkflows = $derived((workflowList?.workflows ?? []).filter((w) => !w.builtin && !w.archived));
	const customChosen = $derived(workflowChoice !== DEFAULT_WORKFLOW && workflowChoice !== 'standard');
	// A custom workflow's first agent step decides the harness and model; a
	// default may also name one.
	const defaultMayBeCustom = $derived((workflowList?.defaults ?? []).some((d) => d.workflow_id));
	const workflowOptions = $derived([
		{ value: DEFAULT_WORKFLOW, label: m['workflows.choose_default']() },
		{ value: 'standard', label: m['workflows.standard']() },
		...customWorkflows.map((w) => ({ value: w.id, label: w.name }))
	]);
	let disposed = false;
	// Codex and Claude Code use a compatible model server when there is one;
	// otherwise, like the other agents, their own sign-in.
	const compatibleServers = $derived(serversFor(options?.model_servers, provider));
	const server = $derived(compatibleServers.find((item) => item.id === serverId));
	const canOverride = $derived(!!server);
	// An agent no model server runs (OpenCode) chooses among its own models:
	// the server's catalog for it. A model belongs to its agent, so changing
	// the agent always starts from that agent's default again, and a catalog
	// of the workspace's model servers is left to the server picker.
	const LEVELS: ThinkingLevel[] = ['off', 'low', 'medium', 'high', 'xhigh', 'max'];
	let ownModels = $state<CatalogModel[]>([]);
	let catalogRequest = 0;
	const ownEfforts = $derived(allowedEfforts(ownModels, model, LEVELS));
	async function loadOwnModels(harness: string) {
		const request = ++catalogRequest;
		ownModels = [];
		if (!harness || serversFor(options?.model_servers, harness).length) return;
		try {
			const catalog = await agentModelCatalog(slug, harness);
			if (!disposed && request === catalogRequest && catalog.source !== 'workspace') ownModels = catalog.models;
		} catch {
			/* Without the list the task starts with the agent's own default. */
		}
	}
	function chooseOwnModel(value: string) {
		model = value;
		thinking = effortAfterModelChange(ownModels, value, thinking).effort;
	}
	function chooseProvider(value: string) {
		if (!value) return;
		provider = value;
		const preferred = preferredServer(options?.model_servers, value);
		serverId = preferred?.id ?? '';
		model = preferred?.default_model ?? '';
		thinking = preferred?.thinking ?? '';
		void loadOwnModels(value);
	}
	function chooseServer(value: string) {
		const next = compatibleServers.find((item) => item.id === value);
		serverId = next?.id ?? '';
		model = next?.default_model ?? '';
		thinking = next?.thinking ?? '';
	}
	const draftKey = $derived(
		`agent-task-draft:${authState.user?.id}:${slug}:${conversation}:${task ? `${task.id}:${task.generation}` : 'new'}`
	);
	onMount(() => {
		prompt = task?.prompt ?? initialPrompt;
		testCommand = formatTestCommand(task?.test_command);
		chooseProvider(task?.provider ?? options?.default_provider ?? options?.providers[0]?.id ?? '');
		if (!task)
			workflowsApi
				.list(slug)
				.then((list) => {
					if (disposed) return;
					workflowList = list;
					if (!workflowOptions.some((o) => o.value === workflowChoice)) workflowChoice = DEFAULT_WORKFLOW;
				})
				.catch(() => {
					/* The task still starts with the issue's default workflow. */
				});
		try {
			const saved = JSON.parse(localStorage.getItem(draftKey) ?? 'null');
			if (saved?.pending && typeof saved.prompt === 'string') {
				prompt = saved.prompt;
				pending = saved.pending;
				testCommand = formatTestCommand(saved.test_command);
				if (!task && typeof saved.workflow_id === 'string') workflowChoice = saved.workflow_id;
				if (!task && options?.providers.some((p) => p.id === saved.provider)) {
					chooseProvider(saved.provider);
					if (serversFor(options?.model_servers, saved.provider).some((item) => item.id === saved.model_server_id)) {
						serverId = saved.model_server_id;
						model = saved.model;
						thinking = saved.thinking;
					} else if (!saved.model_server_id && !serversFor(options?.model_servers, saved.provider).length) {
						// A model of the agent's own, kept so the pending request is replayed unchanged.
						model = typeof saved.model === 'string' ? saved.model : '';
						thinking = typeof saved.thinking === 'string' ? saved.thinking : '';
					}
				}
			}
		} catch {}
		return () => {
			disposed = true;
		};
	});
	async function submit(event?: SubmitEvent) {
		event?.preventDefault();
		if (saving || !prompt.trim() || new TextEncoder().encode(prompt).length > 32768 || (!task && !provider)) return;
		error = '';
		let command: string[] | undefined;
		try {
			command = parseTestCommand(testCommand);
		} catch {
			return;
		}
		saving = true;
		const input = {
			prompt: prompt.trim(),
			test_command: command,
			provider,
			// Without a model server only a model of the agent's own catalog is set.
			model: canOverride || !compatibleServers.length ? model : '',
			thinking: canOverride || !compatibleServers.length ? thinking : '',
			...(server ? { model_server_id: server.id } : {}),
			...(!task && workflowChoice !== DEFAULT_WORKFLOW ? { workflow_id: workflowChoice } : {})
		};
		const key = JSON.stringify(
			task ? { prompt: input.prompt, generation: task.generation, test_command: command } : input
		);
		if (!pending || pending.key !== key) pending = { key, id: crypto.randomUUID() };
		try {
			localStorage.setItem(draftKey, JSON.stringify({ ...input, prompt, pending }));
		} catch {}
		try {
			const result = task
				? await agentTasks.retry(slug, task.id, {
						request_id: pending.id,
						generation: task.generation,
						prompt: input.prompt,
						test_command: command
					})
				: await agentTasks.create(slug, conversation, { ...input, request_id: pending.id } satisfies TaskInput);
			try {
				localStorage.removeItem(draftKey);
			} catch {}
			if (!disposed) {
				onaccepted(result);
				onclose();
			}
		} catch (e) {
			if (!disposed)
				error = (e as { status?: number }).status === 409 ? m['tasks.conflict']() : m['tasks.action_error']();
		} finally {
			if (!disposed) saving = false;
		}
	}
</script>

<!-- Official AI Elements Model Selector and Prompt Input composition. -->
<Dialog.Root
	open
	onOpenChange={(open) => {
		if (!open) onclose();
	}}
>
	<Dialog.Content class="max-w-xl">
		<Dialog.Header
			><Dialog.Title>{task ? m['tasks.retry']() : m['tasks.start']()}</Dialog.Title><Dialog.Description
				>{m['tasks.delegate_description']()}</Dialog.Description
			></Dialog.Header
		>
		<form onsubmit={submit} class="min-w-0 space-y-4">
			{#if !task && workflowList && customWorkflows.length > 0}
				<div class="space-y-1.5">
					<Label for="task-workflow" class="text-xs font-medium text-muted-foreground"
						>{m['workflows.choose_label']()}</Label
					>
					<SelectField
						id="task-workflow"
						bind:value={workflowChoice}
						options={workflowOptions}
						disabled={saving}
						aria-describedby={customChosen || defaultMayBeCustom ? 'task-workflow-note' : undefined}
						data-testid="task-workflow"
					/>
					{#if customChosen || (workflowChoice === DEFAULT_WORKFLOW && defaultMayBeCustom)}<p
							id="task-workflow-note"
							class="text-xs text-muted-foreground"
						>
							{m['workflows.composer_note']()}
						</p>{/if}
				</div>
			{/if}
			{#if !task && !customChosen}
				<div class="space-y-1.5">
					<p id="task-harness-label" class="text-xs font-medium text-muted-foreground">{m['tasks.harness']()}</p>
					<ToggleGroup.Root
						type="single"
						variant="outline"
						spacing={1.5}
						aria-labelledby="task-harness-label"
						value={provider}
						onValueChange={chooseProvider}
						disabled={saving}
						class="w-full flex-wrap justify-start shadow-none"
					>
						{#each options?.providers ?? [] as p (p.id)}
							<!-- Separate chips (spacing > 0): each keeps its own rounded border; the selection is one
							     primary border and tint, without the focus ring doubling it. -->
							<ToggleGroup.Item
								value={p.id}
								aria-label={p.display_name}
								class="h-9 flex-none gap-2 rounded-md px-2.5 focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:ring-offset-0 data-[state=on]:border-primary data-[state=on]:bg-primary/10 data-[state=on]:text-foreground"
								><AgentLogo agent={p.id} size="sm" /><span class="text-xs">{p.display_name}</span></ToggleGroup.Item
							>
						{/each}
					</ToggleGroup.Root>
				</div>
			{/if}
			<InputGroup.Root>
				<InputGroup.Textarea
					bind:value={prompt}
					aria-label={m['tasks.instructions']()}
					maxlength={32000}
					class="min-h-16 max-h-64"
					disabled={saving}
				/>
				<InputGroup.Addon align="block-end" class="flex-wrap gap-2">
					{#if !task && customChosen}<span class="px-1 text-xs text-muted-foreground"
							>{customWorkflows.find((w) => w.id === workflowChoice)?.name}</span
						>
					{:else if !task}
						{#if compatibleServers.length > 1}
							<DropdownMenu.Root
								><DropdownMenu.Trigger
									>{#snippet child({ props })}<InputGroup.Button
											{...props}
											disabled={saving}
											aria-label={m['tasks.model_server']()}>{server?.name}<ChevronDown size={14} /></InputGroup.Button
										>{/snippet}</DropdownMenu.Trigger
								><DropdownMenu.Content
									><DropdownMenu.RadioGroup value={serverId} onValueChange={chooseServer}
										>{#each compatibleServers as item (item.id)}<DropdownMenu.RadioItem value={item.id} closeOnSelect
												>{item.name}</DropdownMenu.RadioItem
											>{/each}</DropdownMenu.RadioGroup
									></DropdownMenu.Content
								></DropdownMenu.Root
							>
						{/if}
						{#if !compatibleServers.length && provider}<span class="px-1 text-xs text-muted-foreground"
								>{m['tasks.own_sign_in']()}</span
							>{/if}
						{#if !compatibleServers.length && ownModels.length}
							<DropdownMenu.Root
								><DropdownMenu.Trigger
									>{#snippet child({ props })}<InputGroup.Button
											{...props}
											disabled={saving}
											aria-label={m['tasks.model']()}
											>{model || m['profiles.model_placeholder']()}<ChevronDown size={14} /></InputGroup.Button
										>{/snippet}</DropdownMenu.Trigger
								><DropdownMenu.Content class="max-h-72 overflow-y-auto"
									><DropdownMenu.RadioGroup value={model} onValueChange={chooseOwnModel}
										><DropdownMenu.RadioItem value="" closeOnSelect
											>{m['profiles.model_placeholder']()}</DropdownMenu.RadioItem
										>{#if model && !ownModels.some((item) => item.id === model)}<DropdownMenu.RadioItem
												value={model}
												closeOnSelect>{model}</DropdownMenu.RadioItem
											>{/if}{#each ownModels as item (item.id)}<DropdownMenu.RadioItem value={item.id} closeOnSelect
												>{item.id}</DropdownMenu.RadioItem
											>{/each}</DropdownMenu.RadioGroup
									></DropdownMenu.Content
								></DropdownMenu.Root
							>
							<DropdownMenu.Root
								><DropdownMenu.Trigger
									>{#snippet child({ props })}<InputGroup.Button
											{...props}
											disabled={saving}
											aria-label={m['tasks.effort']()}
											>{m['tasks.effort']()}: {thinking || m['settings.agents.effort_agent']()}<ChevronDown
												size={14}
											/></InputGroup.Button
										>{/snippet}</DropdownMenu.Trigger
								><DropdownMenu.Content
									><DropdownMenu.RadioGroup bind:value={thinking}
										><DropdownMenu.RadioItem value="" closeOnSelect
											>{m['settings.agents.effort_agent']()}</DropdownMenu.RadioItem
										>{#each ownEfforts as value (value)}<DropdownMenu.RadioItem {value} closeOnSelect
												>{value}</DropdownMenu.RadioItem
											>{/each}</DropdownMenu.RadioGroup
									></DropdownMenu.Content
								></DropdownMenu.Root
							>
						{/if}
						{#if canOverride}
							<DropdownMenu.Root
								><DropdownMenu.Trigger
									>{#snippet child({ props })}<InputGroup.Button
											{...props}
											disabled={saving}
											aria-label={m['tasks.model']()}>{model}<ChevronDown size={14} /></InputGroup.Button
										>{/snippet}</DropdownMenu.Trigger
								><DropdownMenu.Content
									><DropdownMenu.RadioGroup bind:value={model}
										>{#each server?.models.length ? server.models : [server?.default_model ?? ''] as value (value)}<DropdownMenu.RadioItem
												{value}
												closeOnSelect>{value}</DropdownMenu.RadioItem
											>{/each}</DropdownMenu.RadioGroup
									></DropdownMenu.Content
								></DropdownMenu.Root
							>
							<DropdownMenu.Root
								><DropdownMenu.Trigger
									>{#snippet child({ props })}<InputGroup.Button
											{...props}
											disabled={saving}
											aria-label={m['tasks.effort']()}
											>{m['tasks.effort']()}: {thinking}<ChevronDown size={14} /></InputGroup.Button
										>{/snippet}</DropdownMenu.Trigger
								><DropdownMenu.Content
									><DropdownMenu.RadioGroup bind:value={thinking}
										>{#each server?.thinking_levels ?? [] as value (value)}<DropdownMenu.RadioItem {value} closeOnSelect
												>{value}</DropdownMenu.RadioItem
											>{/each}</DropdownMenu.RadioGroup
									></DropdownMenu.Content
								></DropdownMenu.Root
							>
						{/if}
					{:else}<span class="text-xs text-muted-foreground">{task.provider}{task.model ? ` · ${task.model}` : ''}</span
						>{/if}
				</InputGroup.Addon>
			</InputGroup.Root>
			<TestCommandField bind:value={testCommand} disabled={saving} />
			{#if error}<p role="alert" class="text-sm text-destructive">{error}</p>{/if}
			<Dialog.Footer
				><Button type="button" variant="outline" onclick={onclose}>{m['tasks.close']()}</Button><Button
					type="submit"
					disabled={saving || !prompt.trim() || new TextEncoder().encode(prompt).length > 32768 || (!task && !provider)}
					>{saving ? m['tasks.saving']() : task ? m['tasks.retry']() : m['tasks.delegate']()}</Button
				></Dialog.Footer
			>
		</form>
	</Dialog.Content>
</Dialog.Root>
