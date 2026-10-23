<script lang="ts" generics="T extends string | number">
	import * as Select from '$lib/components/ui/select/index.js';
	import type { ComponentProps } from 'svelte';
	import { cn } from '$lib/utils.js';

	// Keep domain values (including numeric priorities) at the boundary while
	// Bits UI owns keyboard navigation, focus return and form serialization.
	let {
		value = $bindable(),
		options,
		placeholder,
		name,
		required,
		disabled,
		class: className,
		...triggerProps
	}: Omit<ComponentProps<typeof Select.Trigger>, 'value' | 'children' | 'child' | 'name'> & {
		value?: T;
		options: { value: T; label: string; disabled?: boolean }[];
		placeholder?: string;
		name?: string;
		required?: boolean;
	} = $props();

	// An empty-value option is the native select's prompt: show it as the placeholder.
	const prompt = $derived(options.find((option) => String(option.value) === '')?.label);
	const items = $derived(
		options
			.filter((option) => String(option.value) !== '')
			.map((option) => ({ ...option, value: String(option.value) }))
	);
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
	<!-- A caller class carries the crafted look of the native select this replaced;
	     the inner offsets mirror the native control's text inset and chevron position. -->
	<Select.Trigger
		{...triggerProps}
		variant={className ? 'plain' : 'default'}
		class={className
			? cn(
					'*:data-[slot=select-value]:pl-1 *:data-[slot=select-value]:leading-[19px] [&>svg:last-child]:-mr-[7px] [&>svg:last-child]:size-3.5 [&>svg:last-child]:stroke-[3] [&>svg:last-child]:text-current',
					className
				)
			: undefined}
	>
		<Select.Value placeholder={placeholder ?? prompt} />
	</Select.Trigger>
	<Select.Content>
		{#each items as item (item.value)}
			<Select.Item value={item.value} label={item.label} disabled={item.disabled}>
				{item.label}
			</Select.Item>
		{/each}
	</Select.Content>
</Select.Root>
