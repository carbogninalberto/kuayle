<script lang="ts">
	import { page } from '$app/state';

	const isHome = $derived(page.url.pathname === '/');

	const productLinks = $derived([
		{ label: 'Open source', homeHref: '#open-source', routeHref: '/open-source' },
		{ label: 'Features', homeHref: '#features', routeHref: '/features' },
		{ label: 'Pricing', homeHref: '#pricing', routeHref: '/#pricing' },
		{ label: 'Self-host', homeHref: '#deploy', routeHref: '/self-hosting' },
		{ label: 'Compare', homeHref: '/compare', routeHref: '/compare' },
		{ label: 'Alternatives', homeHref: '/alternatives', routeHref: '/alternatives' }
	]);

	function linkHref(link: { homeHref: string; routeHref: string }): string {
		return isHome ? link.homeHref : link.routeHref;
	}

	const year = new Date().getFullYear();

	function starPoints(cx: number, cy: number, outer: number, inner: number, rot: number) {
		const pts: string[] = [];
		for (let i = 0; i < 10; i++) {
			const r = i % 2 === 0 ? outer : inner;
			const angle = rot + (i * Math.PI) / 5 - Math.PI / 2;
			pts.push(`${cx + r * Math.cos(angle)},${cy + r * Math.sin(angle)}`);
		}
		return pts.join(' ');
	}
</script>

<footer class="border-t border-border bg-black/30">
	<div class="mx-auto max-w-6xl px-6 py-12">
		<div class="grid gap-10 sm:grid-cols-2 lg:grid-cols-4">
			<div class="flex flex-col gap-3 sm:col-span-2">
				<img src="/logo_white.svg" alt="Kuayle" class="h-6 w-auto self-start" width={96} height={24} />
				<p class="text-xs text-muted-foreground">
					The keyboard-driven, self-hosted issue tracker with no paid tier. Made by Bakney.
				</p>
				<address class="mt-2 text-xs leading-relaxed text-muted-foreground not-italic">
					<span class="font-medium text-foreground/80">Bakney srl</span><br />
					P. IVA: 04921790236<br />
					Sede Legale: Q.re Aldo Moro 49, 37032, Monteforte d'Alpone (VR), Italy<br />
					N. REA: VR-456410 · Registro delle Imprese di Verona<br />
					Capitale Sociale: € 1.000,00 i.v.
				</address>
			</div>

			<div class="flex flex-col gap-3 text-sm">
				<p class="text-xs font-semibold tracking-widest text-muted-foreground uppercase">Product</p>
				{#each productLinks as link}
					<a href={linkHref(link)} class="text-muted-foreground transition-colors hover:text-foreground">{link.label}</a>
				{/each}
			</div>

			<div class="flex flex-col gap-3 text-sm">
				<p class="text-xs font-semibold tracking-widest text-muted-foreground uppercase">Company</p>
				<a
					href="https://github.com/carbogninalberto/kuayle"
					target="_blank"
					rel="noopener"
					class="text-muted-foreground transition-colors hover:text-foreground">GitHub</a
				>
			<a
				href="/license"
				class="text-muted-foreground transition-colors hover:text-foreground">License</a
			>
				<a href="/privacy" class="text-muted-foreground transition-colors hover:text-foreground"
					>Privacy</a
				>
				<a href="/security" class="text-muted-foreground transition-colors hover:text-foreground">Security</a>
				<a href="/about" class="text-muted-foreground transition-colors hover:text-foreground">About</a>
				<a href="/roadmap" class="text-muted-foreground transition-colors hover:text-foreground">Roadmap</a>
			<a
				href="mailto:support@bakney.com"
				class="text-muted-foreground transition-colors hover:text-foreground"
				>support@bakney.com</a
			>
			</div>
		</div>

		<div
			class="mt-10 flex flex-col items-center justify-between gap-3 border-t border-border pt-6 sm:flex-row"
		>
			<p class="text-xs text-muted-foreground">
				© {year} Bakney srl. Kuayle is open source under the Apache 2.0 license.
			</p>
			<p
				class="flex items-center gap-1.5 text-xs text-muted-foreground"
				title="Proudly made in Europe"
			>
					Proudly made in
					<svg
						viewBox="0 0 60 40"
						class="size-3.5 rounded-[2px] ring-1 ring-white/10"
						role="img"
						aria-label="European Union flag"
					>
						<rect width="60" height="40" fill="#003399" />
						<g fill="#FFCC00">
							{#each Array(12) as _, i}
								{@const a = (i * 30 - 90) * Math.PI / 180}
								{@const x = 30 + 9 * Math.cos(a)}
								{@const y = 20 + 9 * Math.sin(a)}
								<polygon points={starPoints(x, y, 1.7, 0.75, a)} />
							{/each}
						</g>
					</svg>
					Europe
			</p>
		</div>
	</div>
</footer>
