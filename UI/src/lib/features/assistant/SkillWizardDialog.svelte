<script lang="ts">
	import { untrack } from 'svelte';
	import { FileUp, LoaderCircle, Sparkles } from '@lucide/svelte';
	import * as Dialog from '$lib/components/ui/dialog/index.js';
	import * as Tooltip from '$lib/components/ui/tooltip/index.js';
	import { Alert, AlertAction, AlertDescription } from '$lib/components/ui/alert/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import { Textarea } from '$lib/components/ui/textarea/index.js';
	import SelectField from '$lib/components/shared/SelectField.svelte';
	import InfoHint from '$lib/components/shared/InfoHint.svelte';
	import WizardSteps from '$lib/components/shared/WizardSteps.svelte';
	import { aiWrite } from '$lib/api/ai-write';
	import { assistantSkills, type AssistantSkill, type AssistantSkillStatus } from '$lib/api/assistant-skills';
	import type { Team } from '$lib/types/team';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/features/i18n/messages';
	import AiMarkdownEditor from '$lib/features/workflows/AiMarkdownEditor.svelte';
	import { parseSkill, type SkillProblem } from '$lib/features/workflows/skill-md';
	import Markdown from './Markdown.svelte';
	import { importedSkill, skillInstruction, skillTemplate } from './creation-wizards';

	// Creates a workspace skill in three steps: describe it, draft the SKILL.md
	// (with the workspace's AI model, or from a template of the answers when AI
	// is off), then review the name, scope and status and save. Nothing is
	// saved before the last step; the API validates the save.
	let {
		open = $bindable(false),
		slug,
		teams = [],
		onsaved
	}: {
		open?: boolean;
		slug: string;
		/** Teams the user can see, for a team scope. */
		teams?: Team[];
		onsaved: (skill: AssistantSkill) => void;
	} = $props();

	const workspaceScope = 'workspace';
	const formId = 'skill-wizard-form';
	let step = $state(0);
	let purpose = $state('');
	let whenToUse = $state('');
	let examples = $state('');
	let source = $state('');
	let ai = $state<'idle' | 'working' | 'off'>('idle');
	let teamId = $state(workspaceScope);
	let name = $state('');
	let description = $state('');
	let status = $state<AssistantSkillStatus>('active');
	let reviewedFrom = $state('');
	let saving = $state(false);
	let file = $state<HTMLInputElement | null>(null);

	$effect(() => {
		if (!open) return;
		untrack(() => {
			step = 0;
			purpose = whenToUse = examples = source = name = description = reviewedFrom = '';
			ai = 'idle';
			teamId = workspaceScope;
			status = 'active';
		});
	});

	const answers = $derived({ purpose, whenToUse, examples });
	const parsed = $derived(parseSkill(source));
	const problems: Record<SkillProblem, () => string> = {
		front_matter: m['profiles.skill_problem_front_matter'],
		name: m['profiles.skill_problem_name'],
		description: m['profiles.skill_problem_description'],
		body: m['profiles.skill_problem_body']
	};
	const steps = [m['wizard.step_describe'](), m['wizard.step_draft'](), m['wizard.step_review']()];
	const scopes = $derived([
		{ value: workspaceScope, label: m['assistant_skills.scope_workspace']() },
		...teams.map((team) => ({ value: team.id, label: team.name }))
	]);
	const statuses = [
		{ value: 'active' as const, label: m['assistant_skills.status_active']() },
		{ value: 'draft' as const, label: m['assistant_skills.status_draft']() }
	];
	const canSave = $derived(name.trim() !== '' && description.trim() !== '' && parsed.body.trim() !== '');

	function toDraft() {
		if (!purpose.trim()) return;
		if (!source.trim()) source = skillTemplate(answers);
		step = 1;
	}

	function toReview() {
		if (parsed.problems.length) return;
		// Edits made on the review step survive a look back at an unchanged draft.
		if (reviewedFrom !== source) {
			name = parsed.name;
			description = parsed.description;
			reviewedFrom = source;
		}
		step = 2;
	}

	async function generate() {
		if (ai === 'working') return;
		ai = 'working';
		const template = skillTemplate(answers);
		try {
			const { text } = await aiWrite(slug, {
				kind: 'skill',
				mode: 'generate',
				instruction: skillInstruction(answers),
				source: template
			});
			source = text + '\n';
			ai = 'idle';
			appToast.success(m['ai_editor.done']());
		} catch (error) {
			const code = (error as { error?: { code?: string } }).error?.code;
			if (code === 'AI_NOT_CONFIGURED') {
				ai = 'off';
				if (!source.trim()) source = template;
			} else {
				ai = 'idle';
				appToast.apiError(error, m['ai_editor.failed']());
			}
		}
	}

	async function importFile(event: Event) {
		const input = event.currentTarget as HTMLInputElement;
		const picked = input.files?.[0];
		input.value = '';
		if (!picked) return;
		const text = await picked.text();
		const skill = importedSkill(text);
		if (skill) {
			source = skill;
			purpose ||= parseSkill(skill).description || picked.name;
			step = 1;
		} else examples = text.slice(0, 20000);
	}

	async function save() {
		if (!canSave || saving) return;
		saving = true;
		try {
			const saved = await assistantSkills.create(slug, {
				team_id: teamId === workspaceScope ? null : teamId,
				name: name.trim(),
				description: description.trim(),
				body: parsed.body,
				status
			});
			appToast.success(m['assistant_skills.created_toast']({ name: saved.name }));
			open = false;
			onsaved(saved);
		} catch (err) {
			appToast.apiError(err, m['assistant_skills.failed_save']());
		} finally {
			saving = false;
		}
	}
