<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { logout } from '$lib/api/auth';
	import { authState } from '$lib/features/auth/auth.state.svelte';
	import type { Workspace } from '$lib/types/workspace';
	import type { Team } from '$lib/types/team';
	import {
		Inbox,
		LayoutDashboard,
		CircleUser,
		Settings,
		LogOut,
		Users,
		FolderKanban,
		ChevronDown
	} from 'lucide-svelte';

	let {
		workspace,
		teams,
		slug
	}: { workspace: Workspace; teams: Team[]; slug: string } = $props();

	const currentPath = $derived(page.url.pathname);

	function isActive(path: string): boolean {
		return currentPath.startsWith(path);
	}

	async function handleLogout() {
		await logout();
		authState.clear();
		goto('/login');
	}
</script>

<aside
	class="flex h-full w-60 flex-col border-r border-[var(--app-border)] bg-[var(--color-bg-secondary)]"
>
	<!-- Workspace header -->
	<div class="flex items-center gap-2 border-b border-[var(--app-border)] px-4 py-3">
		<div
			class="flex h-6 w-6 items-center justify-center rounded bg-[var(--app-accent)] text-xs font-bold text-white"
		>
			{workspace.name.charAt(0).toUpperCase()}
		</div>
		<span class="text-sm font-medium text-[var(--color-text-primary)]">{workspace.name}</span>
	</div>

	<!-- Navigation -->
	<nav class="flex-1 overflow-y-auto px-2 py-2">
		<div class="space-y-0.5">
			<a
				href="/{slug}/inbox"
				class="flex items-center gap-2 rounded-md px-2 py-1.5 text-sm {isActive(`/${slug}/inbox`)
					? 'bg-[var(--color-bg-hover)] text-[var(--color-text-primary)]'
					: 'text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]'}"
			>
				<Inbox size={16} />
				Inbox
			</a>
			<a
				href="/{slug}/my-issues"
				class="flex items-center gap-2 rounded-md px-2 py-1.5 text-sm {isActive(
					`/${slug}/my-issues`
				)
					? 'bg-[var(--color-bg-hover)] text-[var(--color-text-primary)]'
					: 'text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]'}"
			>
				<CircleUser size={16} />
				My Issues
			</a>
			<a
				href="/{slug}/dashboard"
				class="flex items-center gap-2 rounded-md px-2 py-1.5 text-sm {isActive(
					`/${slug}/dashboard`
				)
					? 'bg-[var(--color-bg-hover)] text-[var(--color-text-primary)]'
					: 'text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]'}"
			>
				<LayoutDashboard size={16} />
				Dashboard
			</a>
		</div>

		<!-- Teams -->
		<div class="mt-4">
			<div class="flex items-center justify-between px-2 py-1">
				<span class="text-xs font-medium uppercase text-[var(--color-text-tertiary)]">Teams</span>
			</div>
			{#each teams as team}
				<a
					href="/{slug}/teams/{team.id}"
					class="flex items-center gap-2 rounded-md px-2 py-1.5 text-sm {isActive(
						`/${slug}/teams/${team.id}`
					)
						? 'bg-[var(--color-bg-hover)] text-[var(--color-text-primary)]'
						: 'text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]'}"
				>
					<Users size={16} />
					{team.name}
				</a>
			{/each}
		</div>

		<!-- Projects -->
		<div class="mt-4">
			<a
				href="/{slug}/projects"
				class="flex items-center gap-2 rounded-md px-2 py-1.5 text-sm {isActive(
					`/${slug}/projects`
				)
					? 'bg-[var(--color-bg-hover)] text-[var(--color-text-primary)]'
					: 'text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]'}"
			>
				<FolderKanban size={16} />
				Projects
			</a>
		</div>
	</nav>

	<!-- Footer -->
	<div class="border-t border-[var(--app-border)] px-2 py-2">
		<a
			href="/{slug}/settings"
			class="flex items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]"
		>
			<Settings size={16} />
			Settings
		</a>
		<button
			onclick={handleLogout}
			class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]"
		>
			<LogOut size={16} />
			Log out
		</button>
	</div>
</aside>
