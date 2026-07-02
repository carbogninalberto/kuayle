<script lang="ts">
	import { reveal } from '$lib/actions/reveal';
	import Keyboard from '@lucide/svelte/icons/keyboard';
	import RefreshCw from '@lucide/svelte/icons/refresh-cw';
	import GitPullRequest from '@lucide/svelte/icons/git-pull-request';
	import Zap from '@lucide/svelte/icons/zap';
	import Layers from '@lucide/svelte/icons/layers';
	import Users from '@lucide/svelte/icons/users';

	const features = [
		{
			icon: Keyboard,
			title: 'Keyboard-driven',
			description:
				'Single-key actions for create, assign, status and labels. A command palette for the rest. Built for people who find the mouse slow.'
		},
		{
			icon: RefreshCw,
			title: 'Cycles',
			description:
				'Time-boxed sprints with velocity and burndown charts. Unfinished issues roll over automatically.'
		},
		{
			icon: GitPullRequest,
			title: 'GitHub integration',
			description:
				'A GitHub App links PRs to issues and moves them through your workflow when branches merge. Configured per repo.'
		},
		{
			icon: Zap,
			title: 'Real-time sync',
			description:
				'Changes propagate to every open client over WebSockets. Two people can work the same board without stepping on each other.'
		},
		{
			icon: Layers,
			title: 'Views, triage, labels',
			description:
				'Saved filters as shareable views, a triage inbox for incoming work, hierarchical labels and per-team custom workflows.'
		},
		{
			icon: Users,
			title: 'Teams and access control',
			description:
				'Multiple teams per workspace, owner/admin/member/guest roles, and read-only public links for sharing outside the team.'
		}
	];
</script>

<section id="features" class="relative py-24 sm:py-32">
	<div class="mx-auto max-w-6xl px-6">
		<div class="max-w-2xl" use:reveal>
			<p class="text-sm font-semibold tracking-widest text-brand-300 uppercase">Features</p>
			<h2 class="mt-3 text-3xl font-bold tracking-tight sm:text-4xl">What's in the repo</h2>
			<p class="mt-4 text-lg text-muted-foreground">
				The full feature set, as it ships today. No tiers, so no comparison table.
			</p>
		</div>

		<div class="mt-14 grid gap-5 sm:grid-cols-2 lg:grid-cols-3">
			{#each features as feature, i (feature.title)}
				<div
					use:reveal={{ delay: (i % 3) * 100 }}
					class="group relative rounded-2xl border border-border bg-card/60 p-6 transition-all duration-300 hover:-translate-y-1 hover:border-brand-400/40 hover:bg-card hover:shadow-xl hover:shadow-brand-400/10"
				>
					<div
						class="flex size-11 items-center justify-center rounded-xl border border-brand-400/25 bg-brand-400/10 text-brand-300 transition-colors group-hover:bg-brand-400/20"
					>
						<feature.icon class="size-5" />
					</div>
					<h3 class="mt-5 text-lg font-semibold">{feature.title}</h3>
					<p class="mt-2 text-sm leading-relaxed text-muted-foreground">{feature.description}</p>
				</div>
			{/each}
		</div>
	</div>
</section>
