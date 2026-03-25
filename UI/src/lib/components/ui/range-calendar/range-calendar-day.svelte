<script lang="ts">
	import { cn } from "$lib/utils.js";
	import { RangeCalendar as RangeCalendarPrimitive } from "bits-ui";

	let {
		ref = $bindable(null),
		class: className,
		...restProps
	}: RangeCalendarPrimitive.DayProps = $props();
</script>

<RangeCalendarPrimitive.Day
	bind:ref
	class={cn(
		"flex size-(--cell-size) flex-col items-center justify-center gap-1 rounded-(--cell-radius) p-0 leading-none font-normal whitespace-nowrap select-none",
		"[&:last-child[data-selected=true]_button]:rounded-r-(--cell-radius)",
		"not-data-selected:hover:bg-accent/50 not-data-selected:hover:text-accent-foreground",
		"[&[data-today]:not([data-selected])]:bg-accent [&[data-today]:not([data-selected])]:text-accent-foreground [&[data-today][data-disabled]]:text-muted-foreground",
		// Range-specific: selection start/end get primary, in-range gets accent
		"data-[selection-start]:bg-primary data-[selection-start]:text-primary-foreground",
		"data-[selection-end]:bg-primary data-[selection-end]:text-primary-foreground",
		"data-[selected]:bg-accent/20 data-[selected]:text-accent-foreground",
		"data-[selected]:hover:text-foreground",
		// Range-specific: highlighted (hover range) and rounding
		"data-[highlighted]:bg-accent/20",
		"data-[selection-start]:rounded-l-(--cell-radius)",
		"data-[selection-end]:rounded-r-(--cell-radius)",
		// Outside months
		"[&[data-outside-month]:not([data-selected])]:text-muted-foreground [&[data-outside-month]:not([data-selected])]:hover:text-accent-foreground",
		// Disabled
		"data-[disabled]:text-muted-foreground data-[disabled]:pointer-events-none data-[disabled]:opacity-50",
		// Unavailable
		"data-[unavailable]:text-muted-foreground data-[unavailable]:line-through",
		// focus
		"focus:border-ring focus:ring-ring/50 focus:relative",
		// inner spans
		"[&>span]:text-xs [&>span]:opacity-70",
		className
	)}
	{...restProps}
/>
