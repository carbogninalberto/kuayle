<script lang="ts" generics="T extends string | number">
	import * as Select from '$lib/components/ui/select/index.js';
	import type { ComponentProps } from 'svelte';

	// Keep domain values (including numeric priorities) at the boundary while
	// Bits UI owns keyboard navigation, focus return and form serialization.
	let {
		value = $bindable(),
		options,
		placeholder,
		name,
		required,
		disabled,
		...triggerProps
	}: Omit<ComponentProps<typeof Select.Trigger>, 'value' | 'children' | 'child' | 'name'> & {
		value?: T;
		options: { value: T; label: string; disabled?: boolean }[];
		placeholder?: string;
		name?: string;
		required?: boolean;
	} = $props();

	const items = $derived(options.map((option) => ({ ...option, value: String(option.value) })));
</script>

<Select.Root
	type="single"
	value={value === undefined ? '' : String(value)}
	onValueChange={(next) => {
		const option = options.find((option) => String(option.value) === next);
		if (option) value = option.value;
	}}
	{items}
	{name}
	{required}
	disabled={disabled ?? false}
>
	<Select.Trigger {...triggerProps}>
		<Select.Value {placeholder} />
	</Select.Trigger>
	<Select.Content>
		{#each items as item (item.value)}
			<Select.Item value={item.value} label={item.label} disabled={item.disabled}>
				{item.label}
			</Select.Item>
		{/each}
	</Select.Content>
</Select.Root>
