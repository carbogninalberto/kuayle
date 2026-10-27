<script lang="ts">
	import { Input } from '$lib/components/ui/input/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import { m } from '$lib/paraglide/messages.js';
	import { previewSettingValid } from './preview-setting';
	let {
		command = $bindable(''),
		port = $bindable(''),
		disabled = false
	}: { command?: string; port?: string; disabled?: boolean } = $props();
	const id = $props.id();
	const invalid = $derived(!previewSettingValid(command, port));
</script>

<div class="min-w-0 space-y-1.5 sm:col-span-2">
	<div class="grid gap-3 sm:grid-cols-[1fr_8rem]">
		<div class="space-y-1.5">
			<Label for={`${id}-command`}>{m['tasks.preview_command']()}</Label>
			<Input
				id={`${id}-command`}
				bind:value={command}
				{disabled}
				aria-invalid={invalid}
				aria-describedby={`${id}-help`}
				placeholder={'["npm","run","dev"]'}
				class="font-mono text-sm"
			/>
		</div>
		<div class="space-y-1.5">
			<Label for={`${id}-port`}>{m['tasks.preview_port']()}</Label>
			<Input
				id={`${id}-port`}
				bind:value={port}
				{disabled}
				aria-invalid={invalid}
				inputmode="numeric"
				placeholder="5173"
			/>
		</div>
	</div>
	<p id={`${id}-help`} class="text-xs text-muted-foreground">{m['tasks.preview_help']()}</p>
	{#if invalid}<p role="alert" class="text-sm text-destructive">{m['tasks.preview_invalid']()}</p>{/if}
</div>
