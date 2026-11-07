<script lang="ts">
	import { onMount } from 'svelte';
	import { Archive, Pencil, Plus } from '@lucide/svelte';
	import * as AlertDialog from '$lib/components/ui/alert-dialog/index.js';
	import * as Dialog from '$lib/components/ui/dialog/index.js';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import { Textarea } from '$lib/components/ui/textarea/index.js';
	import SelectField from '$lib/components/shared/SelectField.svelte';
	import {
		agentProfiles,
		type AgentProfile,
		type AgentProfileInput,
		type AgentProfileList,
		type AgentRole
	} from '$lib/api/workflows';
	import type { ThinkingLevel } from '$lib/types/dev-machine';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/paraglide/messages.js';
	import AgentLogo from '$lib/features/agents/AgentLogo.svelte';
	import { agentInfo } from '$lib/features/agents/agents';
	import { AGENT_EFFORT, effortChoice } from '$lib/features/agents/model-choice';
	import { roleLabel } from './labels';

	// Agent profiles (KUA-95): reusable agents that workflow steps run. Members
	// read them; owners and admins create, edit and archive them (the server
	// enforces it). A profile carries no permissions: every step still runs
	// under the workspace policy.
	let { slug }: { slug: string } = $props();

	const LEVELS: ThinkingLevel[] = ['off', 'low', 'medium', 'high', 'xhigh', 'max'];

	let list = $state<AgentProfileList | null>(null),
		loading = $state(true),
		failed = $state(false),
		saving = $state(false),
		editing = $state<AgentProfile | 'new' | null>(null),
		archiving = $state<AgentProfile | null>(null),
		draft = $state<AgentProfileInput>(blank());

	const profiles = $derived((list?.profiles ?? []).filter((p) => !p.archived_at));
	const canManage = $derived(list?.can_manage ?? false);

	function blank(): AgentProfileInput {
		return { name: '', role: 'implementer', harness: '', model: '', effort: '', instructions: '' };
	}

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
	onMount(load);

	function openEditor(profile: AgentProfile | 'new') {
		draft =
			profile === 'new'
				? { ...blank(), harness: list?.harnesses[0] ?? '' }
				: {
						name: profile.name,
						role: profile.role,
						harness: profile.harness,
						model: profile.model,
						effort: profile.effort,
						instructions: profile.instructions
					};
		editing = profile;
	}

	async function save(event: SubmitEvent) {
		event.preventDefault();
		if (!editing || saving || !canManage) return;
		saving = true;
		try {
			const input = { ...draft, name: draft.name.trim(), model: draft.model.trim() };
			if (editing === 'new') await agentProfiles.create(slug, input);
			else await agentProfiles.update(slug, editing.id, input);
			appToast.success(m['profiles.saved']());
			editing = null;
			await load();
		} catch (error) {
			appToast.apiError(error, m['profiles.failed_save']());
		} finally {
			saving = false;
		}
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
			appToast.apiError(error, m['profiles.failed_save']());
		} finally {
			saving = false;
		}
	}

	const effortLabel = (effort: string) =>
		effort ? m[`settings.agents.thinking_${effort as ThinkingLevel}`]?.() ?? effort : m['settings.agents.effort_agent']();
	// Harnesses offered in the dialog; an edited profile keeps its own even if
	// it can no longer run on its own.
	const harnessOptions = $derived.by(() => {
		const ids = [...(list?.harnesses ?? [])];
		if (draft.harness && !ids.includes(draft.harness)) ids.push(draft.harness);
		return ids.map((id) => ({ value: id, label: agentInfo(id).name }));
	});
</script>

<section aria-labelledby="agent-profiles-title" class="space-y-4 rounded-xl border bg-card p-5" data-testid="agent-profiles">
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
							{agentInfo(profile.harness).name} · <span class="font-mono"
								>{profile.model || m['profiles.model_placeholder']()}</span
							>
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
		<p class="text-xs text-muted-foreground">{m['profiles.coming_soon']()}</p>
	{/if}
