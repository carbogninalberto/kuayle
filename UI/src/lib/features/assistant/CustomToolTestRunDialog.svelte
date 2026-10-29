<script lang="ts">
	import { untrack } from 'svelte';
	import * as Dialog from '$lib/components/ui/dialog/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Checkbox } from '$lib/components/ui/checkbox/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import SelectField from '$lib/components/shared/SelectField.svelte';
	import {
		assistantTools,
		type AssistantCustomTool,
		type AssistantCustomToolVersion,
		type AssistantToolRun
	} from '$lib/api/assistant-tools';
	import { listDevMachines } from '$lib/api/dev-machines';
	import type { DevMachine } from '$lib/types/dev-machine';
	import { authState } from '$lib/features/auth/auth.state.svelte';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/paraglide/messages.js';
	import ToolRunView from './ToolRunView.svelte';
	import { coerceInputs, type SchemaIssue } from './tool-schema';
	import { schemaIssueText } from './custom-tool-labels';

	// An owner's or admin's test run of a saved version with sample inputs, on
	// one of their machines or an issue's task machine. The server checks the
	// role, the machine and the inputs again; the run is polled until it ends.
	let {
		open = $bindable(false),
		slug,
		tool,
		versions,
		onran
	}: {
		open?: boolean;
		slug: string;
		tool: AssistantCustomTool;
		versions: AssistantCustomToolVersion[];
		onran?: () => void;
	} = $props();

	const pollMs = 1500;
	let target = $state<'machine' | 'issue'>('machine');
	let machines = $state<DevMachine[]>([]);
	let machineId = $state('');
	let identifier = $state('');
	let version = $state(0);
	// Form values per parameter: text for strings, numbers and enums, flags for booleans.
	let texts = $state<Record<string, string>>({});
	let flags = $state<Record<string, boolean>>({});
	let issue = $state<SchemaIssue | null>(null);
	let run = $state<AssistantToolRun | null>(null);
	let starting = $state(false);
	let session = 0;

	$effect(() => {
		if (!open) {
			session++;
			return;
		}
		untrack(() => {
			target = 'machine';
			identifier = '';
			version = tool.latest_version;
			texts = {};
			flags = {};
			issue = null;
			run = null;
			void loadMachines();
		});
	});

	async function loadMachines() {
		try {
			const page = await listDevMachines(slug, undefined, 1, 100);
			// Test runs use machines you created; the server refuses any other.
			machines = page.data.filter((machine) => machine.created_by_user_id === authState.user?.id);
			if (!machines.some((machine) => machine.id === machineId)) machineId = machines[0]?.id ?? '';
		} catch {
			machines = [];
		}
	}

	const schema = $derived(versions.find((entry) => entry.version === version)?.parameters ?? tool.parameters);
	const fields = $derived(Object.entries(schema.properties));
	const required = $derived(new Set(schema.required ?? []));
	// Every field has a value before it is bound.
	$effect(() => {
		for (const [name, field] of fields) {
			if (field.type === 'boolean') flags[name] ??= false;
			else texts[name] ??= '';
		}
	});
	const targets = $derived([
		{ value: 'machine' as const, label: m['assistant_custom_tools.target_machine']() },
		{ value: 'issue' as const, label: m['assistant_custom_tools.target_issue']() }
	]);
	const machineOptions = $derived(machines.map((machine) => ({ value: machine.id, label: `${machine.name} · ${machine.status}` })));
	const versionOptions = $derived(
		versions.map((entry) => ({
			value: entry.version,
			label: m['assistant_custom_tools.version_option']({
				version: String(entry.version),
				status:
					entry.status === 'active'
						? m['assistant_custom_tools.status_active']()
						: m['assistant_custom_tools.status_draft']()
			})
		}))
	);
	const running = $derived(run !== null && (run.status === 'queued' || run.status === 'running'));
	const ready = $derived(target === 'machine' ? machineId !== '' : identifier.trim() !== '');

	async function poll(id: string, current: number) {
		while (current === session) {
			await new Promise((resolve) => setTimeout(resolve, pollMs));
			if (current !== session) return;
			try {
				const next = await assistantTools.run(slug, id);
				if (current !== session) return;
				run = next;
				if (next.status !== 'queued' && next.status !== 'running') {
					onran?.();
					return;
				}
			} catch (err) {
				appToast.apiError(err, m['assistant_custom_tools.failed_run']());
				return;
			}
		}
	}

	async function start() {
		if (starting || running || !ready) return;
		const coerced = coerceInputs(schema, { ...texts, ...flags });
		issue = coerced.error ?? null;
		if (!coerced.inputs) return;
		starting = true;
		const current = session;
		try {
			const started = await assistantTools.testRun(slug, tool.id, {
				inputs: coerced.inputs,
				...(target === 'machine' ? { machine_id: machineId } : { identifier: identifier.trim().toUpperCase() }),
				version
			});
			if (current !== session) return;
			run = started;
			onran?.();
			void poll(started.id, current);
		} catch (err) {
			appToast.apiError(err, m['assistant_custom_tools.failed_run']());
		} finally {
			starting = false;
		}
	}
