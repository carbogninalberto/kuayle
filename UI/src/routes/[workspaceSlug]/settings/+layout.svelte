<script lang="ts">
	import { page } from '$app/state';
	import { onMount } from 'svelte';
	import { cubicOut } from 'svelte/easing';
	import { ArrowLeft, Users, Tag, Webhook, Settings, FileText, Settings2, CircleDot, ChevronDown } from 'lucide-svelte';
	import { GithubLogoIcon } from 'phosphor-svelte';
	import type { Snippet } from 'svelte';
	import type { Team } from '$lib/types/team';
	import { listTeams } from '$lib/api/teams';

	function slideFade(node: HTMLElement, params: { duration?: number } = {}) {
		const duration = params.duration ?? 200;
		const h = node.offsetHeight;
		return {
			duration,
			easing: cubicOut,
			css: (t: number) => `overflow: hidden; height: ${t * h}px; opacity: ${t};`
		};
	}

	let { children }: { children: Snippet } = $props();

	const slug = $derived(page.params.workspaceSlug ?? '');
	const currentPath = $derived(page.url.pathname);

	let teams = $state<Team[]>([]);
	let expandedTeams = $state<Set<string>>(new Set());

	onMount(async () => {
		teams = await listTeams(slug);
		for (const team of teams) {
			if (currentPath.includes(`/settings/teams/${team.id}`)) {
				expandedTeams.add(team.id);
				expandedTeams = new Set(expandedTeams);
			}
		}
	});

	function isActive(path: string): boolean {
		return currentPath === path || currentPath.startsWith(path + '/');
	}

	const sections = $derived([
		{ label: 'General', href: `/${slug}/settings`, icon: Settings, exact: true },
		{ label: 'Preferences', href: `/${slug}/settings/preferences`, icon: Settings2 },
		{ label: 'Members', href: `/${slug}/settings/members`, icon: Users },
		{ label: 'Labels', href: `/${slug}/settings/labels`, icon: Tag },
		{ label: 'Webhooks', href: `/${slug}/settings/webhooks`, icon: Webhook },
		{ label: 'GitHub', href: `/${slug}/settings/github`, icon: GithubLogoIcon },
		{ label: 'Templates', href: `/${slug}/settings/templates`, icon: FileText },
	]);

	function toggleTeam(teamId: string) {
		if (expandedTeams.has(teamId)) {
			expandedTeams.delete(teamId);
		} else {
			expandedTeams.add(teamId);
		}
		expandedTeams = new Set(expandedTeams);
	}
</script>

<div class="flex h-full">
	<!-- Settings sidebar -->
	<aside class="w-56 shrink-0 border-r border-[var(--app-border)] bg-[var(--color-bg-secondary)] overflow-y-auto">
		<div class="flex h-[49px] items-center gap-2 px-3">
			<a
				href="/{slug}/my-issues"
				class="rounded-md p-1 text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]"
				title="Back"
			>
				<ArrowLeft size={16} />
			</a>
			<span class="text-sm font-medium text-[var(--color-text-primary)]">Settings</span>
		</div>

		<nav class="flex-1 overflow-y-auto overflow-x-hidden px-2 py-2">
			<div class="space-y-px">
				{#each sections as section}
					{@const Icon = section.icon}
					<a
						href={section.href}
						class="flex items-center gap-2 rounded-md px-2 py-1 text-sm {(section.exact ? currentPath === section.href : isActive(section.href))
							? 'bg-[var(--color-bg-hover)]/50 text-[var(--color-text-primary)]'
							: 'text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]'}"
					>
						<Icon size={16} class="shrink-0" />
						<span class="truncate">{section.label}</span>
					</a>
				{/each}
			</div>

			<!-- Teams section -->
			{#if teams.length > 0}
				<div class="mt-4">
					<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
					{#each teams as team}
						{@const expanded = expandedTeams.has(team.id)}
						<div class="flex cursor-pointer items-center gap-2 rounded-md px-2 py-1 text-sm text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]" onclick={() => toggleTeam(team.id)}>
							<ChevronDown size={12} class="shrink-0 text-[var(--color-text-tertiary)] transition-transform {expanded ? '' : '-rotate-90'}" />
							<Users size={16} class="shrink-0" />
							<span class="truncate">{team.name}</span>
						</div>
						{#if expanded}
							<div transition:slideFade>
								<a
									href="/{slug}/settings/teams/{team.id}/statuses"
									class="ml-7 flex items-center gap-2 rounded-md px-2 py-1 text-xs {isActive(`/${slug}/settings/teams/${team.id}/statuses`)
										? 'bg-[var(--color-bg-hover)]/50 text-[var(--color-text-primary)]'
										: 'text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]'}"
								>
									<CircleDot size={13} />
									Issue statuses
								</a>
							</div>
						{/if}
					{/each}
				</div>
			{/if}
		</nav>
	</aside>

	<!-- Settings content -->
	<div class="flex-1 overflow-y-auto">
		{@render children()}
	</div>
</div>
