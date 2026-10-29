<script lang="ts">
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import { m } from '$lib/paraglide/messages.js';
	import type { DevMachineCheckSuggestion } from '$lib/types/dev-machine';
	import { formatTestCommand, parseTestCommand } from './test-command';
	// suggest proposes a check from the repository; null means no repository is
	// chosen yet, and omitting it hides the suggestion button.
	let {
		value = $bindable(''),
		disabled = false,
		suggest
	}: {
		value?: string;
		disabled?: boolean;
		suggest?: (() => Promise<DevMachineCheckSuggestion | null>) | null;
	} = $props();
	const id = $props.id();
	const invalid = $derived.by(() => {
		try {
			parseTestCommand(value);
			return false;
		} catch {
			return true;
		}
	});
	let suggesting = $state(false);
	let note = $state('');

	async function suggestCheck() {
		if (!suggest) return;
		suggesting = true;
		note = m['tasks.test_command_suggesting']();
		try {
			const suggestion = await suggest();
			if (suggestion?.command?.length) {
				value = formatTestCommand(suggestion.command);
				note = m['tasks.test_command_suggested']({ files: suggestion.basis.join(', ') });
			} else {
				note = m['tasks.test_command_no_suggestion']();
			}
		} catch (error) {
			// 404: the scope inherits no repository yet.
			note = (error as { status?: number })?.status === 404 ? m['tasks.test_command_suggest_needs_repository']() : m['tasks.test_command_suggest_failed']();
		} finally {
			suggesting = false;
		}
	}
</script>

<div class="min-w-0 space-y-1.5">
	<div class="flex min-w-0 flex-wrap items-center justify-between gap-2">
		<Label for={id}>{m['tasks.test_command']()}</Label>
		{#if suggest !== undefined}
			<Button type="button" variant="outline" size="sm" disabled={disabled || suggesting || !suggest} onclick={suggestCheck}>{m['tasks.test_command_suggest']()}</Button>
		{/if}
	</div>
	<Input {id} bind:value {disabled} aria-invalid={invalid} aria-describedby={`${id}-help${invalid ? ` ${id}-error` : ''}`} placeholder={'["npm","test"]'} class="font-mono text-sm" />
	<p id={`${id}-help`} class="text-xs text-muted-foreground">{m['tasks.test_command_help']()}</p>
	{#if suggest === null && !disabled}<p class="text-xs text-muted-foreground">{m['tasks.test_command_suggest_needs_repository']()}</p>{/if}
	<p class="text-xs text-muted-foreground break-words" aria-live="polite">{note}</p>
	{#if invalid}<p id={`${id}-error`} role="alert" class="text-sm text-destructive">{m['tasks.test_command_invalid']()}</p>{/if}
</div>