</script>

<Dialog.Root bind:open>
	<Dialog.Content class="max-h-[90vh] overflow-y-auto sm:max-w-xl">
		<Dialog.Header>
			<Dialog.Title>{m['assistant_custom_tools.test_title']({ name: tool.name })}</Dialog.Title>
			<Dialog.Description>{m['assistant_custom_tools.test_desc']()}</Dialog.Description>
		</Dialog.Header>
		<form
			class="grid min-w-0 gap-4"
			onsubmit={(event) => {
				event.preventDefault();
				void start();
			}}
		>
			<div class="grid gap-3 sm:grid-cols-2">
				<div class="grid gap-1.5">
					<Label for="tool-test-target">{m['assistant_custom_tools.target']()}</Label>
					<SelectField
						id="tool-test-target"
						bind:value={target}
						options={targets}
						aria-label={m['assistant_custom_tools.target']()}
					/>
				</div>
				<div class="grid gap-1.5">
					<Label for="tool-test-version">{m['assistant_custom_tools.version']()}</Label>
					<SelectField
						id="tool-test-version"
						bind:value={version}
						options={versionOptions}
						aria-label={m['assistant_custom_tools.version']()}
					/>
				</div>
			</div>
			{#if target === 'machine'}
				<div class="grid gap-1.5">
					<Label for="tool-test-machine">{m['assistant_custom_tools.machine']()}</Label>
					{#if machines.length}
						<SelectField
							id="tool-test-machine"
							bind:value={machineId}
							options={machineOptions}
							aria-label={m['assistant_custom_tools.machine']()}
						/>
					{:else}
						<p class="text-xs text-muted-foreground">{m['assistant_custom_tools.no_machines']()}</p>
					{/if}
				</div>
			{:else}
				<div class="grid gap-1.5">
					<Label for="tool-test-identifier">{m['assistant_custom_tools.identifier']()}</Label>
					<Input
						id="tool-test-identifier"
						bind:value={identifier}
						maxlength={32}
						autocomplete="off"
						placeholder="ENG-1"
						aria-describedby="tool-test-identifier-hint"
					/>
					<p id="tool-test-identifier-hint" class="text-xs text-muted-foreground">
						{m['assistant_custom_tools.identifier_hint']()}
					</p>
				</div>
			{/if}
			{#if fields.length}
				<fieldset class="grid min-w-0 gap-3">
					<legend class="mb-1 text-sm font-medium">{m['assistant_custom_tools.inputs']()}</legend>
					{#each fields as [name, field] (name)}
						{@const id = `tool-test-input-${name}`}
						{#if field.type === 'boolean'}
							<div class="flex items-start gap-2">
								{#if flags[name] !== undefined}<Checkbox {id} bind:checked={flags[name]} class="mt-0.5" />{/if}
								<div class="grid gap-0.5">
									<Label for={id} class="font-mono font-normal">{name}</Label>
									{#if field.description}<p class="text-xs text-muted-foreground">{field.description}</p>{/if}
								</div>
							</div>
						{:else}
							<div class="grid gap-1.5">
								<Label for={id} class="font-mono"
									>{name}{#if required.has(name)}<span aria-hidden="true" class="text-destructive">*</span>{/if}</Label
								>
								{#if texts[name] === undefined}
									<!-- The field gets its value on the next update. -->
								{:else if field.enum}
									<SelectField
										{id}
										bind:value={texts[name]}
										options={[
											...(required.has(name) ? [] : [{ value: '', label: m['assistant_custom_tools.not_set']() }]),
											...field.enum.map((value) => ({ value, label: value }))
										]}
										aria-label={name}
									/>
								{:else}
									<Input
										{id}
										bind:value={texts[name]}
										inputmode={field.type === 'string' ? undefined : 'decimal'}
										maxlength={field.maxLength}
										aria-required={required.has(name)}
										autocomplete="off"
										spellcheck={false}
										class="font-mono"
									/>
								{/if}
								{#if field.description}<p class="text-xs text-muted-foreground">{field.description}</p>{/if}
							</div>
						{/if}
					{/each}
				</fieldset>
			{/if}
			{#if issue}<p role="alert" class="text-xs text-destructive">{schemaIssueText(issue)}</p>{/if}
			{#if run}
				<ToolRunView
					status={run.status}
					exitCode={run.exit_code}
					output={run.output}
					truncated={run.output_truncated}
					error={run.error}
					durationMs={run.duration_ms}
				/>
			{/if}
			<Dialog.Footer class="gap-2">
				<Button type="button" variant="outline" onclick={() => (open = false)}
					>{m['assistant_custom_tools.close']()}</Button
				>
				<Button type="submit" disabled={starting || running || !ready}
					>{running ? m['assistant_custom_tools.running']() : m['assistant_custom_tools.run']()}</Button
				>
			</Dialog.Footer>
		</form>
	</Dialog.Content>
</Dialog.Root>
