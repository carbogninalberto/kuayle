<script lang="ts">
	import { untrack } from 'svelte';
	import { LoaderCircle, ShieldCheck, Sparkles } from '@lucide/svelte';
	import * as Dialog from '$lib/components/ui/dialog/index.js';
	import * as CodeBlock from '$lib/components/ui/code-block/index.js';
	import { Alert, AlertAction, AlertDescription } from '$lib/components/ui/alert/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import { Textarea } from '$lib/components/ui/textarea/index.js';
	import SelectField from '$lib/components/shared/SelectField.svelte';
	import InfoHint from '$lib/components/shared/InfoHint.svelte';
	import WizardSteps from '$lib/components/shared/WizardSteps.svelte';
	import { aiWrite } from '$lib/api/ai-write';
	import {
		assistantTools,
		type AssistantCustomTool,
		type AssistantToolInterpreter,
		type AssistantToolRunMode,
		type AssistantToolStatus
	} from '$lib/api/assistant-tools';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/features/i18n/messages';
	import CustomToolFields from './CustomToolFields.svelte';
	import CustomToolRunFields from './CustomToolRunFields.svelte';
	import { buildSchema, schemaSummary, type ParamDraft } from './tool-schema';
	import { parseToolDraft, toolInstruction, toolNameFrom } from './creation-wizards';

	// Creates a custom tool in three steps: describe it, draft its fields (with
	// the workspace's AI model, which answers JSON that only fills the tool's
	// own fields, or by hand when AI is off), then choose how it runs and save,
	// optionally as a draft to test-run at once. Tools run without credentials
	// and the AI is told never to write any. The API validates the save.
	let {
		open = $bindable(false),
		slug,
		interpreters,
		onsaved
	}: {
		open?: boolean;
		slug: string;
		interpreters: AssistantToolInterpreter[];
		/** `test`: the user asked to test-run the saved draft. */
		onsaved: (tool: AssistantCustomTool, options: { test: boolean }) => void;
	} = $props();

	const formId = 'tool-wizard-form';
	const toolName = /^[a-z][a-z0-9_]{0,47}$/;
	let step = $state(0);
	let what = $state('');
	let service = $state('');
	let inputs = $state('');
	let ai = $state<'idle' | 'working' | 'off' | 'unreadable'>('idle');
	let name = $state('');
	let description = $state('');
	let interpreter = $state<AssistantToolInterpreter>('sh');
	let script = $state('');
	let params = $state<ParamDraft[]>([]);
	let view = $state('builder');
	let runMode = $state<AssistantToolRunMode>('approval');
	let readOnly = $state(false);
	let timeout = $state('600');
	let status = $state<AssistantToolStatus>('active');
	let saving = $state(false);

	$effect(() => {
		if (!open) return;
		untrack(() => {
			step = 0;
			what = service = inputs = name = description = script = '';
			params = [];
			interpreter = interpreters[0] ?? 'sh';
			view = 'builder';
			runMode = 'approval';
			readOnly = false;
			timeout = '600';
			status = 'active';
			ai = 'idle';
		});
	});

	const steps = [m['wizard.step_describe'](), m['wizard.step_draft'](), m['wizard.step_review']()];
	const built = $derived(buildSchema(params));
	const timeoutSeconds = $derived(Number(timeout));
	const timeoutValid = $derived(Number.isInteger(timeoutSeconds) && timeoutSeconds >= 10 && timeoutSeconds <= 1800);
	const fieldsValid = $derived(
		toolName.test(name.trim()) && description.trim() !== '' && script.trim() !== '' && !built.error
	);
	const statuses = [
		{ value: 'active' as const, label: m['assistant_custom_tools.status_active']() },
		{ value: 'draft' as const, label: m['assistant_custom_tools.status_draft']() }
	];

	function toDraft() {
		if (!what.trim()) return;
		// By hand, start from the answers; a later AI draft replaces them.
		if (!name && !description && !script) {
			name = toolNameFrom(what);
			description = what.trim().slice(0, 1000);
		}
		step = 1;
	}

	async function generate() {
		if (ai === 'working') return;
		ai = 'working';
		const current = script.trim()
			? JSON.stringify({ name, description, interpreter, script, parameters: built.schema ?? {} })
			: '';
		try {
			const { text } = await aiWrite(slug, {
				kind: 'custom_tool',
				mode: 'generate',
				instruction: toolInstruction({ what, service, inputs }),
				source: current
			});
			const draft = parseToolDraft(text, interpreters);
			if (!draft) {
				ai = 'unreadable';
				return;
			}
			name = draft.name;
			description = draft.description;
			interpreter = draft.interpreter;
			script = draft.script;
			params = draft.params;
			readOnly = draft.readOnly;
			timeout = draft.timeout;
			view = 'builder';
			ai = 'idle';
			appToast.success(m['ai_editor.done']());
		} catch (error) {
			const code = (error as { error?: { code?: string } }).error?.code;
			if (code === 'AI_NOT_CONFIGURED') ai = 'off';
			else {
				ai = 'idle';
				appToast.apiError(error, m['ai_editor.failed']());
			}
		}
	}

	async function save(test: boolean) {
		if (!fieldsValid || !timeoutValid || saving || !built.schema) return;
		saving = true;
		try {
			const saved = await assistantTools.create(slug, {
				name: name.trim(),
				description: description.trim(),
				interpreter,
				script,
				parameters: built.schema,
				run_mode: runMode,
				read_only: readOnly,
				timeout_seconds: timeoutSeconds,
				status: test ? 'draft' : status
			});
			appToast.success(m['assistant_custom_tools.created_toast']({ name: saved.name }));
			open = false;
			onsaved(saved, { test });
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
			<Dialog.Title>{m['assistant_custom_tools.new_title']()}</Dialog.Title>
			<Dialog.Description>{m['tool_wizard.desc']()}</Dialog.Description>
		</Dialog.Header>
		<WizardSteps {steps} current={step} label={m['wizard.progress']()} />

		<form
			id={formId}
			class="grid min-w-0 gap-4"
			onsubmit={(event) => {
				event.preventDefault();
				if (step === 0) toDraft();
				else if (step === 1) {
					if (fieldsValid) step = 2;
				} else void save(false);
			}}
		>
			{#if step === 0}
				<div class="grid gap-1.5">
					<Label for="tool-what">{m['tool_wizard.what']()}</Label>
					<Textarea
						id="tool-what"
						bind:value={what}
						rows={3}
						maxlength={1000}
						required
						placeholder={m['tool_wizard.what_placeholder']()}
					/>
				</div>
				<div class="grid gap-1.5">
					<Label for="tool-service">{m['tool_wizard.service']()}</Label>
					<Input
						id="tool-service"
						bind:value={service}
						maxlength={400}
						autocomplete="off"
						spellcheck={false}
						placeholder={m['tool_wizard.service_placeholder']()}
					/>
				</div>
				<div class="grid gap-1.5">
					<Label for="tool-inputs">{m['tool_wizard.inputs']()}</Label>
					<Input
						id="tool-inputs"
						bind:value={inputs}
						maxlength={400}
						autocomplete="off"
						placeholder={m['tool_wizard.inputs_placeholder']()}
					/>
				</div>
				<p class="flex items-start gap-2 text-xs text-muted-foreground">
					<ShieldCheck class="mt-px size-3.5 shrink-0" />{m['tool_wizard.no_secrets']()}
				</p>
			{:else if step === 1}
				<div class="flex flex-wrap items-center gap-2">
					<Button type="button" size="sm" disabled={ai === 'working'} onclick={generate}
						>{#if ai === 'working'}<LoaderCircle class="animate-spin" />{:else}<Sparkles />{/if}{m[
							'wizard.generate'
						]()}</Button
					>
					<InfoHint text={m['tool_wizard.draft_hint']()} />
				</div>
				{#if ai === 'off'}
					<Alert>
						<AlertDescription>{m['tool_wizard.ai_off']()}</AlertDescription>
						<AlertAction>
							<Button size="sm" variant="outline" href="/{slug}/settings/ai">{m['wizard.ai_settings']()}</Button>
						</AlertAction>
					</Alert>
				{:else if ai === 'unreadable'}
					<Alert>
						<AlertDescription>{m['tool_wizard.unreadable']()}</AlertDescription>
					</Alert>
				{/if}
				<CustomToolFields
					bind:name
					bind:description
					bind:interpreter
					bind:script
					bind:params
					bind:view
					{interpreters}
				/>
			{:else}
				<dl class="grid gap-x-4 gap-y-1.5 rounded-lg border bg-muted/30 px-3 py-2.5 text-sm sm:grid-cols-[auto_1fr]">
					<dt class="text-muted-foreground">{m['assistant_custom_tools.field_name']()}</dt>
					<dd class="min-w-0 font-mono break-words">custom_{name.trim()}</dd>
					<dt class="text-muted-foreground">{m['assistant_custom_tools.field_description']()}</dt>
					<dd class="min-w-0 break-words">{description.trim()}</dd>
					<dt class="text-muted-foreground">{m['assistant_custom_tools.field_interpreter']()}</dt>
					<dd class="font-mono">{interpreter}</dd>
					<dt class="text-muted-foreground">{m['assistant_custom_tools.field_parameters']()}</dt>
					<dd class="min-w-0 font-mono text-xs break-words">
						{schemaSummary(built.schema) || m['assistant_custom_tools.no_parameters']()}
					</dd>
				</dl>
				<CodeBlock.Root size="sm"><CodeBlock.Content code={script} wrap class="max-h-48 text-xs" /></CodeBlock.Root>
				<CustomToolRunFields bind:runMode bind:readOnly bind:timeout />
				<div class="grid gap-1.5 sm:max-w-48">
					<div class="flex items-center gap-1">
						<Label for="tool-status">{m['wizard.status']()}</Label>
						<InfoHint text={m['tool_wizard.status_hint']()} />
					</div>
					<SelectField id="tool-status" bind:value={status} options={statuses} />
				</div>
			{/if}
		</form>

		<Dialog.Footer class="flex-wrap gap-2">
			{#if step === 0}
				<Button type="button" variant="outline" onclick={() => (open = false)}
					>{m['assistant_custom_tools.cancel']()}</Button
				>
				<Button type="submit" form={formId} disabled={!what.trim()}>{m['wizard.next']()}</Button>
			{:else if step === 1}
				<Button type="button" variant="outline" onclick={() => (step = 0)}>{m['wizard.back']()}</Button>
				<Button type="submit" form={formId} disabled={!fieldsValid || ai === 'working'}>{m['wizard.next']()}</Button>
			{:else}
				<Button type="button" variant="outline" onclick={() => (step = 1)}>{m['wizard.back']()}</Button>
				<Button type="button" variant="outline" disabled={!timeoutValid || saving} onclick={() => void save(true)}
					>{m['tool_wizard.save_test']()}</Button
				>
				<Button type="submit" form={formId} disabled={!timeoutValid || saving}>{m['wizard.create']()}</Button>
			{/if}
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
