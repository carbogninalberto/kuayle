<script lang="ts">
	import * as AvatarPrimitive from '$lib/components/ui/avatar/index.js';
	import { cn } from '$lib/utils.js';

	let {
		name,
		avatarUrl,
		size = 'sm',
		title,
		onerror,
		class: className = ''
	}: {
		name: string;
		avatarUrl?: string | null;
		size?: 'xs' | 'sm' | 'md' | 'lg';
		title?: string;
		onerror?: (event: Event) => void;
		class?: string;
	} = $props();

	const sizes = {
		xs: 'size-4 text-[8px]',
		sm: 'size-5 text-[9px]',
		md: 'size-8 text-sm',
		lg: 'size-10 text-base'
	};
	const initial = $derived((name?.trim().charAt(0) || 'U').toUpperCase());
</script>

{#key avatarUrl}
	<AvatarPrimitive.Root class={cn('shrink-0 rounded-full', sizes[size], className)} {title}>
		{#if avatarUrl}
			<AvatarPrimitive.Image src={avatarUrl} alt={name} {onerror} />
		{/if}
		<AvatarPrimitive.Fallback
			class="rounded-full bg-[var(--app-accent)] font-medium text-[var(--app-accent-foreground)]"
			style="font-size: inherit"
		>
			{initial}
		</AvatarPrimitive.Fallback>
	</AvatarPrimitive.Root>
{/key}
