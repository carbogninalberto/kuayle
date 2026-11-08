<script lang="ts">
	import { untrack } from 'svelte';
	import * as Sheet from '$lib/components/ui/sheet/index.js';
	import * as Tabs from '$lib/components/ui/tabs/index.js';
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
	import Markdown from '$lib/features/assistant/Markdown.svelte';
	import { roleLabel } from './labels';
	import ProfileSkills from './ProfileSkills.svelte';

	// One agent profile in a side panel: who the agent is (role, coding agent,
	// model, effort) and its Markdown instructions, written with a preview.
	// Members see it read-only; the server enforces who may save.
	let {
		slug,
		list,
		profile,
		onclose,
		onsaved
	}: {
		slug: string;
		list: AgentProfileList;
		/** The profile to edit, or "new". */
		profile: AgentProfile | 'new';
		onclose: () => void;
		onsaved: () => void;
	} = $props();

	const LEVELS: ThinkingLevel[] = ['off', 'low', 'medium', 'high', 'xhigh', 'max'];
	const MAX_INSTRUCTIONS = 64 * 1024;

	let draft = $state<AgentProfileInput>(
		untrack(() =>
			profile === 'new'
				? {
						name: '',
						role: 'implementer',
						harness: list.harnesses[0] ?? '',
						model: '',
						effort: '',
						instructions: '',
						skill_ids: []
					}
				: {
						name: profile.name,
						role: profile.role,
						harness: profile.harness,
						model: profile.model,
						effort: profile.effort,
						instructions: profile.instructions,
						skill_ids: [...(profile.skill_ids ?? [])]
					}
		)
	);
	let saving = $state(false);
	let view = $state<'write' | 'preview'>('write');

	const editable = $derived(list.can_manage);
	const bytes = $derived(new TextEncoder().encode(draft.instructions).length);
	const tooLong = $derived(bytes > MAX_INSTRUCTIONS);
	const harnessOptions = $derived.by(() => {
		const ids = [...list.harnesses];
		if (draft.harness && !ids.includes(draft.harness)) ids.push(draft.harness);
		return ids.map((id) => ({ value: id, label: agentInfo(id).name }));
	});
	const sizeLabel = (n: number) => (n < 1024 ? `${n} B` : `${(n / 1024).toFixed(1)} KB`);

	// Refusals the server explains with a code get a translated message.
	function profileError(error: unknown) {
		const code = (error as { error?: { code?: string } }).error?.code;
		if (code === 'PROFILE_ROLE_IN_USE') appToast.error(m['profiles.role_in_use']());
		else if (code === 'PROFILE_IN_USE') appToast.error(m['profiles.in_use']());
		else appToast.apiError(error, m['profiles.failed_save']());
	}

	async function save(event: SubmitEvent) {
		event.preventDefault();
		if (saving || !editable || tooLong) return;
		saving = true;
		try {
			const input = { ...draft, name: draft.name.trim(), model: draft.model.trim() };
			if (profile === 'new') await agentProfiles.create(slug, input);
			else await agentProfiles.update(slug, profile.id, input);
			appToast.success(m['profiles.saved']());
			onsaved();
		} catch (error) {
			profileError(error);
		} finally {
			saving = false;
		}
	}
</script>

