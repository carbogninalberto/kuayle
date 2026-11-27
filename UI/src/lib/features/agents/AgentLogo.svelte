<script lang="ts">
	import * as Avatar from '$lib/components/ui/avatar';
	import { agentInfo } from './agents';
	// A square agent logo in its brand colour; near-black marks use the theme's
	// foreground so they stay visible in dark themes. Image logos fall back to the
	// monogram tile if they fail to load, like agents with no logo at all.
	let {
		agent,
		size = 'md',
		class: className = ''
	}: { agent: string; size?: 'sm' | 'md' | 'lg'; class?: string } = $props();
	const info = $derived(agentInfo(agent));
	const box = $derived({ sm: 'size-5 rounded', md: 'size-7 rounded-md', lg: 'size-10 rounded-lg' }[size]);
	const glyph = $derived({ sm: 'size-3', md: 'size-4', lg: 'size-6' }[size]);
	const monogramText = $derived(
		`font-mono text-[0.6rem] leading-none font-semibold tracking-tight ${size === 'lg' ? 'text-xs' : ''}`
	);
	const colour = $derived.by(() => {
		const hex = info.icon?.hex;
		if (!hex) return undefined;
		const [r, g, b] = [0, 2, 4].map((i) => parseInt(hex.slice(i, i + 2), 16));
		return 0.2126 * r + 0.7152 * g + 0.0722 * b < 60 ? undefined : `#${hex}`;
	});
</script>

<span
	aria-hidden="true"
	class="inline-flex shrink-0 items-center justify-center overflow-hidden border bg-background text-foreground {box} {className}"
>
	{#if info.image}
		<Avatar.Root class="size-full rounded-[inherit] after:hidden">
			<Avatar.Image src={info.image} alt="" class="rounded-[inherit]" />
			<Avatar.Fallback class="rounded-[inherit] bg-transparent {monogramText}">{info.monogram}</Avatar.Fallback>
		</Avatar.Root>
	{:else if info.logo}
		<svg viewBox={info.logo.viewBox} class={glyph}>
			{#each info.logo.layers as layer (layer.path)}
				<path
					d={layer.path}
					style="--light:{layer.light};--dark:{layer.dark}"
					class="fill-(--light) dark:fill-(--dark)"
				/>
			{/each}
		</svg>
	{:else if info.icon}
		<svg viewBox="0 0 24 24" class={glyph} fill={colour ?? 'currentColor'}>
			<path d={info.icon.path} fill-rule={info.icon.evenodd ? 'evenodd' : undefined} />
		</svg>
	{:else}
		<span class={monogramText}>{info.monogram}</span>
	{/if}
</span>
