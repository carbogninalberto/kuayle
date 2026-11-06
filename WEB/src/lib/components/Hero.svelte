<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import ArrowRight from '@lucide/svelte/icons/arrow-right';
	import Star from '@lucide/svelte/icons/star';
	import Github from '$lib/components/GithubIcon.svelte';
	import HeroBoard from '$lib/components/home/HeroBoard.svelte';
	import { DEV_MACHINES_RELEASE_STATUS } from '$lib/config/releases';
	import { useLatestRelease } from '$lib/release.svelte';

	let { stars = null }: { stars?: number | null } = $props();

	const release = useLatestRelease();

	const facts = ['Apache 2.0', '$0 per user', 'No paid tier', 'Self-hosted', `Dev Machines · ${DEV_MACHINES_RELEASE_STATUS}`];

	const starLabel = $derived(
		stars === null ? null : new Intl.NumberFormat('en-US', { notation: 'compact', maximumFractionDigits: 1 }).format(stars)
	);
</script>

<section class="relative overflow-hidden pt-32 pb-24 sm:pt-40">
	<!-- One calm glow behind the headline -->
	<div
		class="absolute -top-48 left-1/2 -z-10 h-[460px] w-[760px] -translate-x-1/2 rounded-full bg-brand-400/15 blur-[150px]"
		aria-hidden="true"
	></div>

	<div class="mx-auto max-w-6xl px-6">
		<div class="mx-auto max-w-4xl text-center">
			<a
				href={release.releaseUrl}
				target="_blank"
				rel="noopener"
				class="group inline-flex items-center gap-2 rounded-full border border-white/10 bg-white/[0.03] py-1 pr-3 pl-1.5 text-xs text-muted-foreground transition-colors hover:border-brand-400/40 hover:text-foreground"
			>
				<span class="rounded-full bg-brand-400/15 px-2 py-0.5 font-mono text-brand-200">{release.version}</span>
				Latest release, open source under Apache 2.0
				<ArrowRight class="size-3 transition-transform group-hover:translate-x-0.5" />
			</a>

			<h1 class="mx-auto mt-7 max-w-4xl text-5xl leading-[1.05] font-bold tracking-tight text-balance sm:text-6xl md:text-7xl">
				The open source issue tracker with <span class="gradient-text">no paid tier</span>
			</h1>
			<p class="mx-auto mt-6 max-w-2xl text-lg leading-relaxed text-muted-foreground sm:text-xl">
				A self-hosted, keyboard-first alternative to Linear and Jira. Plan multi-assignee issues in cycles and projects,
				keep private teams private and let GitHub pull requests move the work. Free for any team size.
			</p>

			<div class="mt-9 flex flex-wrap items-center justify-center gap-3">
				<Button href="/self-hosting" size="lg" class="h-11 bg-brand-400 px-6 text-base text-white hover:bg-brand-500">
					Self-host Kuayle
					<ArrowRight />
				</Button>
				<Button
					href="https://github.com/carbogninalberto/kuayle"
					target="_blank"
					rel="noopener"
					variant="outline"
					size="lg"
					class="h-11 px-6 text-base"
				>
					<Github />
					View source
					{#if starLabel}
						<span class="ml-1 inline-flex items-center gap-1 border-l border-white/15 pl-2.5 font-mono text-sm text-muted-foreground">
							<Star class="size-3.5" aria-hidden="true" />{starLabel}<span class="sr-only"> GitHub stars</span>
						</span>
					{/if}
				</Button>
			</div>

			<ul class="mt-7 flex flex-wrap items-center justify-center gap-x-4 gap-y-2 font-mono text-xs text-muted-foreground">
				{#each facts as fact, i}
					<li class="flex items-center gap-4">
						{#if i > 0}<span class="text-white/20" aria-hidden="true">/</span>{/if}
						{fact}
					</li>
				{/each}
			</ul>
		</div>

		<div class="relative mx-auto mt-16 max-w-5xl">
			<HeroBoard />
		</div>
		<p class="mt-14 text-center text-sm text-muted-foreground">
			Press <kbd class="key h-6! min-w-6! text-xs">C</kbd> to create an issue,
			<kbd class="key h-6! min-w-6! text-xs">⌘</kbd><kbd class="key h-6! min-w-6! text-xs">K</kbd> to search and navigate.
			Illustration with sample data.
		</p>
	</div>
</section>
