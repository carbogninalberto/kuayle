<script lang="ts">
	import { reveal } from '$lib/actions/reveal';
	import { Button } from '$lib/components/ui/button';
	import Copy from '@lucide/svelte/icons/copy';
	import Check from '@lucide/svelte/icons/check';
	import Github from '$lib/components/GithubIcon.svelte';

	const commands = `git clone https://github.com/carbogninalberto/kuayle
cd kuayle/selfhosting
cp .env.example .env
# edit .env (set DOMAIN, JWT_SECRET, POSTGRES_PASSWORD)
docker compose up --build -d
docker compose exec backend /app/server migrate up
docker compose exec backend /app/server seed`;

	let copied = $state(false);

	async function copy() {
		await navigator.clipboard.writeText(commands);
		copied = true;
		setTimeout(() => (copied = false), 2000);
	}
</script>

<section id="deploy" class="relative overflow-hidden py-24 sm:py-32">
	<div class="hero-grid absolute inset-0 -z-10 rotate-180"></div>
	<div
		class="animate-glow absolute -bottom-40 left-1/2 -z-10 h-[420px] w-[720px] -translate-x-1/2 rounded-full bg-brand-400/20 blur-[140px]"
	></div>

	<div class="mx-auto max-w-3xl px-6 text-center">
		<div use:reveal>
			<p class="text-sm font-semibold tracking-widest text-brand-300 uppercase">Self-host</p>
			<h2 class="mt-3 text-3xl font-bold tracking-tight sm:text-4xl">Deploy with Docker Compose</h2>
			<p class="mt-4 text-lg text-muted-foreground">
				The self-hosting stack runs Caddy, PostgreSQL 17, Redis 7, the Go API and the SvelteKit app.
				Set the required secrets, start the services, then run the database migration and seed commands.
			</p>
		</div>

		<div use:reveal={{ delay: 150 }} class="mt-10 text-left">
			<div
				class="overflow-hidden rounded-2xl border border-white/10 bg-black/60 shadow-2xl shadow-black/50 ring-1 ring-brand-400/20"
			>
				<div class="flex items-center justify-between border-b border-white/5 px-4 py-2.5">
					<div class="flex items-center gap-1.5">
						<span class="size-3 rounded-full bg-white/10"></span>
						<span class="size-3 rounded-full bg-white/10"></span>
						<span class="size-3 rounded-full bg-white/10"></span>
					</div>
					<button
						onclick={copy}
						class="flex items-center gap-1.5 rounded-md px-2 py-1 text-xs text-muted-foreground transition-colors hover:bg-white/5 hover:text-foreground"
					>
						{#if copied}
							<Check class="size-3.5 text-green-400" /> Copied
						{:else}
							<Copy class="size-3.5" /> Copy
						{/if}
					</button>
				</div>
				<pre class="overflow-x-auto p-5 font-mono text-sm leading-7"><code
						><span class="text-muted-foreground">$</span> <span class="text-brand-200">git</span> clone https://github.com/carbogninalberto/kuayle
<span class="text-muted-foreground">$</span> <span class="text-brand-200">cd</span> kuayle/selfhosting
<span class="text-muted-foreground">$</span> <span class="text-brand-200">cp</span> .env.example .env
<span class="text-muted-foreground">#</span> edit .env (set DOMAIN, JWT_SECRET, POSTGRES_PASSWORD)
<span class="text-muted-foreground">$</span> <span class="text-brand-200">docker</span> compose up --build -d
<span class="text-muted-foreground">$</span> <span class="text-brand-200">docker</span> compose exec backend /app/server migrate up
<span class="text-muted-foreground">$</span> <span class="text-brand-200">docker</span> compose exec backend /app/server seed<span
							class="ml-1 inline-block h-4 w-2 translate-y-0.5 bg-brand-300"
							style="animation: caret-blink 1.2s step-end infinite"
						></span></code
					></pre>
			</div>
		</div>

		<div use:reveal={{ delay: 250 }} class="mt-10 flex flex-wrap items-center justify-center gap-3">
			<Button
				href="https://github.com/carbogninalberto/kuayle"
				target="_blank"
				rel="noopener"
				size="lg"
				class="h-11 bg-brand-400 px-6 text-base text-white shadow-lg shadow-brand-400/30 hover:bg-brand-500"
			>
				<Github />
				View on GitHub
			</Button>
			<Button
				href="/self-hosting"
				variant="outline"
				size="lg"
				class="h-11 px-6 text-base"
			>
				Deployment requirements
			</Button>
		</div>
	</div>
</section>
