<script lang="ts">
	import { untrack } from 'svelte';
	import * as Dialog from '$lib/components/ui/dialog/index.js';
	import * as Tabs from '$lib/components/ui/tabs/index.js';
	import * as CodeBlock from '$lib/components/ui/code-block/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Checkbox } from '$lib/components/ui/checkbox/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import { Textarea } from '$lib/components/ui/textarea/index.js';
	import SelectField from '$lib/components/shared/SelectField.svelte';
	import {
		assistantTools,
		type AssistantCustomTool,
		type AssistantCustomToolContent,
		type AssistantToolInterpreter,
		type AssistantToolRunMode,
		type AssistantToolStatus
	} from '$lib/api/assistant-tools';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/paraglide/messages.js';
	import ToolParameterBuilder from './ToolParameterBuilder.svelte';
	import { buildSchema, paramsFromSchema, type ParamDraft } from './tool-schema';
	import { schemaIssueText } from './custom-tool-labels';

	// Creates a custom tool, or saves an edit of `tool` as its next version.
	// The script is plain text in a monospace Textarea: no editor primitive
	// exists, and the text is never evaluated in the browser.
	let {
		open = $bindable(false),
		slug,
		interpreters,
		tool = null,
		initial = null,
		onsaved
	}: {
		open?: boolean;
		slug: string;
		interpreters: AssistantToolInterpreter[];
		tool?: AssistantCustomTool | null;
		/** The content to start editing from (a pending draft, or the tool's own). */
		initial?: AssistantCustomToolContent | null;
		onsaved: (tool: AssistantCustomTool) => void;
	} = $props();

	const toolName = /^[a-z][a-z0-9_]{0,47}$/;
	let name = $state('');
	let description = $state('');
	let interpreter = $state<AssistantToolInterpreter>('sh');
	let script = $state('');
	let params = $state<ParamDraft[]>([]);
	let runMode = $state<AssistantToolRunMode>('approval');
	let readOnly = $state(false);
	let timeout = $state('600');
	let view = $state('builder');
	let saving = $state(false);

	// Reset the form each time the dialog opens, not when `initial` changes.
	$effect(() => {
		if (!open) return;
		untrack(() => {
			name = initial?.name ?? '';
			description = initial?.description ?? '';
			interpreter = initial?.interpreter ?? interpreters[0] ?? 'sh';
			script = initial?.script ?? '';
			params = paramsFromSchema(initial?.parameters);
			runMode = initial?.run_mode ?? 'approval';
			readOnly = initial?.read_only ?? false;
			timeout = String(initial?.timeout_seconds ?? 600);
			view = 'builder';
		});
	});

	// Auto runs are allowed only for read-only tools; the database enforces it too.
	$effect(() => {
		if (!readOnly && runMode === 'auto') runMode = 'approval';
	});

	const built = $derived(buildSchema(params));
	const timeoutSeconds = $derived(Number(timeout));
	const timeoutValid = $derived(Number.isInteger(timeoutSeconds) && timeoutSeconds >= 10 && timeoutSeconds <= 1800);
	const nameValid = $derived(toolName.test(name.trim()));
	const valid = $derived(
		nameValid && description.trim() !== '' && script.trim() !== '' && !built.error && timeoutValid
	);
	const interpreterOptions = $derived(interpreters.map((value) => ({ value, label: value })));
	const modes = $derived([
		{ value: 'approval' as const, label: m['assistant_custom_tools.mode_approval']() },
		{ value: 'auto' as const, label: m['assistant_custom_tools.mode_auto'](), disabled: !readOnly }
	]);

	async function save(status: AssistantToolStatus | undefined) {
		if (!valid || saving || !built.schema) return;
		saving = true;
		const content: AssistantCustomToolContent = {
			name: name.trim(),
			description: description.trim(),
			interpreter,
			script,
			parameters: built.schema,
			run_mode: runMode,
			read_only: readOnly,
			timeout_seconds: timeoutSeconds
		};
		try {
			const saved = tool
				? await assistantTools.update(slug, tool.id, status ? { ...content, status } : content)
				: await assistantTools.create(slug, { ...content, status: status ?? 'active' });
			appToast.success(
				tool
					? m['assistant_custom_tools.saved_toast']({ name: saved.name })
					: m['assistant_custom_tools.created_toast']({ name: saved.name })
			);
			open = false;
			onsaved(saved);
		} catch (err) {
			appToast.apiError(err, m['assistant_custom_tools.failed_save']());
		} finally {
			saving = false;
		}
	}
</script>

