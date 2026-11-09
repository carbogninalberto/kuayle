<script lang="ts">
	import { Check } from '@lucide/svelte';
	import { cn } from '$lib/utils.js';

	// The progress of a multi-step dialog: numbered steps, done ones checked,
	// the current one marked for assistive technology with aria-current.
	let { steps, current, label }: { steps: string[]; current: number; label: string } = $props();
</script>

<ol aria-label={label} class="flex items-center gap-2 text-xs">
	{#each steps as step, index (index)}
		{#if index > 0}<li
				aria-hidden="true"
				class={cn('h-px min-w-3 flex-1', index <= current ? 'bg-primary' : 'bg-border')}
			></li>{/if}
		<li
			aria-current={index === current ? 'step' : undefined}
			class={cn(
				'flex shrink-0 items-center gap-1.5',
				index === current ? 'font-medium text-foreground' : 'text-muted-foreground'
			)}
		>
			<span
				class={cn(
					'flex size-5 items-center justify-center rounded-full border text-[11px] tabular-nums',
					index < current && 'border-primary bg-primary text-primary-foreground',
					index === current && 'border-primary text-primary'
				)}
				>{#if index < current}<Check class="size-3" />{:else}{index + 1}{/if}</span
			>
			<span class={cn(index !== current && 'max-sm:sr-only')}>{step}</span>
		</li>
	{/each}
</ol>