</section>

{#if editing}
	<Dialog.Root
		open
		onOpenChange={(open) => {
			if (!open) editing = null;
		}}
	>
		<Dialog.Content class="max-h-[90vh] overflow-y-auto sm:max-w-xl">
			<form onsubmit={save} class="space-y-4" data-testid="agent-profile-form">
				<Dialog.Header>
					<Dialog.Title
						>{editing === 'new' ? m['profiles.dialog_title_new']() : m['profiles.dialog_title_edit']()}</Dialog.Title
					>
					<Dialog.Description>{m['profiles.dialog_desc']()}</Dialog.Description>
				</Dialog.Header>
				<div class="space-y-1.5">
					<Label for="profile-name">{m['profiles.name']()}</Label>
					<Input id="profile-name" bind:value={draft.name} maxlength={80} required autocomplete="off" />
				</div>
				<div class="grid gap-3 sm:grid-cols-2">
					<div class="space-y-1.5">
						<Label for="profile-role">{m['profiles.role']()}</Label>
						<SelectField
							id="profile-role"
							bind:value={() => draft.role, (value: string) => (draft.role = value as AgentRole)}
							options={(list?.roles ?? []).map((role) => ({ value: role, label: roleLabel(role) }))}
						/>
					</div>
					<div class="space-y-1.5">
						<Label for="profile-harness">{m['profiles.harness']()}</Label>
						<SelectField id="profile-harness" bind:value={draft.harness} options={harnessOptions} />
					</div>
					<div class="space-y-1.5">
						<Label for="profile-model">{m['profiles.model']()}</Label>
						<Input
							id="profile-model"
							bind:value={draft.model}
							maxlength={128}
							autocomplete="off"
							class="font-mono"
							placeholder={m['profiles.model_placeholder']()}
						/>
					</div>
					<div class="space-y-1.5">
						<Label for="profile-effort">{m['profiles.effort']()}</Label>
						<SelectField
							id="profile-effort"
							bind:value={
								() => effortChoice(draft.effort),
								(value: string) => (draft.effort = value === AGENT_EFFORT ? '' : value)
							}
							options={[
								{ value: AGENT_EFFORT, label: m['settings.agents.effort_agent']() },
								...LEVELS.map((level) => ({ value: level, label: m[`settings.agents.thinking_${level}`]() }))
							]}
						/>
					</div>
				</div>
				<div class="space-y-1.5">
					<Label for="profile-instructions">{m['profiles.instructions']()}</Label>
					<Textarea
						id="profile-instructions"
						bind:value={draft.instructions}
						rows={6}
						placeholder={m['profiles.instructions_placeholder']()}
					/>
				</div>
				<p class="text-xs text-muted-foreground">{m['profiles.coming_soon']()}</p>
				<Dialog.Footer>
					<Button type="button" variant="outline" onclick={() => (editing = null)}>{m['common.cancel']()}</Button>
					<Button type="submit" disabled={saving || !draft.name.trim() || !draft.harness}
						>{saving ? m['settings.saving']() : m['common.save']()}</Button
					>
				</Dialog.Footer>
			</form>
		</Dialog.Content>
	</Dialog.Root>
{/if}

<AlertDialog.Root open={!!archiving} onOpenChange={(open) => !open && (archiving = null)}>
	<AlertDialog.Content>
		<AlertDialog.Header>
			<AlertDialog.Title>{m['profiles.archive_title']()}</AlertDialog.Title>
			<AlertDialog.Description>{m['profiles.archive_confirm']({ name: archiving?.name ?? '' })}</AlertDialog.Description>
		</AlertDialog.Header>
		<AlertDialog.Footer>
			<AlertDialog.Cancel>{m['common.cancel']()}</AlertDialog.Cancel>
			<AlertDialog.Action variant="destructive" disabled={saving} onclick={archive}
				>{m['profiles.archive']()}</AlertDialog.Action
			>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>
