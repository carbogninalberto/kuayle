<script lang="ts">
	import { Input } from '$lib/components/ui/input/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import { m } from '$lib/paraglide/messages.js';
	import { validComposePath } from './compose-file';
	let { value = $bindable(''), disabled = false }: { value?: string; disabled?: boolean } = $props();
	const id = $props.id();
	const invalid = $derived(!validComposePath(value));
</script>

<div class="min-w-0 space-y-1.5">
	<Label for={id}>{m['tasks.compose_file']()}</Label>
	<Input
		{id}
		bind:value
		{disabled}
		aria-invalid={invalid}
		aria-describedby={`${id}-help${invalid ? ` ${id}-error` : ''}`}
		placeholder="compose.yaml"
		class="font-mono text-sm"
	/>
	<p id={`${id}-help`} class="text-xs text-muted-foreground">{m['tasks.compose_file_help']()}</p>
	{#if invalid}<p id={`${id}-error`} role="alert" class="text-sm text-destructive">
			{m['tasks.compose_file_invalid']()}
		</p>{/if}
</div>
