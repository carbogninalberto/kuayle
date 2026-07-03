<script lang="ts">
	import type { HTMLInputAttributes } from "svelte/elements";
	import { cn, type WithElementRef } from "$lib/utils.js";
	import Eye from "@lucide/svelte/icons/eye";
	import EyeOff from "@lucide/svelte/icons/eye-off";

	type Props = WithElementRef<Omit<HTMLInputAttributes, "type">> & {
		class?: string;
	};

	let {
		ref = $bindable(null),
		value = $bindable(),
		class: className,
		"data-slot": dataSlot = "password",
		disabled,
		...restProps
	}: Props = $props();

	let showPassword = $state(false);

	function togglePassword() {
		showPassword = !showPassword;
	}

	let inputType = $derived(showPassword ? "text" : "password");
</script>

<div class="relative" data-slot={dataSlot}>
	<input
		bind:this={ref}
		data-slot="input"
		class={cn(
			"dark:bg-input/30 border-input focus-visible:border-ring focus-visible:ring-ring/50 aria-invalid:ring-destructive/20 dark:aria-invalid:ring-destructive/40 aria-invalid:border-destructive dark:aria-invalid:border-destructive/50 disabled:bg-input/50 dark:disabled:bg-input/80 h-8 rounded-lg border bg-transparent px-2.5 py-1 text-base transition-colors focus-visible:ring-3 aria-invalid:ring-3 md:text-sm placeholder:text-muted-foreground w-full min-w-0 outline-none disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-50 pr-8",
			className
		)}
		type={inputType}
		bind:value
		{disabled}
		{...restProps}
	/>
	<button
		type="button"
		class="absolute inset-y-0 right-0 flex items-center pr-2 text-muted-foreground hover:text-foreground"
		onclick={togglePassword}
		tabindex={-1}
		aria-label={showPassword ? "Hide password" : "Show password"}
		{disabled}
	>
		{#if showPassword}
			<EyeOff class="size-4" />
		{:else}
			<Eye class="size-4" />
		{/if}
	</button>
</div>
