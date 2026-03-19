<script lang="ts">
	import { page } from '$app/state';
	import { Settings, Users, Tag, Webhook } from 'lucide-svelte';

	const slug = $derived(page.params.workspaceSlug);
</script>

<div class="h-full">
	<div class="flex h-[49px] items-center border-b border-[var(--app-border)] px-6">
		<h1 class="text-sm font-medium text-[var(--color-text-primary)]">Settings</h1>
	</div>

	<div class="space-y-2 p-6">
		<a
			href="/{slug}/settings/members"
			class="flex items-center gap-3 rounded-md p-3 hover:bg-[var(--color-bg-hover)]"
		>
			<Users size={18} class="text-[var(--color-text-tertiary)]" />
			<div>
				<p class="text-sm text-[var(--color-text-primary)]">Members</p>
				<p class="text-xs text-[var(--color-text-tertiary)]">
					Manage workspace members and roles
				</p>
			</div>
		</a>
		<a
			href="/{slug}/settings/labels"
			class="flex items-center gap-3 rounded-md p-3 hover:bg-[var(--color-bg-hover)]"
		>
			<Tag size={18} class="text-[var(--color-text-tertiary)]" />
			<div>
				<p class="text-sm text-[var(--color-text-primary)]">Labels</p>
				<p class="text-xs text-[var(--color-text-tertiary)]">Manage issue labels</p>
			</div>
		</a>
		<a
			href="/{slug}/settings/webhooks"
			class="flex items-center gap-3 rounded-md p-3 hover:bg-[var(--color-bg-hover)]"
		>
			<Webhook size={18} class="text-[var(--color-text-tertiary)]" />
			<div>
				<p class="text-sm text-[var(--color-text-primary)]">Webhooks</p>
				<p class="text-xs text-[var(--color-text-tertiary)]">Manage webhook integrations</p>
			</div>
		</a>
	</div>
</div>
