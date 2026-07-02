<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import Github from '$lib/components/GithubIcon.svelte';

	let scrolled = $state(false);

	function onScroll() {
		scrolled = window.scrollY > 12;
	}
</script>

<svelte:window onscroll={onScroll} />

<header
	class="fixed inset-x-0 top-0 z-50 transition-all duration-300 {scrolled
		? 'border-b border-border bg-background/70 backdrop-blur-xl'
		: 'border-b border-transparent bg-transparent'}"
>
	<nav class="mx-auto flex h-16 max-w-6xl items-center justify-between px-6">
		<a href="/" class="flex items-center gap-2" aria-label="Kuayle home">
			<img src="/logo_white.svg" alt="Kuayle" class="h-7 w-auto" />
		</a>

		<div class="hidden items-center gap-8 text-sm text-muted-foreground md:flex">
			<a href="#open-source" class="transition-colors hover:text-foreground">Open source</a>
			<a href="#features" class="transition-colors hover:text-foreground">Features</a>
			<a href="#pricing" class="transition-colors hover:text-foreground">Pricing</a>
			<a href="#deploy" class="transition-colors hover:text-foreground">Self-host</a>
		</div>

		<div class="flex items-center gap-2">
			<Button
				variant="ghost"
				href="https://github.com/carbogninalberto/kuayle"
				target="_blank"
				rel="noopener"
			>
				<Github />
				<span class="hidden sm:inline">GitHub</span>
			</Button>
			<Button href="#deploy" class="bg-brand-400 text-white hover:bg-brand-500">Self-host</Button>
		</div>
	</nav>
</header>
