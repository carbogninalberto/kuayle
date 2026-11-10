<script lang="ts">
	import * as Tabs from '$lib/components/ui/tabs/index.js';
	import * as CodeBlock from '$lib/components/ui/code-block/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import { Textarea } from '$lib/components/ui/textarea/index.js';
	import SelectField from '$lib/components/shared/SelectField.svelte';
	import type { AssistantToolInterpreter } from '$lib/api/assistant-tools';
	import { m } from '$lib/features/i18n/messages';
	import ToolParameterBuilder from './ToolParameterBuilder.svelte';
	import { buildSchema, type ParamDraft } from './tool-schema';
	import { schemaIssueText } from './custom-tool-labels';

	// What a custom tool is: its name, description, interpreter, script and
	// parameters. Shared by the editor and the creation wizard. The script is
	// plain text in a monospace Textarea: no editor primitive exists, and the
	// text is never evaluated in the browser.
	let {
		name = $bindable(''),
		description = $bindable(''),
		interpreter = $bindable<AssistantToolInterpreter>('sh'),
		script = $bindable(''),
		params = $bindable<ParamDraft[]>([]),
		view = $bindable('builder'),
		interpreters
	}: {
		name?: string;
		description?: string;
		interpreter?: AssistantToolInterpreter;
		script?: string;
		params?: ParamDraft[];
		view?: string;
		interpreters: AssistantToolInterpreter[];
	} = $props();

	const toolName = /^[a-z][a-z0-9_]{0,47}$/;
	const built = $derived(buildSchema(params));
	const nameValid = $derived(toolName.test(name.trim()));
	const interpreterOptions = $derived(interpreters.map((value) => ({ value, label: value })));
</script>

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
