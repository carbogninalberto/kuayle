<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import Github from '$lib/components/GithubIcon.svelte';
	import Menu from '@lucide/svelte/icons/menu';
	import X from '@lucide/svelte/icons/x';
	import ArrowRight from '@lucide/svelte/icons/arrow-right';

	let scrolled = $state(false);
	let menuOpen = $state(false);

	function onScroll() {
		scrolled = window.scrollY > 12;
	}

	function closeMenu() {
		menuOpen = false;
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

		<!-- Desktop nav links -->
		<div class="hidden items-center gap-8 text-sm text-muted-foreground md:flex">
			<a href="#open-source" class="transition-colors hover:text-foreground">Open source</a>
			<a href="#features" class="transition-colors hover:text-foreground">Features</a>
			<a href="#pricing" class="transition-colors hover:text-foreground">Pricing</a>
			<a href="#deploy" class="transition-colors hover:text-foreground">Self-host</a>
			<a href="https://demo.kuayle.com" target="_blank" rel="noopener" class="transition-colors hover:text-foreground">Demo</a>
		</div>

		<!-- Desktop right actions -->
		<div class="hidden items-center gap-2 md:flex">
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

		<!-- Mobile hamburger toggle -->
		<button
			class="inline-flex items-center justify-center rounded-md p-2 text-muted-foreground transition-colors hover:text-foreground md:hidden"
			aria-label={menuOpen ? 'Close menu' : 'Open menu'}
			onclick={() => (menuOpen = !menuOpen)}
		>
			{#if menuOpen}
				<X class="size-5" />
			{:else}
				<Menu class="size-5" />
			{/if}
		</button>
	</nav>

	<!-- Mobile menu -->
	{#if menuOpen}
		<div class="border-b border-border bg-background/95 backdrop-blur-xl md:hidden">
			<div class="mx-auto max-w-6xl space-y-1 px-6 pb-4 pt-2">
				<a href="#open-source" class="block rounded-md px-3 py-2 text-sm text-muted-foreground transition-colors hover:bg-muted hover:text-foreground" onclick={closeMenu}>Open source</a>
				<a href="#features" class="block rounded-md px-3 py-2 text-sm text-muted-foreground transition-colors hover:bg-muted hover:text-foreground" onclick={closeMenu}>Features</a>
				<a href="#pricing" class="block rounded-md px-3 py-2 text-sm text-muted-foreground transition-colors hover:bg-muted hover:text-foreground" onclick={closeMenu}>Pricing</a>
				<a href="#deploy" class="block rounded-md px-3 py-2 text-sm text-muted-foreground transition-colors hover:bg-muted hover:text-foreground" onclick={closeMenu}>Self-host</a>
				<a href="https://demo.kuayle.com" target="_blank" rel="noopener" class="flex items-center gap-1 rounded-md px-3 py-2 text-sm text-muted-foreground transition-colors hover:bg-muted hover:text-foreground" onclick={closeMenu}>
					Demo
					<ArrowRight class="size-3" />
				</a>
				<a href="https://github.com/carbogninalberto/kuayle" target="_blank" rel="noopener" class="flex items-center gap-1.5 rounded-md px-3 py-2 text-sm text-muted-foreground transition-colors hover:bg-muted hover:text-foreground" onclick={closeMenu}>
					<Github />
					GitHub
				</a>
				<Button href="#deploy" class="mt-2 w-full bg-brand-400 text-white hover:bg-brand-500" onclick={closeMenu}>Self-host</Button>
			</div>
		</div>
	{/if}
</header>
