<script lang="ts">
	import { reveal } from '$lib/actions/reveal';
	import ScrollText from '@lucide/svelte/icons/scroll-text';
	import GitFork from '@lucide/svelte/icons/git-fork';
	import Database from '@lucide/svelte/icons/database';

	const facts = [
		{
			icon: ScrollText,
			title: 'Apache 2.0 license',
			description:
				'Use, modify and redistribute Kuayle, including for commercial purposes, subject to the Apache 2.0 terms.'
		},
		{
			icon: GitFork,
			title: 'One public edition',
			description:
				'The implemented issue tracking, planning, GitHub, real-time and access-control features are maintained in the public repository.'
		},
		{
			icon: Database,
			title: 'Your database, your data',
			description:
				'Application records stay in your PostgreSQL database. Uploads use local storage or an S3-compatible service that you configure.'
		}
	];
</script>

<section id="open-source" class="border-y border-border bg-black/30 py-20 sm:py-24">
	<div class="mx-auto max-w-6xl px-6">
		<div class="max-w-2xl" use:reveal>
			<p class="text-sm font-semibold tracking-widest text-brand-300 uppercase">Source and license</p>
			<h2 class="mt-3 text-3xl font-bold tracking-tight sm:text-4xl">
				One public edition
			</h2>
			<p class="mt-4 text-lg text-muted-foreground">
				Kuayle is distributed under Apache 2.0 from one public repository. There is no separate
				commercial codebase for the features listed on this site.
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
			<a href="/open-source" class="text-brand-200 underline underline-offset-4 hover:text-brand-100">Read how the licensing model works</a>.
			Verify the claims directly:
			<a
				href="https://github.com/carbogninalberto/kuayle"
				target="_blank"
				rel="noopener"
				class="text-brand-200 underline underline-offset-4 hover:text-brand-100"
				>the repo is public</a
			>. Read the license and inspect the implementation before deploying it.
		</p>
	</div>
</section>
