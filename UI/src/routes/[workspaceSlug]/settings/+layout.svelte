<script lang="ts">
	import { page } from '$app/state';
	import { onMount } from 'svelte';
	import { cubicOut } from 'svelte/easing';
	import {
		ArrowLeft,
		Users,
		Tag,
		Webhook,
		Settings,
		FileText,
		ScrollText,
		Settings2,
		SlidersHorizontal,
		CircleDot,
		ChevronDown,
		Menu
	} from 'lucide-svelte';
	import { GithubLogoIcon } from 'phosphor-svelte';
	import type { Snippet } from 'svelte';
	import type { Team } from '$lib/types/team';
	import { listTeams } from '$lib/api/teams';
	import TeamIcon from '$lib/components/shared/TeamIcon.svelte';
	import * as Sheet from '$lib/components/ui/sheet';
	import { Button } from '$lib/components/ui/button';

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
	let showMobileNav = $state(false);

	async function loadTeams() {
		teams = await listTeams(slug);
		for (const team of teams) {
			if (currentPath.includes(`/settings/teams/${team.id}`)) {
				expandedTeams.add(team.id);
				expandedTeams = new Set(expandedTeams);
			}
		}
	}

	onMount(() => {
		loadTeams();

		function handleAppRefresh(e: Event) {
			const detail = (e as CustomEvent<{ slug?: string; resources?: string[] }>).detail;
			if (detail?.slug && detail.slug !== slug) return;
			const resources = detail?.resources;
			if (!resources || resources.includes('teams')) {
				loadTeams();
			}
		}

		window.addEventListener('app:refresh', handleAppRefresh);
		return () => window.removeEventListener('app:refresh', handleAppRefresh);
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
		{ label: 'Licenses', href: `/${slug}/settings/licenses`, icon: ScrollText }
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

{#snippet settingsNav()}
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
					class="flex items-center gap-2 rounded-md px-2 py-1 text-sm {(
						section.exact ? currentPath === section.href : isActive(section.href)
					)
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
				{#each teams as team}
					{@const expanded = expandedTeams.has(team.id)}
					<button
						type="button"
						class="flex w-full cursor-pointer items-center gap-2 rounded-md px-2 py-1 text-left text-sm text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]"
						onclick={() => toggleTeam(team.id)}
					>
						<ChevronDown
							size={12}
							class="shrink-0 text-[var(--color-text-tertiary)] transition-transform {expanded ? '' : '-rotate-90'}"
						/>
						<TeamIcon {team} />
						<span class="truncate">{team.name}</span>
					</button>
					{#if expanded}
						<div transition:slideFade>
							<a
								href="/{slug}/settings/teams/{team.id}"
								class="ml-7 flex items-center gap-2 rounded-md px-2 py-1 text-xs {isActive(
									`/${slug}/settings/teams/${team.id}`
								) && !isActive(`/${slug}/settings/teams/${team.id}/statuses`)
									? 'bg-[var(--color-bg-hover)]/50 text-[var(--color-text-primary)]'
									: 'text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]'}"
							>
								<SlidersHorizontal size={13} />
								General
							</a>
							<a
								href="/{slug}/settings/teams/{team.id}/statuses"
								class="ml-7 flex items-center gap-2 rounded-md px-2 py-1 text-xs {isActive(
									`/${slug}/settings/teams/${team.id}/statuses`
								)
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
{/snippet}

<div class="flex h-full min-w-0 flex-col">
	<!-- Mobile top bar -->
	<div
		class="flex h-12 shrink-0 items-center gap-2 border-b border-[var(--app-border)] bg-[var(--color-bg)] px-3 md:hidden"
	>
		<Button variant="ghost" size="icon-lg" onclick={() => (showMobileNav = true)} aria-label="Open settings menu">
			<Menu size={18} />
		</Button>
		<span class="text-sm font-medium text-[var(--color-text-primary)]">Settings</span>
	</div>

	<div class="flex min-h-0 flex-1">
		<!-- Desktop settings sidebar -->
		<aside
			class="hidden w-56 shrink-0 border-r border-[var(--app-border)] bg-[var(--color-bg-secondary)] overflow-y-auto md:block"
		>
			{@render settingsNav()}
		</aside>

		<!-- Mobile settings nav sheet -->
		<Sheet.Root bind:open={showMobileNav}>
			<Sheet.Content side="left" class="w-[min(88vw,320px)] p-0 [&>button]:hidden" showCloseButton={false}>
				<Sheet.Header class="sr-only">
					<Sheet.Title>Settings navigation</Sheet.Title>
					<Sheet.Description>Navigate settings sections.</Sheet.Description>
				</Sheet.Header>
				<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
				<div
					role="presentation"
					class="flex h-full flex-col bg-[var(--color-bg-secondary)]"
					onclick={(e) => {
						if ((e.target as HTMLElement).closest('a')) showMobileNav = false;
					}}
				>
					{@render settingsNav()}
				</div>
			</Sheet.Content>
		</Sheet.Root>

		<!-- Settings content -->
		<div class="min-w-0 flex-1 overflow-y-auto">
			{@render children()}
		</div>
	</div>
</div>
