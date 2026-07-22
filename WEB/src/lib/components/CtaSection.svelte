<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import Github from '$lib/components/GithubIcon.svelte';
	import ArrowRight from '@lucide/svelte/icons/arrow-right';

	let {
		heading = 'Review Kuayle before you deploy it',
		description = 'Inspect the Apache 2.0 source, then follow the Docker Compose guide to run an instance on your infrastructure.'
	}: {
		heading?: string;
		description?: string;
	} = $props();
</script>

<section class="relative mt-20">
	<div
		class="animate-glow absolute -inset-3 -z-10 rounded-3xl bg-gradient-to-tr from-brand-600/25 via-brand-400/15 to-transparent blur-2xl"
	></div>
	<div class="ring-gradient noise-overlay overflow-hidden rounded-2xl border border-brand-400/30 bg-card p-8 text-center sm:p-10">
		<h2 class="text-2xl font-bold tracking-tight sm:text-3xl">{heading}</h2>
		<p class="mt-3 text-muted-foreground">{description}</p>
		<div class="mt-6 flex flex-wrap items-center justify-center gap-3">
			<Button
				href="https://github.com/carbogninalberto/kuayle"
				target="_blank"
				rel="noopener"
				size="lg"
				class="h-11 bg-brand-400 px-6 text-base text-white shadow-lg shadow-brand-400/30 hover:bg-brand-500"
			>
				<Github />
				View source on GitHub
			</Button>
			<Button
				href="/self-hosting"
				variant="outline"
				size="lg"
				class="h-11 px-6 text-base"
			>
				Deployment guide
				<ArrowRight />
			</Button>
		</div>
	</div>
</section>
