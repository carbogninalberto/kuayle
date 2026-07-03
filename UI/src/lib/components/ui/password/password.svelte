<script lang="ts">
	import type { HTMLInputAttributes } from "svelte/elements";
	import { cn, type WithElementRef } from "$lib/utils.js";
	import { Input } from "$lib/components/ui/input";
	import Eye from "@lucide/svelte/icons/eye";
	import EyeOff from "@lucide/svelte/icons/eye-off";

	type Props = WithElementRef<Omit<HTMLInputAttributes, "type" | "files">> & {
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
	<Input
		bind:ref
		bind:value
		type={inputType}
		class={cn(className, "pr-8")}
		{disabled}
		{...restProps}
	/>
	<button
		type="button"
		class="absolute inset-y-0 right-0 grid w-8 place-items-center border-0 bg-transparent p-0 text-muted-foreground hover:text-foreground disabled:pointer-events-none disabled:opacity-50"
		onclick={togglePassword}
		tabindex={-1}
		aria-label={showPassword ? "Hide password" : "Show password"}
		{disabled}
	>
		{#if showPassword}
			<EyeOff class="size-4 translate-y-px" />
		{:else}
			<Eye class="size-4 translate-y-px" />
		{/if}
	</button>
</div>
