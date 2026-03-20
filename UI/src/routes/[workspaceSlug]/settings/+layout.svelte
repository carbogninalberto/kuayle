<script lang="ts">
	import { page } from '$app/state';
	import { onMount } from 'svelte';
	import { ArrowLeft, Users, Tag, Webhook, Settings, FileText, Settings2, CircleDot, ChevronDown, ChevronRight } from 'lucide-svelte';
	import type { Snippet } from 'svelte';
	import type { Team } from '$lib/types/team';
	import { listTeams } from '$lib/api/teams';

	let { children }: { children: Snippet } = $props();

	const slug = $derived(page.params.workspaceSlug ?? '');
	const currentPath = $derived(page.url.pathname);

	let teams = $state<Team[]>([]);
	let expandedTeams = $state<Set<string>>(new Set());

	onMount(async () => {
		teams = await listTeams(slug);
		// Auto-expand the team if we're on its settings page
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
		<div class="flex h-[49px] items-center gap-2 border-b border-[var(--app-border)] px-4">
			<a
				href="/{slug}/dashboard"
				class="rounded p-1 text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]"
				title="Back"
			>
				<ArrowLeft size={16} />
			</a>
			<span class="text-sm font-medium text-[var(--color-text-primary)]">Settings</span>
		</div>
		<nav class="p-2 space-y-0.5">
			{#each sections as section}
				{@const Icon = section.icon}
				<a
					href={section.href}
					class="flex items-center gap-2 rounded-md px-2 py-1.5 text-sm {(section.exact ? currentPath === section.href : isActive(section.href))
						? 'bg-[var(--color-bg-hover)] text-[var(--color-text-primary)]'
						: 'text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]'}"
				>
					<Icon size={16} />
					{section.label}
				</a>
			{/each}
		</nav>

		<!-- Teams section -->
		{#if teams.length > 0}
			<div class="px-2 pt-3 pb-1">
				<span class="px-2 text-[10px] font-medium uppercase tracking-wider text-[var(--color-text-tertiary)]">Teams</span>
			</div>
			<nav class="px-2 pb-2 space-y-0.5">
				{#each teams as team}
					<div>
						<button
							onclick={() => toggleTeam(team.id)}
							class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]"
						>
							{#if expandedTeams.has(team.id)}
								<ChevronDown size={14} class="shrink-0 text-[var(--color-text-tertiary)]" />
							{:else}
								<ChevronRight size={14} class="shrink-0 text-[var(--color-text-tertiary)]" />
							{/if}
							<span class="truncate">{team.name}</span>
						</button>
						{#if expandedTeams.has(team.id)}
							<div class="ml-4 mt-0.5 space-y-0.5">
								<span class="block px-2 py-1 text-[10px] font-medium uppercase tracking-wider text-[var(--color-text-tertiary)]">Workflow</span>
								<a
									href="/{slug}/settings/teams/{team.id}/statuses"
									class="flex items-center gap-2 rounded-md px-2 py-1.5 text-sm {isActive(`/${slug}/settings/teams/${team.id}/statuses`)
										? 'bg-[var(--color-bg-hover)] text-[var(--color-text-primary)]'
										: 'text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]'}"
								>
									<CircleDot size={14} />
									Issue statuses
								</a>
							</div>
						{/if}
					</div>
				{/each}
			</nav>
		{/if}
	</aside>

	<!-- Settings content -->
	<div class="flex-1 overflow-y-auto">
		{@render children()}
	</div>
</div>
