<script lang="ts">
	import * as AvatarPrimitive from '$lib/components/ui/avatar/index.js';
	import { cn } from '$lib/utils.js';

	let {
		name,
		logoUrl,
		size = 'md',
		title,
		class: className = ''
	}: {
		name: string;
		logoUrl?: string | null;
		size?: 'xs' | 'sm' | 'md' | 'lg';
		title?: string;
		class?: string;
	} = $props();

	const sizes = {
		xs: 'size-4 text-[8px]',
		sm: 'size-5 text-[9px]',
		md: 'size-8 text-sm',
		lg: 'size-10 text-base'
	};
	const initial = $derived((name?.trim().charAt(0) || 'W').toUpperCase());
</script>

{#key logoUrl}
	<AvatarPrimitive.Root class={cn('shrink-0 rounded-md', sizes[size], className)} {title}>
		{#if logoUrl}
			<AvatarPrimitive.Image src={logoUrl} alt={name} class="rounded-md" />
		{/if}
		<AvatarPrimitive.Fallback
			class="rounded-md bg-[var(--app-accent)] font-bold text-[var(--app-accent-foreground)]"
			style="font-size: inherit"
		>
			{initial}
		</AvatarPrimitive.Fallback>
	</AvatarPrimitive.Root>
{/key}