<Sheet.Root open onOpenChange={(open) => !open && onclose()}>
	<Sheet.Content
		side="right"
		class="w-full gap-0 p-0 data-[side=right]:w-full data-[side=right]:sm:max-w-2xl"
		data-testid="agent-profile-sheet"
	>
		<form onsubmit={save} class="flex h-full min-h-0 flex-col" data-testid="agent-profile-form">
			<Sheet.Header class="border-b px-6 py-4">
				<div class="flex items-center gap-3 pr-8">
					<AgentLogo agent={draft.harness} size="sm" />
					<div class="min-w-0">
						<Sheet.Title class="flex items-center gap-2 truncate">
							{profile === 'new' ? m['profiles.dialog_title_new']() : draft.name || profile.name}
							{#if profile !== 'new' && profile.builtin_key}<Badge variant="secondary">{m['profiles.builtin']()}</Badge
								>{/if}
						</Sheet.Title>
						<Sheet.Description class="text-xs"
							>{roleLabel(draft.role)} · {agentInfo(draft.harness).name}</Sheet.Description
						>
					</div>
				</div>
			</Sheet.Header>

			<div class="min-h-0 flex-1 space-y-8 overflow-y-auto px-6 py-5">
				<section class="space-y-4" aria-labelledby="profile-agent">
					<div>
						<h3 id="profile-agent" class="text-sm font-medium">{m['profiles.section_agent']()}</h3>
						<p class="mt-0.5 text-xs text-muted-foreground">{m['profiles.dialog_desc']()}</p>
					</div>
					<div class="space-y-1.5">
						<Label for="profile-name">{m['profiles.name']()}</Label>
						<Input
							id="profile-name"
							bind:value={draft.name}
							maxlength={80}
							required
							autocomplete="off"
							disabled={!editable}
						/>
					</div>
					<div class="grid gap-3 sm:grid-cols-2">
						<div class="space-y-1.5">
							<Label for="profile-role">{m['profiles.role']()}</Label>
							<SelectField
								id="profile-role"
								disabled={!editable}
								bind:value={() => draft.role, (value: string) => (draft.role = value as AgentRole)}
								options={list.roles.map((role) => ({ value: role, label: roleLabel(role) }))}
							/>
						</div>
						<div class="space-y-1.5">
							<Label for="profile-harness">{m['profiles.harness']()}</Label>
							<SelectField
								id="profile-harness"
								disabled={!editable}
								bind:value={draft.harness}
								options={harnessOptions}
							/>
						</div>
						<div class="space-y-1.5">
							<Label for="profile-model">{m['profiles.model']()}</Label>
							<Input
								id="profile-model"
								bind:value={draft.model}
								maxlength={128}
								autocomplete="off"
								class="font-mono"
								disabled={!editable}
								placeholder={m['profiles.model_placeholder']()}
							/>
						</div>
						<div class="space-y-1.5">
							<Label for="profile-effort">{m['profiles.effort']()}</Label>
							<SelectField
								id="profile-effort"
								disabled={!editable}
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
				</section>

				<section class="space-y-3" aria-labelledby="profile-instructions-title">
					<div class="flex items-end justify-between gap-3">
						<div>
							<h3 id="profile-instructions-title" class="text-sm font-medium">{m['profiles.instructions']()}</h3>
							<p class="mt-0.5 text-xs text-muted-foreground">{m['profiles.instructions_help']()}</p>
						</div>
						<Tabs.Root bind:value={view}>
							<Tabs.List>
								<Tabs.Trigger value="write">{m['profiles.write']()}</Tabs.Trigger>
								<Tabs.Trigger value="preview">{m['profiles.preview']()}</Tabs.Trigger>
							</Tabs.List>
						</Tabs.Root>
					</div>
					{#if view === 'write'}
						<Textarea
							id="profile-instructions"
							aria-labelledby="profile-instructions-title"
							bind:value={draft.instructions}
							disabled={!editable}
							placeholder={m['profiles.instructions_placeholder']()}
							class="min-h-[22rem] resize-y font-mono text-[13px] leading-relaxed"
						/>
					{:else}
						<div class="min-h-[22rem] rounded-md border px-4 py-3" data-testid="agent-profile-preview">
							{#if draft.instructions.trim()}<Markdown source={draft.instructions} class="text-sm" />
							{:else}<p class="text-sm text-muted-foreground">{m['profiles.instructions_empty']()}</p>{/if}
						</div>
					{/if}
					<p class="text-right text-xs {tooLong ? 'text-destructive' : 'text-muted-foreground'}">
						{m['profiles.instructions_size']({ used: sizeLabel(bytes) })}
					</p>
				</section>

				<ProfileSkills {slug} {editable} bind:selected={draft.skill_ids!} />
			</div>

			<Sheet.Footer class="flex-row justify-end gap-2 border-t px-6 py-3">
				<Button type="button" variant="outline" onclick={onclose}>{m['common.cancel']()}</Button>
				{#if editable}<Button type="submit" disabled={saving || tooLong || !draft.name.trim() || !draft.harness}
						>{saving ? m['settings.saving']() : m['common.save']()}</Button
					>{/if}
			</Sheet.Footer>
		</form>
	</Sheet.Content>
</Sheet.Root>
