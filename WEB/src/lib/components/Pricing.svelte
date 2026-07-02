<script lang="ts">
	import { reveal } from '$lib/actions/reveal';
	import { Button } from '$lib/components/ui/button';
	import Check from '@lucide/svelte/icons/check';

	const included = [
		'Unlimited users, teams and workspaces',
		'Unlimited issues, projects and cycles',
		'GitHub integration',
		'Real-time sync',
		'Saved views, triage and analytics',
		'Public share links',
		'Role-based access control',
		'Every future feature'
	];
</script>

<section id="pricing" class="relative py-24 sm:py-32">
	<div class="mx-auto max-w-6xl px-6">
		<div class="grid items-start gap-14 lg:grid-cols-2">
			<div use:reveal>
				<p class="text-sm font-semibold tracking-widest text-brand-300 uppercase">Pricing</p>
				<h2 class="mt-3 text-3xl font-bold tracking-tight sm:text-4xl">
					There is no pricing page
				</h2>
				<p class="mt-5 text-lg leading-relaxed text-muted-foreground">
					Kuayle has no per-seat fees, no usage limits and no sales team, because you host it
					yourself. Your only cost is the server it runs on.
				</p>
				<p class="mt-4 text-lg leading-relaxed text-muted-foreground">
					Development is funded by sponsorships. If Kuayle saves your team money, sponsoring the
					project is welcome, but it never unlocks anything, because nothing is locked.
				</p>
			</div>

			<div use:reveal={{ delay: 100 }} class="relative">
				<div
					class="animate-glow absolute -inset-4 -z-10 rounded-3xl bg-gradient-to-tr from-brand-600/30 via-brand-400/20 to-transparent blur-2xl"
				></div>
				<div
					class="rounded-2xl border border-brand-400/30 bg-card p-8 shadow-2xl shadow-black/40"
				>
					<div class="flex items-baseline justify-between">
						<h3 class="text-xl font-semibold">Self-hosted</h3>
						<div class="text-right">
							<span class="text-5xl font-bold tracking-tight">$0</span>
							<span class="text-sm text-muted-foreground">/user/month</span>
						</div>
					</div>

					<ul class="mt-8 space-y-3">
						{#each included as item (item)}
							<li class="flex items-start gap-3 text-sm">
								<Check class="mt-0.5 size-4 shrink-0 text-brand-300" />
								<span>{item}</span>
							</li>
						{/each}
					</ul>

					<Button
						href="#deploy"
						size="lg"
						class="mt-8 h-11 w-full bg-brand-400 text-base text-white shadow-lg shadow-brand-400/30 hover:bg-brand-500"
					>
						Deploy it
					</Button>
				</div>
			</div>
		</div>
	</div>
</section>
