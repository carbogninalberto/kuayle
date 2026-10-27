<script lang="ts" module>
	export type TraceLineStatus = "running" | "error" | "pending" | "success" | "review" | "warning";
</script>

<script lang="ts">
	import { cn, type WithElementRef } from "$lib/utils.js";
	import type { HTMLAttributes } from "svelte/elements";

	let {
		ref = $bindable(null),
		class: className,
		status,
		children,
		...restProps
	}: WithElementRef<HTMLAttributes<HTMLSpanElement>> & {
		status?: TraceLineStatus;
	} = $props();
</script>

<span
	bind:this={ref}
	data-slot="trace-line"
	data-status={status}
	class={cn(
		"gap-2 text-sm flex w-fit max-w-full min-w-0 items-center text-muted-foreground transition-colors in-[a,button]:cursor-pointer in-[a:hover,button:hover]:text-foreground data-[status=error]:text-destructive data-[status=error]:in-[a:hover,button:hover]:text-destructive data-[status=pending]:opacity-60 data-[status=running]:*:data-[slot=trace-title]:shimmer data-[status=running]:*:data-[slot=trace-icon]:text-[var(--status-running)] data-[status=review]:*:data-[slot=trace-icon]:text-[var(--status-warning)] data-[status=success]:*:data-[slot=trace-icon]:text-[var(--status-success)] data-[status=warning]:*:data-[slot=trace-icon]:text-[var(--status-warning)]",
		className
	)}
	{...restProps}
>
	{@render children?.()}
</span>
