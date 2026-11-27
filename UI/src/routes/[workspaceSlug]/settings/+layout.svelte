<script lang="ts">
	import { getCurrentWorkspace } from '$lib/features/workspaces/current-workspace.svelte';
	import { page } from '$app/state';
	import { onMount } from 'svelte';
	import { cubicOut } from 'svelte/easing';
	import { prefersReducedMotion } from 'svelte/motion';
	import { IsMobile } from '$lib/hooks/is-mobile.svelte';
	import { SettingsPageHeader } from '$lib/components/settings';
	import {
		Lightbulb,
		ArrowLeft,
		User,
		Users,
		Tag,
		Webhook,
		Settings,
		FileText,
		ScrollText,
		Settings2,
		Sparkles,
		SlidersHorizontal,
		CircleDot,
		ChevronDown,
		Menu,
		X,
		RefreshCw,
		KeyRound,
		Plug,
		Bot,
		MessagesSquare,
		Mic,
		BotMessageSquare,
		Workflow,
		GitBranch,
		DatabaseBackup,
		Bell,
		ShieldCheck,
		Import,
		SlidersVertical
	} from '@lucide/svelte';
	import { GithubLogoIcon } from 'phosphor-svelte';
	import type { Snippet } from 'svelte';
	import type { Team } from '$lib/types/team';
	import { listTeams } from '$lib/api/teams';
	import TeamIcon from '$lib/components/shared/TeamIcon.svelte';
	import * as Sheet from '$lib/components/ui/sheet';
	import { Button } from '$lib/components/ui/button';
	import { authState } from '$lib/features/auth/auth.state.svelte';
	import { demoMode } from '$lib/demo';
	import { m } from '$lib/features/i18n/messages';

	function slideFade(node: HTMLElement, params: { duration?: number } = {}) {
		const duration = prefersReducedMotion.current ? 0 : (params.duration ?? 200);
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
	const canUseDevMachines = $derived(!demoMode || authState.user?.is_sysadmin === true);
	// Guests cannot read the workspace's lessons; the server refuses them.
	const currentWorkspace = getCurrentWorkspace();
	const isGuest = $derived(currentWorkspace.membership?.workspace.current_user_role === 'guest');
	const canManageWorkspace = $derived(currentWorkspace.membership?.workspace.current_user_role === 'owner');
	// SSO settings are for owners and admins; the server enforces the same rule.
	const canAdministerWorkspace = $derived(
		['owner', 'admin'].includes(currentWorkspace.membership?.workspace.current_user_role ?? '')
	);

	let teams = $state<Team[]>([]);
	let expandedTeams = $state<Set<string>>(new Set());
	let showMobileNav = $state(false);
	const mobile = new IsMobile();

	$effect(() => {
		if (!mobile.current) showMobileNav = false;
	});

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

	function currentItem(path: string, exact = false) {
		if (currentPath === path) return 'page';
		if (!exact && currentPath.startsWith(path + '/')) return 'location';
	}

	const sectionGroups = $derived([
		{
			label: m['settings.nav.personal'](),
			items: [
				{ label: m['settings.nav.profile'](), href: `/${slug}/settings/profile`, icon: User },
				{ label: m['settings.nav.preferences'](), href: `/${slug}/settings/preferences`, icon: Settings2 },
				{ label: m['settings.nav.api_tokens'](), href: `/${slug}/settings/tokens`, icon: KeyRound },
				{ label: m['settings.mcp.title'](), href: `/${slug}/settings/mcp`, icon: Plug }
			]
		},
		{
			label: m['settings.nav.workspace'](),
			items: [
				{ label: m['settings.nav.general'](), href: `/${slug}/settings`, icon: Settings, exact: true },
				{ label: m['settings.nav.members'](), href: `/${slug}/settings/members`, icon: Users },
				{ label: m['settings.nav.labels'](), href: `/${slug}/settings/labels`, icon: Tag },
				{ label: m['settings.nav.webhooks'](), href: `/${slug}/settings/webhooks`, icon: Webhook },
				...(canAdministerWorkspace
					? [
							{ label: m['settings.nav.sso'](), href: `/${slug}/settings/sso`, icon: ShieldCheck },
							{ label: m['settings.nav.import'](), href: `/${slug}/settings/import`, icon: Import }
						]
					: []),
				{ label: m['settings.nav.github'](), href: `/${slug}/settings/github`, icon: GithubLogoIcon },
				...(canAdministerWorkspace ? [{ label: "Slack", href: `/${slug}/settings/slack`, icon: MessagesSquare }] : []),
				...(canManageWorkspace
					? [{ label: m['settings.nav.forges'](), href: `/${slug}/settings/forges`, icon: GitBranch }]
					: []),
				{ label: m['settings.nav.templates'](), href: `/${slug}/settings/templates`, icon: FileText },
				{ label: m['settings.nav.ai'](), href: `/${slug}/settings/ai`, icon: Sparkles },
				{ label: m['settings.nav.assistant'](), href: `/${slug}/settings/assistant`, icon: BotMessageSquare },
				{ label: m['telegram.title'](), href: `/${slug}/settings/telegram`, icon: Sparkles },
				{ label: m['settings.nav.voice'](), href: `/${slug}/settings/voice`, icon: Mic },
				...(canUseDevMachines
					? [
							{ label: m['settings.nav.dev_machines'](), href: `/${slug}/settings/dev-machines`, icon: SlidersHorizontal },
							{ label: m['settings.nav.agents'](), href: `/${slug}/settings/agents`, icon: Bot },
							{ label: m['settings.nav.workflows'](), href: `/${slug}/settings/workflows`, icon: Workflow },
							...(isGuest
								? []
								: [{ label: m['lessons.title'](), href: `/${slug}/settings/lessons`, icon: Lightbulb }]),
							{ label: m['agent_messages.nav'](), href: `/${slug}/settings/agent-messages`, icon: MessagesSquare }
						]
					: [])
			]
		},
		{
			label: m['settings.nav.system'](),
			items: [
				{ label: m['settings.nav.version'](), href: `/${slug}/settings/version`, icon: RefreshCw },
				...(authState.user?.is_sysadmin === true
					? [
							{ label: m['settings.nav.backups'](), href: `/${slug}/settings/backups`, icon: DatabaseBackup },
							{ label: m['settings.push.title'](), href: `/${slug}/settings/push`, icon: Bell },
							{
								label: m['settings.instance_config.title'](),
								href: `/${slug}/settings/instance-config`,
								icon: SlidersVertical
							}
						]
					: []),
				{ label: m['settings.nav.licenses'](), href: `/${slug}/settings/licenses`, icon: ScrollText }
			]
		}
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

{#snippet settingsNav(inSheet = false)}
	<div class="flex min-h-[49px] shrink-0 items-center gap-2 px-3">
		<a
			href="/{slug}/my-issues"
			onclick={() => (showMobileNav = false)}
			class="inline-flex items-center justify-center rounded-md p-1 max-md:size-[44px] text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]"
			title={m['settings.back']()}
		>
			<ArrowLeft size={16} />
		</a>
		<span class="text-sm font-medium text-[var(--color-text-primary)]">{m['settings.title']()}</span>
		{#if inSheet}
			<Sheet.Close>
				{#snippet child({ props })}
					<Button {...props} variant="ghost" size="icon" class="ml-auto size-[44px]" aria-label={m['common.close']()}>
						<X size={18} />
					</Button>
				{/snippet}
			</Sheet.Close>
		{/if}
	</div>

	<nav aria-label={m['settings.nav.settings_navigation']()} class="min-h-0 flex-1 overflow-y-auto overscroll-contain px-2 py-2">
		<div class="space-y-4">
			{#each sectionGroups as group}
				<div>
					<p class="px-2 pb-1 text-[11px] font-medium uppercase tracking-wide text-[var(--color-text-tertiary)]">
						{group.label}
					</p>
					<div class="space-y-px">
						{#each group.items as section}
							{@const Icon = section.icon}
							{@const current = currentItem(section.href, section.exact)}
							<a
								href={section.href}
								onclick={() => (showMobileNav = false)}
								aria-current={current}
								class="nav-link flex items-center gap-2 rounded-md px-2 py-1 text-sm {current
									? 'bg-[var(--color-bg-hover)]/50 text-[var(--color-text-primary)]'
									: 'text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]'}"
							>
								<Icon size={16} class="shrink-0" />
								<span class="truncate">{section.label}</span>
							</a>
						{/each}
					</div>
				</div>
			{/each}
		</div>

		<!-- Teams section -->
		{#if teams.length > 0}
			<div class="mt-4">
				{#each teams as team}
					{@const expanded = expandedTeams.has(team.id)}
					{@const teamPath = `/${slug}/settings/teams/${team.id}`}
					<Button variant="plain" type="button"
						aria-expanded={expanded}
						class="flex w-full max-md:min-h-[44px] cursor-pointer items-center gap-2 rounded-md px-2 py-1 text-left text-sm text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]"
						onclick={() => toggleTeam(team.id)}
					>
						<ChevronDown
							size={12}
							class="shrink-0 text-[var(--color-text-tertiary)] motion-safe:transition-transform {expanded ? '' : '-rotate-90'}"
						/>
						<TeamIcon {team} />
						<span class="truncate">{team.name}</span>
					</Button>
					{#if expanded}
						{@const generalCurrent = currentItem(teamPath, true)}
						{@const statusesCurrent = currentItem(`${teamPath}/statuses`)}
						<div transition:slideFade>
							<a
								href={teamPath}
								onclick={() => (showMobileNav = false)}
								aria-current={generalCurrent}
								class="nav-link ml-7 flex items-center gap-2 rounded-md px-2 py-1 text-xs {generalCurrent
									? 'bg-[var(--color-bg-hover)]/50 text-[var(--color-text-primary)]'
									: 'text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]'}"
							>
								<SlidersHorizontal size={13} />
								{m['settings.nav.general']()}
							</a>
							<a
								href="{teamPath}/statuses"
								onclick={() => (showMobileNav = false)}
								aria-current={statusesCurrent}
								class="nav-link ml-7 flex items-center gap-2 rounded-md px-2 py-1 text-xs {statusesCurrent
									? 'bg-[var(--color-bg-hover)]/50 text-[var(--color-text-primary)]'
									: 'text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]'}"
							>
								<CircleDot size={13} />
								{m['settings.nav.issue_statuses']()}
							</a>
						</div>
					{/if}
				{/each}
			</div>
		{/if}
	</nav>
{/snippet}

<div class="flex h-full min-w-0 flex-col">
	<Sheet.Root bind:open={showMobileNav}>
		<div class="mobile-bar shrink-0 border-b border-[var(--app-border)] bg-[var(--color-bg)] md:hidden">
			<SettingsPageHeader title={m['settings.title']()} compact>
				{#snippet action()}
					<Sheet.Trigger>
						{#snippet child({ props })}
							<Button {...props} variant="ghost" size="icon" class="size-[44px]" aria-label={m['settings.open_menu']()}>
								<Menu size={18} />
							</Button>
						{/snippet}
					</Sheet.Trigger>
				{/snippet}
			</SettingsPageHeader>
		</div>

		<Sheet.Content
			side="left"
			showCloseButton={false}
			class="gap-0 bg-[var(--color-bg-secondary)] data-[side=left]:w-[min(88vw,320px)] motion-reduce:animate-none! motion-reduce:transition-none"
		>
			<Sheet.Header class="sr-only">
				<Sheet.Title>{m['settings.nav.settings_navigation']()}</Sheet.Title>
				<Sheet.Description>{m['settings.nav.navigate_sections']()}</Sheet.Description>
			</Sheet.Header>
			<div class="mobile-sheet flex min-h-0 flex-1 flex-col">
				{@render settingsNav(true)}
			</div>
		</Sheet.Content>
	</Sheet.Root>

	<div class="flex min-h-0 flex-1">
		<aside class="hidden min-h-0 w-56 shrink-0 flex-col border-r border-[var(--app-border)] bg-[var(--color-bg-secondary)] md:flex">
			{@render settingsNav()}
		</aside>
		<div class="min-w-0 flex-1 overflow-y-auto" data-testid="settings-content">
			{@render children()}
		</div>
	</div>
</div>

<style>
	.mobile-bar {
		padding-block: calc(env(safe-area-inset-top) + 2px) 2px;
		padding-left: max(0.75rem, env(safe-area-inset-left));
		padding-right: max(0.75rem, env(safe-area-inset-right));
	}

	.mobile-sheet {
		padding: env(safe-area-inset-top) env(safe-area-inset-right) env(safe-area-inset-bottom) env(safe-area-inset-left);
	}

	.mobile-sheet .nav-link {
		min-height: 44px;
	}
</style>
