<script lang="ts">
	import { onMount } from 'svelte';
	import TestCommandField from '$lib/features/dev-machines/TestCommandField.svelte';
	import { parseTestCommand, formatTestCommand } from '$lib/features/dev-machines/test-command';
	import { ChevronDown } from '@lucide/svelte';
	import * as Dialog from '$lib/components/ui/dialog/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import * as InputGroup from '$lib/components/ui/input-group/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { agentTasks, type AgentTask, type TaskOptions, type TaskInput } from '$lib/api/agent-tasks';
	import { authState } from '$lib/features/auth/auth.state.svelte';
	import { m } from '$lib/paraglide/messages.js';
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
		model = $state(''),
		thinking = $state(''),
		saving = $state(false),
		error = $state('');
	let pending: { key: string; id: string } | null = null;
	let disposed = false;
	const canOverride = $derived(options?.model_provider.configured && ['codex', 'claude-code'].includes(provider));
	const draftKey = $derived(
		`agent-task-draft:${authState.user?.id}:${slug}:${conversation}:${task ? `${task.id}:${task.generation}` : 'new'}`
	);
	onMount(() => {
		prompt = task?.prompt ?? initialPrompt;
		testCommand = formatTestCommand(task?.test_command);
		provider = task?.provider ?? options?.default_provider ?? options?.providers[0]?.id ?? '';
		model = options?.model_provider.configured ? options.model_provider.default_model : '';
		thinking = options?.model_provider.configured ? options.model_provider.thinking : '';
		try {
			const saved = JSON.parse(localStorage.getItem(draftKey) ?? 'null');
			if (saved?.pending && typeof saved.prompt === 'string') {
				prompt = saved.prompt;
				pending = saved.pending;
				testCommand = formatTestCommand(saved.test_command);
				if (!task && options?.providers.some((p) => p.id === saved.provider)) {
					provider = saved.provider;
					model = saved.model;
					thinking = saved.thinking;
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
			model: canOverride ? model : '',
			thinking: canOverride ? thinking : ''
		};
		const key = JSON.stringify(task ? { prompt: input.prompt, generation: task.generation, test_command: command } : input);
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
			<InputGroup.Root>
				<InputGroup.Textarea
					bind:value={prompt}
					aria-label={m['tasks.instructions']()}
					maxlength={32000}
					class="min-h-16 max-h-64"
					disabled={saving}
				/>
				<InputGroup.Addon align="block-end" class="flex-wrap gap-2">
					{#if !task}
						<DropdownMenu.Root
							><DropdownMenu.Trigger
								>{#snippet child({ props })}<InputGroup.Button
										{...props}
										disabled={saving}
										aria-label={m['tasks.harness']()}
										>{options?.providers.find((p) => p.id === provider)?.display_name ??
											m['tasks.harness']()}<ChevronDown size={14} /></InputGroup.Button
									>{/snippet}</DropdownMenu.Trigger
							>
							<DropdownMenu.Content
								><DropdownMenu.RadioGroup bind:value={provider}
									>{#each options?.providers ?? [] as p}<DropdownMenu.RadioItem value={p.id} closeOnSelect
											>{p.display_name}</DropdownMenu.RadioItem
										>{/each}</DropdownMenu.RadioGroup
								></DropdownMenu.Content
							>
						</DropdownMenu.Root>
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
										>{#each options?.model_provider.models ?? [] as value}<DropdownMenu.RadioItem {value} closeOnSelect
												>{value}</DropdownMenu.RadioItem
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
										>{#each options?.model_provider.thinking_levels ?? [] as value}<DropdownMenu.RadioItem
												{value}
												closeOnSelect>{value}</DropdownMenu.RadioItem
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
