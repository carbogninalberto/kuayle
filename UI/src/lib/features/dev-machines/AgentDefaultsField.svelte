<script lang="ts">
	import { Label } from '$lib/components/ui/label/index.js';
	import { Textarea } from '$lib/components/ui/textarea/index.js';
	import SelectField from '$lib/components/shared/SelectField.svelte';
	import { m } from '$lib/paraglide/messages.js';
	let {
		provider = $bindable('inherit'),
		instructions = $bindable(''),
		disabled = false
	}: { provider?: string; instructions?: string; disabled?: boolean } = $props();
	const id = $props.id();
	// Built-in autonomous harnesses; the workspace policy still decides which may run.
	const providers = [
		{ value: 'inherit', label: m['tasks.agent_provider_inherit']() },
		{ value: 'codex', label: 'Codex' },
		{ value: 'claude-code', label: 'Claude Code' },
		{ value: 'opencode', label: 'OpenCode' },
		{ value: 'qwen-code', label: 'Qwen Code' },
		{ value: 'crush', label: 'Crush' },
		{ value: 'kimi', label: 'Kimi' },
		{ value: 'goose', label: 'Goose' }
	];
	const tooLong = $derived([...instructions].length > 8000);
</script>

<div class="grid min-w-0 gap-3 sm:col-span-2">
	<div class="min-w-0 space-y-1.5">
		<Label for={`${id}-provider`}>{m['tasks.agent_provider']()}</Label>
		<SelectField
			id={`${id}-provider`}
			bind:value={provider}
			options={providers}
			{disabled}
			aria-label={m['tasks.agent_provider']()}
		/>
	</div>
	<div class="min-w-0 space-y-1.5">
		<Label for={`${id}-instructions`}>{m['tasks.agent_instructions']()}</Label>
		<Textarea
			id={`${id}-instructions`}
			bind:value={instructions}
			{disabled}
			rows={4}
			aria-invalid={tooLong}
			aria-describedby={`${id}-help`}
			placeholder={m['tasks.agent_instructions_placeholder']()}
		/>
		<p id={`${id}-help`} class="text-xs text-muted-foreground">{m['tasks.agent_instructions_help']()}</p>
		{#if tooLong}<p role="alert" class="text-sm text-destructive">{m['tasks.agent_instructions_too_long']()}</p>{/if}
	</div>
</div>
