<script lang="ts">
	import { reveal } from '$lib/actions/reveal';
	import { Button } from '$lib/components/ui/button';
	import Check from '@lucide/svelte/icons/check';

	const included = [
		'No software-enforced user or workspace limit',
		'No software-enforced issue, project or cycle limit',
		'GitHub integration with auto-transitions',
		'Real-time sync via WebSockets',
		'Saved views, triage and labels',
		'Public share links',
		'Role-based access control',
		'Apache 2.0 source code'
	];
</script>

<section id="pricing" class="relative py-24 sm:py-32">
	<div class="mx-auto max-w-6xl px-6">
		<div class="grid items-start gap-14 lg:grid-cols-2">
			<div use:reveal>
				<p class="text-sm font-semibold tracking-widest text-brand-300 uppercase">Pricing</p>
				<h2 class="mt-3 text-3xl font-bold tracking-tight sm:text-4xl">
					No software license fee
				</h2>
				<p class="mt-5 text-lg leading-relaxed text-muted-foreground">
					Kuayle does not charge per user and does not enforce paid feature tiers. The software is
					available under Apache 2.0 and runs on infrastructure you operate.
				</p>
				<p class="mt-4 text-lg leading-relaxed text-muted-foreground">
					You remain responsible for infrastructure, storage, backups, monitoring, upgrades and the
					time required to operate the instance.
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
							<span class="text-sm text-muted-foreground">software license</span>
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
						href="/self-hosting"
						size="lg"
						class="mt-8 h-11 w-full bg-brand-400 text-base text-white shadow-lg shadow-brand-400/30 hover:bg-brand-500"
					>
						Read the deployment guide
					</Button>
				</div>
			</div>
		</div>
	</div>
</section>
