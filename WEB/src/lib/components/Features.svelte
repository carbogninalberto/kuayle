<script lang="ts">
	import { reveal } from '$lib/actions/reveal';
	import Keyboard from '@lucide/svelte/icons/keyboard';
	import RefreshCw from '@lucide/svelte/icons/refresh-cw';
	import GitPullRequest from '@lucide/svelte/icons/git-pull-request';
	import Zap from '@lucide/svelte/icons/zap';
	import Layers from '@lucide/svelte/icons/layers';
	import Users from '@lucide/svelte/icons/users';
	import SquareTerminal from '@lucide/svelte/icons/square-terminal';
	import ChartColumn from '@lucide/svelte/icons/chart-column';
	import FolderKanban from '@lucide/svelte/icons/folder-kanban';
	import History from '@lucide/svelte/icons/history';
	import Lock from '@lucide/svelte/icons/lock';
	import ArrowRightLeft from '@lucide/svelte/icons/arrow-right-left';
	import ArrowRight from '@lucide/svelte/icons/arrow-right';
	import { DEV_MACHINES_RELEASE_STATUS } from '$lib/config/releases';
	import { useLatestRelease } from '$lib/release.svelte';

	const release = useLatestRelease();

	const features = [
		{
			icon: Keyboard,
			title: 'Keyboard-driven',
			description:
				'Create issues with C, search with Cmd/Ctrl+K, and update status, priority, assignees or labels from the issue view.',
			href: '/features/keyboard-shortcuts',
			span: 'lg:col-span-2',
			tags: []
		},
		{
			icon: RefreshCw,
			title: 'Cycles',
			description:
				'Time-box work with the full issue list, a searchable issue picker, burndown and velocity charts, and optional carry-over.',
			href: '/features/cycles',
			span: 'lg:col-span-2',
			tags: []
		},
		{
			icon: FolderKanban,
			title: 'Projects',
			description:
				'Group issues across teams and work them in list, board or Gantt layouts with project dates, a lead and progress.',
			href: '/features/projects',
			span: 'lg:col-span-2',
			tags: []
		},
		{
			icon: SquareTerminal,
			title: 'Dev Machines',
			description:
				'Unreleased opt-in multi-container coding environments: code-server, a native terminal, an in-browser Chrome and agent providers working issue worktrees — all behind an authenticated gateway.',
			href: '/features/dev-machines',
			span: 'lg:col-span-3',
			tags: [DEV_MACHINES_RELEASE_STATUS, 'Claude Code', 'OpenCode', 'Codex', 'Custom CLIs']
		},
		{
			icon: GitPullRequest,
			title: 'GitHub integration',
			description:
				'Match issue IDs in branches, commits and pull requests, move issues with configurable rules, and get a bot comment linking each new pull request to its issue.',
			href: '/features/github-integration',
			span: 'lg:col-span-3',
			tags: ['Auto-linking', 'Status rules', 'PR comments']
		},
		{
			icon: Lock,
			title: 'Private teams',
			description:
				'Limit issues, projects, cycles, comments and attachments to team members. Owners and admins keep access.',
			href: '/features/teams-and-access-control',
			span: 'lg:col-span-2',
			tags: []
		},
		{
			icon: History,
			title: 'Version history',
			description:
				'Review every recorded title and description edit with its author, time and full before/after values.',
			href: '/features/issue-tracking',
			span: 'lg:col-span-2',
			tags: []
		},
		{
			icon: ChartColumn,
			title: 'Analytics & Insights',
			description:
				'Workspace and team overviews, burn-up trends and an insight builder for count, lead time and cycle time.',
			href: '/features/analytics-insights',
			span: 'lg:col-span-2',
			tags: []
		},
		{
			icon: Layers,
			title: 'Views, triage, labels',
			description:
				'Saved filters as shareable views, a triage inbox for incoming work, hierarchical labels and per-team custom workflows.',
			href: '/features/views-and-triage',
			span: 'lg:col-span-3',
			tags: []
		},
		{
			icon: Users,
			title: 'Roles, invites and tokens',
			description:
				'Owner, admin, member and guest roles, invite links, optional invite-only sign-up and scoped personal access tokens.',
			href: '/features/teams-and-access-control',
			span: 'lg:col-span-3',
			tags: []
		},
		{
			icon: Zap,
			title: 'Real-time sync',
			description:
				'WebSocket events notify connected workspace clients about issue, comment, cycle and GitHub changes.',
			href: '/features/real-time-sync',
			span: 'lg:col-span-3',
			tags: []
		},
		{
			icon: ArrowRightLeft,
			title: 'Workspace transfer',
			description:
				'Export a workspace as a portable archive and import it on another instance, with secrets left behind.',
			href: '/self-hosting/workspace-transfer',
			span: 'lg:col-span-3',
			tags: []
		}
	];
</script>

<section id="features" class="relative py-24 sm:py-32">
	<div class="mx-auto max-w-6xl px-6">
		<div class="max-w-2xl" use:reveal>
			<p class="text-sm font-semibold tracking-widest text-brand-300 uppercase">Features</p>
			<h2 class="mt-3 text-3xl font-bold tracking-tight sm:text-4xl">Implemented in Kuayle today</h2>
			<p class="mt-4 text-lg text-muted-foreground">
				The current {release.version} release covers issue tracking, cycles and projects, private teams, GitHub automation
				and analytics. Dev Machines are {DEV_MACHINES_RELEASE_STATUS.toLowerCase()}
				development-branch functionality. The source for each feature is available in the public repository.
			</p>
		</div>

		<div class="mt-14 grid gap-5 sm:grid-cols-2 lg:grid-cols-6">
			{#each features as feature, i (feature.title)}
				<a
					href={feature.href}
					use:reveal={{ delay: (i % 3) * 100 }}
					class="group relative flex flex-col overflow-hidden rounded-2xl border border-border bg-card/60 p-6 transition-all duration-300 hover:-translate-y-1 hover:border-brand-400/40 hover:bg-card hover:shadow-xl hover:shadow-brand-400/10 {feature.span}"
				>
					{#if feature.tags.length > 0}
						<div
							class="absolute -top-16 -right-16 size-40 rounded-full bg-brand-400/15 blur-3xl transition-opacity duration-300 group-hover:opacity-100"
						></div>
					{/if}
					<div
						class="flex size-11 items-center justify-center rounded-xl border border-brand-400/25 bg-brand-400/10 text-brand-300 transition-colors group-hover:bg-brand-400/20"
					>
						<feature.icon class="size-5" />
					</div>
					<h3 class="mt-5 text-lg font-semibold">{feature.title}</h3>
					<p class="mt-2 text-sm leading-relaxed text-muted-foreground">{feature.description}</p>
					{#if feature.tags.length > 0}
						<div class="mt-4 flex flex-wrap gap-1.5">
							{#each feature.tags as tag (tag)}
								<span
									class="rounded-full border border-brand-400/25 bg-brand-400/10 px-2.5 py-0.5 text-[11px] font-medium text-brand-200"
								>
									{tag}
								</span>
							{/each}
						</div>
					{/if}
					<span
						class="mt-4 flex items-center gap-1 text-xs font-medium text-brand-300 opacity-0 transition-opacity group-hover:opacity-100"
					>
						View feature <ArrowRight class="size-3" />
					</span>
				</a>
			{/each}
		</div>

		<div class="mt-8 text-center" use:reveal>
			<a
				href="/features"
				class="inline-flex items-center gap-1.5 text-sm font-medium text-brand-300 transition-colors hover:text-brand-200"
			>
				All features <ArrowRight class="size-3.5" />
			</a>
		</div>
	</div>
</section>
