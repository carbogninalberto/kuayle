<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import SelectField from '$lib/components/shared/SelectField.svelte';
	import { Switch } from '$lib/components/ui/switch';
	import type { InstanceConfigVariable } from '$lib/api/instance-config';
	import { m } from '$lib/features/i18n/messages';

	// The editor of a variable the API reads at startup: it only drafts a value.
	// Nothing is stored until the sysadmin applies the drafts from the panel
	// above the list, which checks them first. `error` is the dry run's message.
	let {
		variable,
		value,
		changed,
		reverting,
		error,
		disabled,
		onedit,
		onrevert,
		ondiscard
	}: {
		variable: InstanceConfigVariable;
		/** The value shown: the draft, or what is in effect. */
		value: string;
		changed: boolean;
		reverting: boolean;
		error?: string;
		disabled: boolean;
		onedit: (value: string) => void;
		onrevert: () => void;
		ondiscard: () => void;
	} = $props();

	const label = $derived(m['settings.instance_config.value_label']({ name: variable.name }));
	const hintId = $derived(`instance-config-hint-${variable.name}`);
	const errorId = $derived(`instance-config-error-${variable.name}`);
	const choices = $derived(variable.rule?.choices ?? []);
	const range = $derived(variable.rule?.min && variable.rule?.max ? { min: variable.rule.min, max: variable.rule.max } : null);
	const describedBy = $derived([range ? hintId : '', error ? errorId : ''].filter(Boolean).join(' ') || undefined);
</script>

<div class="grid gap-1.5">
	<div class="flex flex-wrap items-center gap-x-3 gap-y-2">
		{#if variable.kind === 'bool'}
			<Switch
				bind:checked={() => value === 'true', (checked) => onedit(String(checked))}
				{disabled}
				aria-label={label}
				aria-invalid={error ? true : undefined}
				aria-describedby={describedBy}
			/>
			<span class="font-mono text-sm">{value}</span>
		{:else if choices.length > 0}
			<SelectField
				bind:value={() => value, (next) => onedit(next)}
				options={choices.map((choice) => ({ value: choice, label: choice }))}
				{disabled}
				aria-label={label}
				aria-invalid={error ? true : undefined}
				aria-describedby={describedBy}
			/>
		{:else}
			<Input
				class="h-8 w-72 max-w-full font-mono"
				{value}
				oninput={(event) => onedit(event.currentTarget.value)}
				{disabled}
				aria-label={label}
				aria-invalid={error ? true : undefined}
				aria-describedby={describedBy}
				inputmode={variable.kind === 'int' ? 'numeric' : undefined}
				autocomplete="off"
				spellcheck={false}
			/>
		{/if}
		{#if changed}
			<Button variant="outline" size="sm" {disabled} onclick={ondiscard}>
				{m['settings.instance_config.restart_discard']()}
			</Button>
		{:else if variable.source === 'override' && !reverting}
			<Button variant="outline" size="sm" {disabled} onclick={onrevert}>
				{m['settings.instance_config.restart_revert']()}
			</Button>
		{/if}
	</div>
	{#if range}
		<span id={hintId} class="text-xs text-muted-foreground">
			{m['settings.instance_config.range']({ min: range.min, max: range.max })}
		</span>
	{/if}
	{#if error}
		<p id={errorId} class="text-xs text-destructive">{error}</p>
	{/if}
</div>