</script>

<Dialog.Root bind:open>
	<Dialog.Content class="max-h-[90vh] overflow-y-auto sm:max-w-2xl">
		<Dialog.Header>
			<Dialog.Title>{m['assistant_skills.new']()}</Dialog.Title>
			<Dialog.Description>{m['skill_wizard.desc']()}</Dialog.Description>
		</Dialog.Header>
		<WizardSteps {steps} current={step} label={m['wizard.progress']()} />

		<!-- The draft step holds the AI editor, whose Ask AI bar is a form of its
		     own, so only the other steps are a form; the footer submits them. -->
		{#if step === 1}
			<div class="grid min-w-0 gap-4">
				<div class="flex flex-wrap items-center gap-2">
					<Button type="button" size="sm" disabled={ai === 'working'} onclick={generate}
						>{#if ai === 'working'}<LoaderCircle class="animate-spin" />{:else}<Sparkles />{/if}{m[
							'wizard.generate'
						]()}</Button
					>
					<InfoHint text={m['skill_wizard.draft_hint']()} />
				</div>
				{#if ai === 'off'}
					<Alert>
						<AlertDescription>{m['skill_wizard.ai_off']()}</AlertDescription>
						<AlertAction>
							<Button size="sm" variant="outline" href="/{slug}/settings/ai">{m['wizard.ai_settings']()}</Button>
						</AlertAction>
					</Alert>
				{/if}
				<AiMarkdownEditor
					bind:value={source}
					{slug}
					kind="skill"
					label={m['skill_wizard.draft_label']()}
					class="max-h-[45vh] min-h-64"
					testid="skill-wizard-editor"
				>
					{#snippet preview()}
						<div class="space-y-2">
							<div class="flex flex-wrap items-baseline gap-2 border-b pb-2">
								<code class="text-sm font-medium">{parsed.name || '—'}</code>
								<span class="text-xs text-muted-foreground">{parsed.description}</span>
							</div>
							<Markdown source={parsed.body} class="text-sm" />
						</div>
					{/snippet}
				</AiMarkdownEditor>
				{#if parsed.problems.length}
					<ul role="alert" class="list-inside list-disc text-xs text-destructive">
						{#each parsed.problems as problem (problem)}<li>{problems[problem]()}</li>{/each}
					</ul>
				{/if}
			</div>
		{:else}
			<form
				id={formId}
				class="grid min-w-0 gap-4"
				onsubmit={(event) => {
					event.preventDefault();
					if (step === 0) toDraft();
					else void save();
				}}
			>
				{#if step === 0}
					<div class="grid gap-1.5">
						<Label for="skill-purpose">{m['skill_wizard.purpose']()}</Label>
						<Textarea
							id="skill-purpose"
							bind:value={purpose}
							rows={2}
							maxlength={1000}
							required
							placeholder={m['skill_wizard.purpose_placeholder']()}
						/>
					</div>
					<div class="grid gap-1.5">
						<Label for="skill-when">{m['skill_wizard.when']()}</Label>
						<Input
							id="skill-when"
							bind:value={whenToUse}
							maxlength={500}
							autocomplete="off"
							placeholder={m['skill_wizard.when_placeholder']()}
						/>
					</div>
					<div class="grid gap-1.5">
						<div class="flex items-center gap-1">
							<Label for="skill-examples">{m['skill_wizard.examples']()}</Label>
							<InfoHint text={m['skill_wizard.examples_hint']()} />
							<input
								bind:this={file}
								type="file"
								accept=".md,text/markdown,text/plain"
								class="hidden"
								aria-label={m['skill_wizard.import']()}
								onchange={importFile}
							/>
							<Tooltip.Provider delayDuration={300}>
								<Tooltip.Root>
									<Tooltip.Trigger>
										{#snippet child({ props })}<Button
												{...props}
												type="button"
												size="icon-sm"
												variant="ghost"
												class="ml-auto"
												aria-label={m['skill_wizard.import']()}
												onclick={() => file?.click()}><FileUp /></Button
											>{/snippet}
									</Tooltip.Trigger>
									<Tooltip.Content>{m['skill_wizard.import']()}</Tooltip.Content>
								</Tooltip.Root>
							</Tooltip.Provider>
						</div>
						<Textarea
							id="skill-examples"
							bind:value={examples}
							rows={4}
							maxlength={20000}
							class="max-h-60 font-mono text-xs"
						/>
					</div>
				{:else}
					<div class="grid gap-3 sm:grid-cols-2">
						<div class="grid gap-1.5">
							<div class="flex items-center gap-1">
								<Label for="skill-scope">{m['assistant_skills.field_scope']()}</Label>
								<InfoHint text={m['assistant_skills.scope_hint']()} />
							</div>
							<SelectField id="skill-scope" bind:value={teamId} options={scopes} />
						</div>
						<div class="grid gap-1.5">
							<div class="flex items-center gap-1">
								<Label for="skill-status">{m['wizard.status']()}</Label>
								<InfoHint text={m['skill_wizard.status_hint']()} />
							</div>
							<SelectField id="skill-status" bind:value={status} options={statuses} />
						</div>
					</div>
					<div class="grid gap-1.5">
						<Label for="skill-name">{m['assistant_skills.field_name']()}</Label>
						<Input id="skill-name" bind:value={name} maxlength={100} required autocomplete="off" />
					</div>
					<div class="grid gap-1.5">
						<div class="flex items-center gap-1">
							<Label for="skill-description">{m['assistant_skills.field_description']()}</Label>
							<InfoHint text={m['assistant_skills.description_hint']()} />
						</div>
						<Input id="skill-description" bind:value={description} maxlength={500} required autocomplete="off" />
					</div>
					<section aria-label={m['assistant_skills.preview']()} class="grid gap-1.5">
						<span class="text-sm font-medium">{m['assistant_skills.preview']()}</span>
						<div class="max-h-[35vh] overflow-y-auto rounded-lg border px-3 py-2">
							<Markdown source={parsed.body} class="text-sm" />
						</div>
					</section>
				{/if}
			</form>
		{/if}

		<Dialog.Footer class="flex-wrap gap-2">
			{#if step === 0}
				<Button type="button" variant="outline" onclick={() => (open = false)}>{m['assistant_skills.cancel']()}</Button>
			{:else}
				<Button type="button" variant="outline" onclick={() => (step -= 1)}>{m['wizard.back']()}</Button>
			{/if}
			{#if step === 0}
				<Button type="submit" form={formId} disabled={!purpose.trim()}>{m['wizard.next']()}</Button>
			{:else if step === 1}
				<Button type="button" disabled={parsed.problems.length > 0 || ai === 'working'} onclick={toReview}
					>{m['wizard.next']()}</Button
				>
			{:else}
				<Button type="submit" form={formId} disabled={!canSave || saving}>{m['assistant_skills.create']()}</Button>
			{/if}
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
