<script lang="ts">
	import { Button } from '$lib/components/ui/button/index.js';
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

	// Kuayle: margins position the whole field, so they move to the wrapper; the
	// absolutely positioned toggle then spans exactly the input's border box.
	const marginClass = /^(?:[\w[\]=-]+:)*-?m[trblxyse]?-/;
	let classTokens = $derived((className ?? "").split(/\s+/).filter(Boolean));
	let wrapperClass = $derived(classTokens.filter((token) => marginClass.test(token)).join(" "));
	let inputClass = $derived(classTokens.filter((token) => !marginClass.test(token)).join(" "));
</script>

<div class={cn("relative flex", wrapperClass)} data-slot={dataSlot}>
	<Input
		bind:ref
		bind:value
		type={inputType}
		class={cn(inputClass, "pr-8")}
		{disabled}
		{...restProps}
	/>
	<!-- Kuayle: plain keeps the toggle a full-height, vertically centered overlay; it stays out of the tab order as before. -->
	<Button variant="plain"
		type="button"
		data-slot="password-toggle"
		class="absolute inset-y-0 right-0 grid w-8 place-items-center text-muted-foreground hover:text-foreground disabled:pointer-events-none disabled:opacity-50"
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
	</Button>
</div>
