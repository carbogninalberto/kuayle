<script lang="ts">
	import { untrack } from 'svelte';
	import * as Dialog from '$lib/components/ui/dialog/index.js';
	import * as Tabs from '$lib/components/ui/tabs/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import { Textarea } from '$lib/components/ui/textarea/index.js';
	import SelectField from '$lib/components/shared/SelectField.svelte';
	import {
		assistantSkills,
		type AssistantSkill,
		type AssistantSkillContent,
		type AssistantSkillStatus
	} from '$lib/api/assistant-skills';
	import type { Team } from '$lib/types/team';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/paraglide/messages.js';
	import Markdown from './Markdown.svelte';

	// Creates a skill, or saves an edit of `skill` as its next version. The body
	// is plain Markdown in a Textarea; the preview uses the sanitizing renderer.
	let {
		open = $bindable(false),
		slug,
		teams = [],
		skill = null,
		initial = null,
		onsaved
	}: {
		open?: boolean;
		slug: string;
		/** Teams the user can see; only used when creating. */
		teams?: Team[];
		skill?: AssistantSkill | null;
		/** The content to start editing from (a pending draft, or the skill's own). */
		initial?: Pick<AssistantSkillContent, 'name' | 'description' | 'body'> | null;
		onsaved: (skill: AssistantSkill) => void;
	} = $props();

	// SelectField treats an empty value as its placeholder, so workspace scope has a name.
	const workspaceScope = 'workspace';
	let teamId = $state(workspaceScope);
	let name = $state('');
	let description = $state('');
	let body = $state('');
	let mode = $state('write');
	let saving = $state(false);

	// Reset the form each time the dialog opens, not when `initial` changes.
	$effect(() => {
		if (!open) return;
		untrack(() => {
			teamId = workspaceScope;
			name = initial?.name ?? '';
			description = initial?.description ?? '';
			body = initial?.body ?? '';
			mode = 'write';
		});
	});

	const scopes = $derived([
		{ value: workspaceScope, label: m['assistant_skills.scope_workspace']() },
		...teams.map((team) => ({ value: team.id, label: team.name }))
	]);
	const valid = $derived(name.trim() !== '' && description.trim() !== '' && body.trim() !== '');

	async function save(status: AssistantSkillStatus | undefined) {
		if (!valid || saving) return;
		saving = true;
		const content = { name: name.trim(), description: description.trim(), body };
		try {
			const saved = skill
				? await assistantSkills.update(slug, skill.id, status ? { ...content, status } : content)
				: await assistantSkills.create(slug, {
						team_id: teamId === workspaceScope ? null : teamId,
						...content,
						status: status ?? 'active'
					});
			appToast.success(
				skill
					? m['assistant_skills.saved_toast']({ name: saved.name })
					: m['assistant_skills.created_toast']({ name: saved.name })
			);
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
	<Dialog.Content class="max-h-[85vh] overflow-y-auto sm:max-w-2xl">
		<Dialog.Header>
			<Dialog.Title
				>{skill ? m['assistant_skills.edit_title']({ name: skill.name }) : m['assistant_skills.new']()}</Dialog.Title
			>
			<Dialog.Description>{m['assistant_skills.editor_desc']()}</Dialog.Description>
		</Dialog.Header>
		<form
			class="grid gap-4"
			onsubmit={(event) => {
				event.preventDefault();
				void save(skill?.status === 'draft' ? undefined : 'active');
			}}
		>
			{#if !skill}
				<div class="grid gap-1.5">
					<Label for="skill-scope">{m['assistant_skills.field_scope']()}</Label>
					<SelectField id="skill-scope" bind:value={teamId} options={scopes} />
					<p class="text-xs text-muted-foreground">{m['assistant_skills.scope_hint']()}</p>
				</div>
			{/if}
			<div class="grid gap-1.5">
				<Label for="skill-name">{m['assistant_skills.field_name']()}</Label>
				<Input id="skill-name" bind:value={name} maxlength={100} required autocomplete="off" />
			</div>
			<div class="grid gap-1.5">
				<Label for="skill-description">{m['assistant_skills.field_description']()}</Label>
				<Input
					id="skill-description"
					bind:value={description}
					maxlength={500}
					required
					autocomplete="off"
					aria-describedby="skill-description-hint"
				/>
				<p id="skill-description-hint" class="text-xs text-muted-foreground">
					{m['assistant_skills.description_hint']()}
				</p>
			</div>
			<Tabs.Root bind:value={mode} class="gap-2">
				<div class="flex flex-wrap items-center justify-between gap-2">
					<Label for="skill-body">{m['assistant_skills.field_body']()}</Label>
					<Tabs.List>
						<Tabs.Trigger value="write">{m['assistant_skills.write']()}</Tabs.Trigger>
						<Tabs.Trigger value="preview">{m['assistant_skills.preview']()}</Tabs.Trigger>
					</Tabs.List>
				</div>
				<Tabs.Content value="write">
					<Textarea
						id="skill-body"
						bind:value={body}
						required
						rows={12}
						class="max-h-[50vh] min-h-48 font-mono text-xs"
					/>
				</Tabs.Content>
				<Tabs.Content value="preview" class="max-h-[50vh] min-h-48 overflow-y-auto rounded-lg border px-3 py-2">
					{#if body.trim()}<Markdown source={body} class="text-sm" />{:else}<p class="text-sm text-muted-foreground">
							{m['assistant_skills.preview_empty']()}
						</p>{/if}
				</Tabs.Content>
			</Tabs.Root>
			<Dialog.Footer class="flex-wrap gap-2">
				<Button type="button" variant="outline" onclick={() => (open = false)}>{m['assistant_skills.cancel']()}</Button>
				{#if skill?.status === 'draft'}
					<Button type="submit" disabled={!valid || saving}>{m['assistant_skills.save_draft_only']()}</Button>
				{:else}
					<Button type="button" variant="outline" disabled={!valid || saving} onclick={() => void save('draft')}
						>{m['assistant_skills.save_draft']()}</Button
					>
					<Button type="submit" disabled={!valid || saving}
						>{skill ? m['assistant_skills.save_apply']() : m['assistant_skills.create']()}</Button
					>
				{/if}
			</Dialog.Footer>
		</form>
	</Dialog.Content>
</Dialog.Root>
