<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { Archive, Pencil, Plus } from '@lucide/svelte';
	import * as AlertDialog from '$lib/components/ui/alert-dialog/index.js';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { agentProfiles, type AgentProfile, type AgentProfileList } from '$lib/api/workflows';
	import type { ThinkingLevel } from '$lib/types/dev-machine';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/features/i18n/messages';
	import AgentLogo from '$lib/features/agents/AgentLogo.svelte';
	import { agentInfo } from '$lib/features/agents/agents';
	import { roleLabel } from './labels';
	import AgentProfileSheet from './AgentProfileSheet.svelte';

	// Agent profiles (KUA-95): reusable agents that workflow steps run. Members
	// read them; owners and admins create, edit and archive them (the server
	// enforces it). A profile carries no permissions: every step still runs
	// under the workspace policy.
	let { slug }: { slug: string } = $props();

	let list = $state<AgentProfileList | null>(null),
		loading = $state(true),
		failed = $state(false),
		saving = $state(false),
		editing = $state<AgentProfile | 'new' | null>(null),
		archiving = $state<AgentProfile | null>(null);

	const profiles = $derived((list?.profiles ?? []).filter((p) => !p.archived_at));
	const canManage = $derived(list?.can_manage ?? false);

	async function load() {
		loading = true;
		failed = false;
		try {
			list = await agentProfiles.list(slug);
		} catch (error) {
			failed = true;
			appToast.apiError(error, m['profiles.failed_load']());
		} finally {
			loading = false;
		}
	}
	onMount(async () => {
		await load();
		const id = page.url.searchParams.get('profile');
		if (id) editing = id === 'new' ? 'new' : (profiles.find((p) => p.id === id) ?? null);
	});

	// The open profile is in the address (?profile=<id>), so it can be linked.
	function openEditor(profile: AgentProfile | 'new' | null) {
		editing = profile;
		const url = new URL(page.url);
		if (profile) url.searchParams.set('profile', profile === 'new' ? 'new' : profile.id);
		else url.searchParams.delete('profile');
		void goto(url, { replaceState: true, noScroll: true, keepFocus: true });
	}

	// Refusals the server explains with a code get a translated message.
	function profileError(error: unknown) {
		const code = (error as { error?: { code?: string } }).error?.code;
		if (code === 'PROFILE_ROLE_IN_USE') appToast.error(m['profiles.role_in_use']());
		else if (code === 'PROFILE_IN_USE') appToast.error(m['profiles.in_use']());
		else appToast.apiError(error, m['profiles.failed_save']());
	}

	async function archive() {
		if (!archiving || saving) return;
		saving = true;
		try {
			await agentProfiles.archive(slug, archiving.id);
			appToast.success(m['profiles.archived_done']());
			archiving = null;
			await load();
		} catch (error) {
			profileError(error);
		} finally {
			saving = false;
		}
	}

	const effortLabel = (effort: string) =>
		effort
			? (m[`settings.agents.thinking_${effort as ThinkingLevel}`]?.() ?? effort)
			: m['settings.agents.effort_agent']();
</script>

<section
	aria-labelledby="agent-profiles-title"
	class="space-y-4 rounded-xl border bg-card p-5"
	data-testid="agent-profiles"
>
	<div class="flex flex-wrap items-start justify-between gap-3">
		<div>
			<h2 id="agent-profiles-title" class="text-sm font-semibold">{m['profiles.title']()}</h2>
			<p class="text-xs text-muted-foreground">{m['profiles.desc']()}</p>
		</div>
		{#if canManage}<Button size="sm" onclick={() => openEditor('new')} data-testid="agent-profile-add"
				><Plus />{m['profiles.add']()}</Button
			>{/if}
	</div>
	{#if loading}<p role="status" class="text-xs text-muted-foreground">{m['common.loading']()}</p>
	{:else if failed}
		<div class="flex items-center justify-between gap-3">
			<p role="alert" class="text-sm text-destructive">{m['profiles.failed_load']()}</p>
			<Button size="sm" variant="outline" onclick={load}>{m['machines.refresh']()}</Button>
		</div>
	{:else}
		<ul class="space-y-2" role="list">
			{#each profiles as profile (profile.id)}
				<li class="flex flex-wrap items-center gap-3 rounded-lg border p-3" data-testid="agent-profile">
					<AgentLogo agent={profile.harness} size="sm" />
					<div class="min-w-0 flex-1">
						<div class="flex flex-wrap items-center gap-2 text-sm font-medium">
							<span class="truncate">{profile.name}</span>
							<Badge variant="outline">{roleLabel(profile.role)}</Badge>
							{#if profile.builtin_key}<Badge variant="secondary">{m['profiles.builtin']()}</Badge>{/if}
						</div>
						<p class="truncate text-xs text-muted-foreground">
							{agentInfo(profile.harness).name} ·
							<span class="font-mono">{profile.model || m['profiles.model_placeholder']()}</span>
							· {effortLabel(profile.effort)}
						</p>
					</div>
					{#if canManage}
						<div class="flex gap-1">
							<Button
								size="icon-sm"
								variant="ghost"
								aria-label={m['profiles.edit_named']({ name: profile.name })}
								onclick={() => openEditor(profile)}><Pencil /></Button
							>
							<Button
								size="icon-sm"
								variant="ghost"
								aria-label={m['profiles.archive_named']({ name: profile.name })}
								onclick={() => (archiving = profile)}><Archive /></Button
							>
						</div>
					{/if}
				</li>
			{/each}
		</ul>
	{/if}
</section>

{#if editing && list}
	<AgentProfileSheet
		{slug}
		{list}
		profile={editing}
		onclose={() => openEditor(null)}
		onsaved={async () => {
			openEditor(null);
			await load();
		}}
	/>
{/if}

<AlertDialog.Root open={!!archiving} onOpenChange={(open) => !open && (archiving = null)}>
	<AlertDialog.Content>
		<AlertDialog.Header>
			<AlertDialog.Title>{m['profiles.archive_title']()}</AlertDialog.Title>
			<AlertDialog.Description>{m['profiles.archive_confirm']({ name: archiving?.name ?? '' })}</AlertDialog.Description
			>
		</AlertDialog.Header>
		<AlertDialog.Footer>
			<AlertDialog.Cancel>{m['common.cancel']()}</AlertDialog.Cancel>
			<AlertDialog.Action variant="destructive" disabled={saving} onclick={archive}
				>{m['profiles.archive']()}</AlertDialog.Action
			>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>
