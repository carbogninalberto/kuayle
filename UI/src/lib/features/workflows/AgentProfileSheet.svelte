<script lang="ts">
	import { untrack } from 'svelte';
	import * as Sheet from '$lib/components/ui/sheet/index.js';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import SelectField from '$lib/components/shared/SelectField.svelte';
	import {
		agentProfiles,
		type AgentProfile,
		type AgentProfileInput,
		type AgentProfileList,
		type AgentRole
	} from '$lib/api/workflows';
	import { agentModelCatalog } from '$lib/api/dev-machines';
	import type { AgentModelCatalog, ThinkingLevel } from '$lib/types/dev-machine';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/features/i18n/messages';
	import AgentLogo from '$lib/features/agents/AgentLogo.svelte';
	import { agentInfo } from '$lib/features/agents/agents';
	import { AGENT_EFFORT, effortChoice } from '$lib/features/agents/model-choice';
	import { allowedEfforts, catalogStatus, effortAfterModelChange } from '$lib/features/agents/model-catalog';
	import ModelCombobox from '$lib/features/agents/ModelCombobox.svelte';
	import { roleLabel } from './labels';
	import { modelFormat, profileSubmission, takesModel } from './profile-model';
	import ProfileSkills from './ProfileSkills.svelte';
	import AiMarkdownEditor from './AiMarkdownEditor.svelte';
	import InfoHint from '$lib/components/shared/InfoHint.svelte';

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

	const editable = $derived(list.can_manage);
	const bytes = $derived(new TextEncoder().encode(draft.instructions).length);
	const tooLong = $derived(bytes > MAX_INSTRUCTIONS);
	const harnessOptions = $derived.by(() => {
		const ids = [...list.harnesses];
		if (draft.harness && !ids.includes(draft.harness)) ids.push(draft.harness);
		return ids.map((id) => ({ value: id, label: agentInfo(id).name }));
	});
	// The server says which coding agents take a model and an effort; for the
	// others both fields are off and nothing left in them is sent.
	const modelAllowed = $derived(takesModel(list, draft.harness));
	// It also says how an agent writes its model names when not a bare name.
	const format = $derived(modelFormat(list, draft.harness));

	// The models the server suggests for the chosen coding agent. The field
	// stays free text: without a list (loading, or the server could not read
	// it) any model can still be typed, and the server validates it on save.
	let catalog = $state<AgentModelCatalog | null>(null),
		catalogLoading = $state(false),
		catalogFailed = $state(false);
	let catalogRequest = 0;
	async function loadCatalog(harness: string, refresh = false) {
		const request = ++catalogRequest;
		catalogLoading = true;
		catalogFailed = false;
		if (!refresh) catalog = null;
		try {
			const loaded = await agentModelCatalog(slug, harness, refresh);
			if (request === catalogRequest) catalog = loaded;
		} catch {
			if (request !== catalogRequest) return;
			// A failed refresh keeps the list it had, now known to be out of date.
			if (catalog) catalog = { ...catalog, stale: true };
			else catalogFailed = true;
		} finally {
			if (request === catalogRequest) catalogLoading = false;
		}
	}
	// Changing the coding agent loads its suggestions.
	$effect(() => {
		const harness = draft.harness;
		const wanted = editable && modelAllowed && !!harness;
		untrack(() => {
			if (wanted) void loadCatalog(harness);
			else {
				catalogRequest++;
				catalog = null;
				catalogLoading = catalogFailed = false;
			}
		});
	});
	const status = $derived(catalogStatus(catalog, catalogLoading, catalogFailed));
	// The effort a model change dropped, to tell the user.
	let effortReset = $state<{ model: string; effort: ThinkingLevel } | null>(null);
	function chooseModel(model: string) {
		const next = effortAfterModelChange(catalog?.models, model, draft.effort);
		draft.model = model;
		draft.effort = next.effort;
		effortReset = next.reset ? { model, effort: next.reset as ThinkingLevel } : null;
	}
	// Only the levels the chosen model allows; every level for a typed model.
	// An effort saved earlier stays visible until the user changes it.
	const effortLevels = $derived.by(() => {
		const levels = allowedEfforts(catalog?.models, draft.model, LEVELS);
		const current = draft.effort as ThinkingLevel;
		return current && LEVELS.includes(current) && !levels.includes(current) ? [...levels, current] : levels;
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
			const input = profileSubmission(draft, list);
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
					<div class="flex items-center gap-1">
						<h3 id="profile-agent" class="text-sm font-medium">{m['profiles.section_agent']()}</h3>
						<InfoHint text={m['profiles.dialog_desc']()} />
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
							<ModelCombobox
								id="profile-model"
								value={modelAllowed ? draft.model : ''}
								models={catalog?.models ?? []}
								{status}
								placeholder={m['profiles.model_placeholder']()}
								disabled={!editable || !modelAllowed}
								aria-describedby={modelAllowed ? (format ? 'profile-model-format' : undefined) : 'profile-model-hint'}
								onselect={chooseModel}
								onrefresh={() => void loadCatalog(draft.harness, true)}
							/>
						</div>
						<div class="space-y-1.5">
							<Label for="profile-effort">{m['profiles.effort']()}</Label>
							<SelectField
								id="profile-effort"
								disabled={!editable || !modelAllowed}
								aria-describedby={modelAllowed ? undefined : 'profile-model-hint'}
								bind:value={
									() => effortChoice(modelAllowed ? draft.effort : ''),
									(value: string) => {
										draft.effort = value === AGENT_EFFORT ? '' : value;
										effortReset = null;
									}
								}
								options={[
									{ value: AGENT_EFFORT, label: m['settings.agents.effort_agent']() },
									...effortLevels.map((level) => ({ value: level, label: m[`settings.agents.thinking_${level}`]() }))
								]}
							/>
						</div>
						{#if modelAllowed && effortReset}
							<p role="status" class="text-xs text-muted-foreground sm:col-span-2" data-testid="profile-effort-reset">
								{m['profiles.effort_reset']({
									model: effortReset.model,
									effort: m[`settings.agents.thinking_${effortReset.effort}`]()
								})}
							</p>
						{/if}
						{#if !modelAllowed}
							<p id="profile-model-hint" class="text-xs text-muted-foreground sm:col-span-2">
								{m['profiles.model_unsupported']({ agent: agentInfo(draft.harness).name })}
							</p>
						{:else if format}
							<p id="profile-model-format" class="text-xs text-muted-foreground sm:col-span-2">
								{m['profiles.model_format']({ format: format.format, example: format.example })}
							</p>
						{/if}
					</div>
				</section>

				<section class="space-y-3" aria-labelledby="profile-instructions-title">
					<div class="flex items-center gap-1">
						<h3 id="profile-instructions-title" class="text-sm font-medium">{m['profiles.instructions']()}</h3>
						<InfoHint text={m['profiles.instructions_help']()} />
					</div>
					<AiMarkdownEditor
						bind:value={draft.instructions}
						{slug}
						kind="instructions"
						{editable}
						label={m['profiles.instructions']()}
						placeholder={m['profiles.instructions_placeholder']()}
						class="min-h-[22rem]"
						testid="agent-profile-instructions"
					/>
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
