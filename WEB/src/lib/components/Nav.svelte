<script lang="ts">
	import { page } from '$app/state';
	import { Button } from '$lib/components/ui/button';
	import Github from '$lib/components/GithubIcon.svelte';
	import Menu from '@lucide/svelte/icons/menu';
	import X from '@lucide/svelte/icons/x';
	import ArrowRight from '@lucide/svelte/icons/arrow-right';

	let scrolled = $state(false);
	let menuOpen = $state(false);

	// On homepage, use fragment links for smooth scroll. On other pages,
	// use proper route links so navigation works correctly.
	const isHome = $derived(page.url.pathname === '/');

	const links = $derived([
		{ label: 'Open source', homeHref: '#open-source', routeHref: '/open-source' },
		{ label: 'Features', homeHref: '#features', routeHref: '/features' },
		{ label: 'Pricing', homeHref: '#pricing', routeHref: '/#pricing' },
		{ label: 'Self-host', homeHref: '#deploy', routeHref: '/self-hosting' },
	]);

	function linkHref(link: { homeHref: string; routeHref: string }): string {
		return isHome ? link.homeHref : link.routeHref;
	}

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
			<img src="/logo_white.svg" alt="Kuayle" class="h-7 w-auto" width={112} height={28} />
		</a>

		<!-- Desktop nav links -->
		<div class="hidden items-center gap-8 text-sm text-muted-foreground md:flex">
			{#each links as link}
				<a href={linkHref(link)} class="transition-colors hover:text-foreground">{link.label}</a>
			{/each}
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
			<Button href={isHome ? '#deploy' : '/self-hosting'} class="bg-brand-400 text-white hover:bg-brand-500">Self-host</Button>
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
				{#each links as link}
					<a href={linkHref(link)} class="block rounded-md px-3 py-2 text-sm text-muted-foreground transition-colors hover:bg-muted hover:text-foreground" onclick={closeMenu}>{link.label}</a>
				{/each}
				<a href="https://demo.kuayle.com" target="_blank" rel="noopener" class="flex items-center gap-1 rounded-md px-3 py-2 text-sm text-muted-foreground transition-colors hover:bg-muted hover:text-foreground" onclick={closeMenu}>
					Demo
					<ArrowRight class="size-3" />
				</a>
				<a href="https://github.com/carbogninalberto/kuayle" target="_blank" rel="noopener" class="flex items-center gap-1.5 rounded-md px-3 py-2 text-sm text-muted-foreground transition-colors hover:bg-muted hover:text-foreground" onclick={closeMenu}>
					<Github />
					GitHub
				</a>
				<Button href={isHome ? '#deploy' : '/self-hosting'} class="mt-2 w-full bg-brand-400 text-white hover:bg-brand-500" onclick={closeMenu}>Self-host</Button>
			</div>
		</div>
	{/if}
</header>
