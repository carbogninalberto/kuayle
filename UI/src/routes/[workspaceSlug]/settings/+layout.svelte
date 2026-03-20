<script lang="ts">
	import { page } from '$app/state';
	import { ArrowLeft, Users, Tag, Webhook, Settings, FileText } from 'lucide-svelte';
	import type { Snippet } from 'svelte';

	let { children }: { children: Snippet } = $props();

	const slug = $derived(page.params.workspaceSlug ?? '');
	const currentPath = $derived(page.url.pathname);

	function isActive(path: string): boolean {
		return currentPath === path || currentPath.startsWith(path + '/');
	}

	const sections = $derived([
		{ label: 'General', href: `/${slug}/settings`, icon: Settings, exact: true },
		{ label: 'Members', href: `/${slug}/settings/members`, icon: Users },
		{ label: 'Labels', href: `/${slug}/settings/labels`, icon: Tag },
		{ label: 'Webhooks', href: `/${slug}/settings/webhooks`, icon: Webhook },
		{ label: 'Templates', href: `/${slug}/settings/templates`, icon: FileText },
	]);
</script>

<div class="flex h-full">
	<!-- Settings sidebar -->
	<aside class="w-56 shrink-0 border-r border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
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
	</aside>

	<!-- Settings content -->
	<div class="flex-1 overflow-y-auto">
		{@render children()}
	</div>
</div>