<Dialog.Root bind:open>
	<Dialog.Content class="max-h-[90vh] overflow-y-auto sm:max-w-2xl">
		<Dialog.Header>
			<Dialog.Title
				>{tool
					? m['assistant_custom_tools.edit_title']({ name: tool.name })
					: m['assistant_custom_tools.new_title']()}</Dialog.Title
			>
			<Dialog.Description>{m['assistant_custom_tools.editor_desc']()}</Dialog.Description>
		</Dialog.Header>
		<form
			class="grid min-w-0 gap-4"
			onsubmit={(event) => {
				event.preventDefault();
				void save(tool?.status === 'draft' ? undefined : 'active');
			}}
		>
			<div class="grid gap-1.5">
				<Label for="tool-name">{m['assistant_custom_tools.field_name']()}</Label>
				<Input
					id="tool-name"
					bind:value={name}
					maxlength={48}
					required
					autocomplete="off"
					spellcheck={false}
					class="font-mono"
					aria-invalid={name.trim() !== '' && !nameValid}
					aria-describedby="tool-name-hint"
				/>
				<p id="tool-name-hint" class="text-xs break-words text-muted-foreground">
					{m['assistant_custom_tools.name_hint']()}
					<code class="rounded bg-muted px-1 font-mono">custom_{name.trim() || 'name'}</code>
				</p>
			</div>
			<div class="grid gap-1.5">
				<Label for="tool-description">{m['assistant_custom_tools.field_description']()}</Label>
				<Textarea
					id="tool-description"
					bind:value={description}
					maxlength={1000}
					rows={2}
					required
					aria-describedby="tool-description-hint"
				/>
				<p id="tool-description-hint" class="text-xs text-muted-foreground">
					{m['assistant_custom_tools.description_hint']()}
				</p>
			</div>
			<div class="grid gap-1.5 sm:max-w-48">
				<Label for="tool-interpreter">{m['assistant_custom_tools.field_interpreter']()}</Label>
				<SelectField
					id="tool-interpreter"
					bind:value={interpreter}
					options={interpreterOptions}
					aria-label={m['assistant_custom_tools.field_interpreter']()}
				/>
			</div>
			<div class="grid gap-1.5">
				<Label for="tool-script">{m['assistant_custom_tools.field_script']()}</Label>
				<Textarea
					id="tool-script"
					bind:value={script}
					required
					rows={10}
					spellcheck={false}
					class="max-h-[50vh] min-h-40 font-mono text-xs"
					aria-describedby="tool-script-hint"
				/>
				<p id="tool-script-hint" class="text-xs break-words text-muted-foreground">
					{m['assistant_custom_tools.script_hint']()}
				</p>
			</div>
			<Tabs.Root bind:value={view} class="min-w-0 gap-2">
				<div class="flex flex-wrap items-center justify-between gap-2">
					<span class="text-sm font-medium">{m['assistant_custom_tools.field_parameters']()}</span>
					<Tabs.List>
						<Tabs.Trigger value="builder">{m['assistant_custom_tools.builder']()}</Tabs.Trigger>
						<Tabs.Trigger value="json">{m['assistant_custom_tools.json']()}</Tabs.Trigger>
					</Tabs.List>
				</div>
				<Tabs.Content value="builder"><ToolParameterBuilder bind:params /></Tabs.Content>
				<Tabs.Content value="json">
					<CodeBlock.Root size="sm"
						><CodeBlock.Content
							code={built.schema ? JSON.stringify(built.schema, null, 2) : ''}
							wrap
							class="max-h-72 text-xs"
						/></CodeBlock.Root
					>
				</Tabs.Content>
				{#if built.error}<p role="alert" class="text-xs break-words text-destructive">
						{schemaIssueText(built.error)}
					</p>{/if}
			</Tabs.Root>
			<div class="grid gap-3 sm:grid-cols-2">
				<div class="grid gap-1.5">
					<Label for="tool-run-mode">{m['assistant_custom_tools.field_run_mode']()}</Label>
					<SelectField
						id="tool-run-mode"
						bind:value={runMode}
						options={modes}
						aria-label={m['assistant_custom_tools.field_run_mode']()}
					/>
				</div>
				<div class="grid gap-1.5">
					<Label for="tool-timeout">{m['assistant_custom_tools.field_timeout']()}</Label>
					<Input
						id="tool-timeout"
						bind:value={timeout}
						inputmode="numeric"
						autocomplete="off"
						aria-invalid={!timeoutValid}
						aria-describedby="tool-timeout-hint"
					/>
					<p id="tool-timeout-hint" class="text-xs text-muted-foreground">
						{m['assistant_custom_tools.timeout_hint']()}
					</p>
				</div>
			</div>
			<div class="grid gap-1">
				<div class="flex items-start gap-2">
					<Checkbox id="tool-read-only" bind:checked={readOnly} class="mt-0.5" />
					<Label for="tool-read-only" class="font-normal">{m['assistant_custom_tools.field_read_only']()}</Label>
				</div>
				<p class="text-xs text-muted-foreground">{m['assistant_custom_tools.run_mode_hint']()}</p>
			</div>
			<Dialog.Footer class="flex-wrap gap-2">
				<Button type="button" variant="outline" onclick={() => (open = false)}
					>{m['assistant_custom_tools.cancel']()}</Button
				>
				{#if tool?.status === 'draft'}
					<Button type="submit" disabled={!valid || saving}>{m['assistant_custom_tools.save_draft_only']()}</Button>
				{:else}
					<Button type="button" variant="outline" disabled={!valid || saving} onclick={() => void save('draft')}
						>{m['assistant_custom_tools.save_draft']()}</Button
					>
					<Button type="submit" disabled={!valid || saving}
						>{tool ? m['assistant_custom_tools.save_apply']() : m['assistant_custom_tools.create']()}</Button
					>
				{/if}
			</Dialog.Footer>
		</form>
	</Dialog.Content>
</Dialog.Root>
