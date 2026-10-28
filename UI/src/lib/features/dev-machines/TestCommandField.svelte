<script lang="ts">
	import { Input } from '$lib/components/ui/input/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import { m } from '$lib/paraglide/messages.js';
	import { parseTestCommand } from './test-command';
	let { value = $bindable(''), disabled = false }: { value?: string; disabled?: boolean } = $props();
	const id = $props.id();
	const invalid = $derived.by(() => {
		try {
			parseTestCommand(value);
			return false;
		} catch {
			return true;
		}
	});
</script>

<div class="min-w-0 space-y-1.5">
	<Label for={id}>{m['tasks.test_command']()}</Label>
	<Input {id} bind:value {disabled} aria-invalid={invalid} aria-describedby={`${id}-help${invalid ? ` ${id}-error` : ''}`} placeholder={'["npm","test"]'} class="font-mono text-sm" />
	<p id={`${id}-help`} class="text-xs text-muted-foreground">{m['tasks.test_command_help']()}</p>
	{#if invalid}<p id={`${id}-error`} role="alert" class="text-sm text-destructive">{m['tasks.test_command_invalid']()}</p>{/if}
</div>
