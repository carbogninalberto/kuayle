<script lang="ts">
	import { agentInfo } from './agents';
	// A square agent logo in its brand colour; near-black marks use the theme's
	// foreground so they stay visible in dark themes. Others get a monogram tile.
	let {
		agent,
		size = 'md',
		class: className = ''
	}: { agent: string; size?: 'sm' | 'md' | 'lg'; class?: string } = $props();
	const info = $derived(agentInfo(agent));
	const box = $derived({ sm: 'size-5 rounded', md: 'size-7 rounded-md', lg: 'size-10 rounded-lg' }[size]);
	const glyph = $derived({ sm: 'size-3', md: 'size-4', lg: 'size-6' }[size]);
	const colour = $derived.by(() => {
		const hex = info.icon?.hex;
		if (!hex) return undefined;
		const [r, g, b] = [0, 2, 4].map((i) => parseInt(hex.slice(i, i + 2), 16));
		return 0.2126 * r + 0.7152 * g + 0.0722 * b < 60 ? undefined : `#${hex}`;
	});
</script>

<span
	aria-hidden="true"
	class="inline-flex shrink-0 items-center justify-center border bg-background text-foreground {box} {className}"
>
	{#if info.icon}
		<svg viewBox="0 0 24 24" class={glyph} fill={colour ?? 'currentColor'}><path d={info.icon.path} /></svg>
	{:else}
		<span class="font-mono text-[0.6rem] leading-none font-semibold tracking-tight {size === 'lg' ? 'text-xs' : ''}"
			>{info.monogram}</span
		>
	{/if}
</span>
