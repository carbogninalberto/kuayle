<script lang="ts">
	import { Plus, Trash2 } from '@lucide/svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Checkbox } from '$lib/components/ui/checkbox/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import { Textarea } from '$lib/components/ui/textarea/index.js';
	import SelectField from '$lib/components/shared/SelectField.svelte';
	import { m } from '$lib/paraglide/messages.js';
	import { blankParam, maxParameters, type ParamDraft, type ParamType } from './tool-schema';

	// Rows of a custom tool's parameters; the editor turns them into the schema.
	let { params = $bindable([]), idPrefix = 'tool-param' }: { params?: ParamDraft[]; idPrefix?: string } = $props();

	const types: { value: ParamType; label: string }[] = [
		{ value: 'string', label: m['assistant_custom_tools.type_string']() },
		{ value: 'number', label: m['assistant_custom_tools.type_number']() },
		{ value: 'integer', label: m['assistant_custom_tools.type_integer']() },
		{ value: 'boolean', label: m['assistant_custom_tools.type_boolean']() },
		{ value: 'enum', label: m['assistant_custom_tools.type_enum']() }
	];

	function add() {
		params = [...params, blankParam()];
	}
	function remove(index: number) {
		params = params.filter((_, i) => i !== index);
	}
</script>

<div class="grid gap-3">
	{#if params.length === 0}
		<p class="text-xs text-muted-foreground">{m['assistant_custom_tools.no_parameters']()}</p>
	{/if}
	{#each params as param, index (index)}
		{@const id = `${idPrefix}-${index}`}
		<fieldset class="grid min-w-0 gap-3 rounded-lg border p-3">
			<legend class="sr-only">{param.name || m['assistant_custom_tools.parameter_number']({ n: String(index + 1) })}</legend>
			<div class="grid gap-3 sm:grid-cols-[minmax(0,1fr)_10rem]">
				<div class="grid gap-1.5">
					<Label for="{id}-name">{m['assistant_custom_tools.param_name']()}</Label>
					<Input
						id="{id}-name"
						bind:value={param.name}
						maxlength={40}
						autocomplete="off"
						spellcheck={false}
						class="font-mono"
					/>
				</div>
				<div class="grid gap-1.5">
					<Label for="{id}-type">{m['assistant_custom_tools.param_type']()}</Label>
					<SelectField
						id="{id}-type"
						bind:value={param.type}
						options={types}
						aria-label={m['assistant_custom_tools.param_type']()}
					/>
				</div>
			</div>
			<div class="grid gap-1.5">
				<Label for="{id}-description">{m['assistant_custom_tools.param_description']()}</Label>
				<Input id="{id}-description" bind:value={param.description} maxlength={500} autocomplete="off" />
			</div>
			{#if param.type === 'string'}
				<div class="grid grid-cols-2 gap-3">
					<div class="grid gap-1.5">
						<Label for="{id}-min-length">{m['assistant_custom_tools.min_length']()}</Label>
						<Input id="{id}-min-length" bind:value={param.minLength} inputmode="numeric" autocomplete="off" />
					</div>
					<div class="grid gap-1.5">
						<Label for="{id}-max-length">{m['assistant_custom_tools.max_length']()}</Label>
						<Input
							id="{id}-max-length"
							bind:value={param.maxLength}
							inputmode="numeric"
							autocomplete="off"
							placeholder="1024"
						/>
					</div>
				</div>
			{:else if param.type === 'number' || param.type === 'integer'}
				<div class="grid grid-cols-2 gap-3">
					<div class="grid gap-1.5">
						<Label for="{id}-minimum">{m['assistant_custom_tools.minimum']()}</Label>
						<Input id="{id}-minimum" bind:value={param.minimum} inputmode="decimal" autocomplete="off" />
					</div>
					<div class="grid gap-1.5">
						<Label for="{id}-maximum">{m['assistant_custom_tools.maximum']()}</Label>
						<Input id="{id}-maximum" bind:value={param.maximum} inputmode="decimal" autocomplete="off" />
					</div>
				</div>
			{:else if param.type === 'enum'}
				<div class="grid gap-1.5">
					<Label for="{id}-enum">{m['assistant_custom_tools.enum_values']()}</Label>
					<Textarea id="{id}-enum" bind:value={param.enumValues} rows={3} class="font-mono text-xs" />
				</div>
			{/if}
			<div class="flex flex-wrap items-center justify-between gap-2">
				<div class="flex items-center gap-2">
					<Checkbox id="{id}-required" bind:checked={param.required} />
					<Label for="{id}-required" class="font-normal">{m['assistant_custom_tools.param_required']()}</Label>
				</div>
				<Button
					type="button"
					variant="ghost"
					size="sm"
					class="text-destructive"
					aria-label={m['assistant_custom_tools.remove_parameter']({ name: param.name || String(index + 1) })}
					onclick={() => remove(index)}><Trash2 />{m['assistant_custom_tools.remove']()}</Button
				>
			</div>
		</fieldset>
	{/each}
	<div>
		<Button type="button" variant="outline" size="sm" disabled={params.length >= maxParameters} onclick={add}
			><Plus />{m['assistant_custom_tools.add_parameter']()}</Button
		>
	</div>
</div>
