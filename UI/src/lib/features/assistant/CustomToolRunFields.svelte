<script lang="ts">
	import { Checkbox } from '$lib/components/ui/checkbox/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import SelectField from '$lib/components/shared/SelectField.svelte';
	import type { AssistantToolRunMode } from '$lib/api/assistant-tools';
	import { m } from '$lib/features/i18n/messages';

	// How a custom tool runs: approval or automatic, its timeout and the
	// read-only mark. Shared by the editor and the creation wizard.
	let {
		runMode = $bindable<AssistantToolRunMode>('approval'),
		readOnly = $bindable(false),
		timeout = $bindable('600')
	}: { runMode?: AssistantToolRunMode; readOnly?: boolean; timeout?: string } = $props();

	// Auto runs are allowed only for read-only tools; the database enforces it too.
	$effect(() => {
		if (!readOnly && runMode === 'auto') runMode = 'approval';
	});

	const timeoutSeconds = $derived(Number(timeout));
	const timeoutValid = $derived(Number.isInteger(timeoutSeconds) && timeoutSeconds >= 10 && timeoutSeconds <= 1800);
	const modes = $derived([
		{ value: 'approval' as const, label: m['assistant_custom_tools.mode_approval']() },
		{ value: 'auto' as const, label: m['assistant_custom_tools.mode_auto'](), disabled: !readOnly }
	]);
</script>

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
