<script lang="ts">
	import { reveal } from '$lib/actions/reveal';
	import ScrollText from '@lucide/svelte/icons/scroll-text';
	import GitFork from '@lucide/svelte/icons/git-fork';
	import Database from '@lucide/svelte/icons/database';

	const facts = [
		{
			icon: ScrollText,
			title: 'One license: Apache 2.0',
			description:
				'Use it commercially, modify it, redistribute it. No dual licensing, no CLA that lets us relicense your contributions later, no "source available" fine print.'
		},
		{
			icon: GitFork,
			title: 'No enterprise edition',
			description:
				'There is no second, private repo where the good features live. SSO-style access control, analytics, GitHub integration - everything ships in the same public codebase.'
		},
		{
			icon: Database,
			title: 'Your database, your data',
			description:
				'Kuayle runs on your servers against your own Postgres. If you ever leave, your data is already sitting in tables you control. Nothing to export, nothing held hostage.'
		}
	];
</script>

<section id="open-source" class="border-y border-border bg-black/30 py-20 sm:py-24">
	<div class="mx-auto max-w-6xl px-6">
		<div class="max-w-2xl" use:reveal>
			<p class="text-sm font-semibold tracking-widest text-brand-300 uppercase">Open source</p>
			<h2 class="mt-3 text-3xl font-bold tracking-tight sm:text-4xl">
				Open source, not open core
			</h2>
			<p class="mt-4 text-lg text-muted-foreground">
				Most "open source" trackers keep the useful parts in a commercial edition. Kuayle
				doesn't. Here is exactly what that means:
			</p>
		</div>

		<div class="mt-12 grid gap-5 md:grid-cols-3">
			{#each facts as fact, i (fact.title)}
				<div
					use:reveal={{ delay: i * 100 }}
					class="rounded-2xl border border-border bg-card/60 p-6"
				>
					<div
						class="flex size-11 items-center justify-center rounded-xl border border-brand-400/25 bg-brand-400/10 text-brand-300"
					>
						<fact.icon class="size-5" />
					</div>
					<h3 class="mt-5 text-lg font-semibold">{fact.title}</h3>
					<p class="mt-2 text-sm leading-relaxed text-muted-foreground">{fact.description}</p>
				</div>
			{/each}
		</div>

		<p class="mt-10 text-sm text-muted-foreground" use:reveal>
			Don't take our word for it:
			<a
				href="https://github.com/carbogninalberto/kuayle"
				target="_blank"
				rel="noopener"
				class="text-brand-200 underline underline-offset-4 hover:text-brand-100"
				>the repo is public</a
			>. Check the license, read the code, count the feature flags. There are none.
		</p>
	</div>
</section>
