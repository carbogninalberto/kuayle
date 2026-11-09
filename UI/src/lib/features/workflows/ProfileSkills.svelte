<script lang="ts">
	import { onMount } from 'svelte';
	import { ChevronDown, FileUp, Plus, Save, X } from '@lucide/svelte';
	import * as Tooltip from '$lib/components/ui/tooltip/index.js';
	import AiMarkdownEditor from './AiMarkdownEditor.svelte';
	import InfoHint from '$lib/components/shared/InfoHint.svelte';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Checkbox } from '$lib/components/ui/checkbox/index.js';
	import { assistantSkills, type AssistantSkill } from '$lib/api/assistant-skills';
	import { appToast } from '$lib/features/toast/toast';
	import { cn } from '$lib/utils.js';
	import { m } from '$lib/paraglide/messages.js';
	import Markdown from '$lib/features/assistant/Markdown.svelte';
	import { SKILL_TEMPLATE, parseSkill, type SkillProblem } from './skill-md';

	// The skills an agent profile gets, chosen from the workspace skill
	// library: attached ones first, in order, then the rest. Any skill can be
	// previewed; a new one is written (or imported) as a standard SKILL.md,
	// saved to the library and attached in one step. Only active skills shared
	// with the whole workspace can be attached; the server checks it again.
	let { slug, selected = $bindable(), editable }: { slug: string; selected: string[]; editable: boolean } = $props();

	const MAX = 20;
	let skills = $state<AssistantSkill[]>([]),
		loading = $state(true),
		failed = $state(false),
		open = $state<string | null>(null),
		bodies = $state<Record<string, string>>({}),
		writing = $state(false),
		source = $state(SKILL_TEMPLATE),
		saving = $state(false),
		file = $state<HTMLInputElement | null>(null);

	async function load() {
		loading = true;
		failed = false;
		try {
			skills = (await assistantSkills.list(slug)).skills;
		} catch {
			failed = true;
		} finally {
			loading = false;
		}
	}
	onMount(load);

	const attachable = (s: AssistantSkill) => !s.team_id && s.status === 'active' && s.enabled;
	const ordered = $derived([
		...selected.map((id) => skills.find((s) => s.id === id)).filter((s): s is AssistantSkill => !!s),
		...skills.filter((s) => !selected.includes(s.id) && !s.team_id)
	]);

	function toggle(skill: AssistantSkill, on: boolean) {
		selected = on ? [...selected, skill.id] : selected.filter((id) => id !== skill.id);
	}

	async function togglePreview(skill: AssistantSkill) {
		open = open === skill.id ? null : skill.id;
		if (open && bodies[skill.id] === undefined) {
			try {
				bodies[skill.id] = (await assistantSkills.get(slug, skill.id)).body ?? '';
			} catch (error) {
				open = null;
				appToast.apiError(error, m['profiles.skills_failed_load']());
			}
		}
	}

	const parsed = $derived(parseSkill(source));
	const problems: Record<SkillProblem, () => string> = {
		front_matter: m['profiles.skill_problem_front_matter'],
		name: m['profiles.skill_problem_name'],
		description: m['profiles.skill_problem_description'],
		body: m['profiles.skill_problem_body']
	};

	async function importFile(event: Event) {
		const input = event.currentTarget as HTMLInputElement;
		const picked = input.files?.[0];
		input.value = '';
		if (!picked) return;
		source = await picked.text();
	}

	async function saveSkill() {
		if (saving || parsed.problems.length > 0) return;
		saving = true;
		try {
			const created = await assistantSkills.create(slug, {
				name: parsed.name,
				description: parsed.description,
				body: parsed.body,
				status: 'active',
				team_id: null
			});
			skills = [...skills, created];
			bodies[created.id] = parsed.body;
			if (selected.length < MAX) selected = [...selected, created.id];
			appToast.success(m['profiles.skill_saved']());
			writing = false;
			source = SKILL_TEMPLATE;
		} catch (error) {
			appToast.apiError(error, m['profiles.skills_failed_load']());
		} finally {
			saving = false;
		}
	}
</script>

<section class="space-y-3" aria-labelledby="profile-skills" data-testid="profile-skills">
	<div class="flex items-end justify-between gap-3">
		<div class="flex items-center gap-1">
			<h3 id="profile-skills" class="text-sm font-medium">
				{m['profiles.section_skills']()}
				<span class="ml-1 text-xs font-normal text-muted-foreground"
					>{m['profiles.skills_count']({ count: selected.length })}</span
				>
			</h3>
			<InfoHint text={m['profiles.skills_help']()} />
		</div>
		{#if editable && !writing}<Button size="sm" variant="outline" onclick={() => (writing = true)}
				><Plus />{m['profiles.skill_new']()}</Button
			>{/if}
	</div>

	{#if writing}
		<div class="space-y-2" data-testid="profile-skill-editor">
			<input bind:this={file} type="file" accept=".md,text/markdown,text/plain" class="hidden" onchange={importFile} />
			<AiMarkdownEditor
				bind:value={source}
				{slug}
				kind="skill"
				label={m['profiles.skill_new']()}
				testid="profile-skill-ai"
			>
				{#snippet actions()}
					<Tooltip.Root>
						<Tooltip.Trigger>
							{#snippet child({ props })}<Button
									{...props}
									size="icon-sm"
									variant="ghost"
									aria-label={m['profiles.skill_import']()}
									onclick={() => file?.click()}><FileUp /></Button
								>{/snippet}
						</Tooltip.Trigger>
						<Tooltip.Content>{m['profiles.skill_import']()}</Tooltip.Content>
					</Tooltip.Root>
					<Tooltip.Root>
						<Tooltip.Trigger>
							{#snippet child({ props })}<Button
									{...props}
									size="icon-sm"
									variant="ghost"
									aria-label={m['common.cancel']()}
									onclick={() => {
										writing = false;
										source = SKILL_TEMPLATE;
									}}><X /></Button
								>{/snippet}
						</Tooltip.Trigger>
						<Tooltip.Content>{m['common.cancel']()}</Tooltip.Content>
					</Tooltip.Root>
				{/snippet}
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
			<div class="flex items-start justify-between gap-3">
				{#if parsed.problems.length > 0}
					<ul class="space-y-0.5 text-xs text-destructive" role="list">
						{#each parsed.problems as problem (problem)}<li>{problems[problem]()}</li>{/each}
					</ul>
				{:else}
					<p class="text-xs text-muted-foreground">{m['profiles.skill_format_help']()}</p>
				{/if}
				<Button size="sm" class="shrink-0" onclick={saveSkill} disabled={saving || parsed.problems.length > 0}
					><Save />{m['profiles.skill_save']()}</Button
				>
			</div>
		</div>
	{/if}

	{#if loading}<p role="status" class="text-xs text-muted-foreground">{m['common.loading']()}</p>
	{:else if failed}
		<div class="flex items-center justify-between gap-3">
			<p role="alert" class="text-sm text-destructive">{m['profiles.skills_failed_load']()}</p>
			<Button size="sm" variant="outline" onclick={load}>{m['machines.refresh']()}</Button>
		</div>
	{:else if ordered.length === 0}
		{#if !writing}<p class="text-xs text-muted-foreground">{m['profiles.skills_empty']()}</p>{/if}
	{:else}
		<ul class="divide-y rounded-lg border" role="list">
			{#each ordered as skill (skill.id)}
				{@const on = selected.includes(skill.id)}
				{@const usable = attachable(skill)}
				<li>
					<div class="flex items-start gap-3 px-3 py-2.5">
						<Checkbox
							class="mt-0.5"
							checked={on}
							disabled={!editable || (!on && (!usable || selected.length >= MAX))}
							aria-label={skill.name}
							onCheckedChange={(value) => toggle(skill, value === true)}
						/>
						<div class="min-w-0 flex-1">
							<div class="flex flex-wrap items-center gap-1.5">
								<span class="truncate text-sm font-medium">{skill.name}</span>
								{#if skill.status === 'draft'}<Badge variant="outline">{m['profiles.skill_draft']()}</Badge>{/if}
								{#if !skill.enabled}<Badge variant="outline">{m['profiles.skill_disabled']()}</Badge>{/if}
							</div>
							<p class="line-clamp-2 text-xs text-muted-foreground">{skill.description}</p>
						</div>
						<Button
							size="icon-sm"
							variant="ghost"
							aria-label={m['profiles.skill_preview']({ name: skill.name })}
							aria-expanded={open === skill.id}
							onclick={() => togglePreview(skill)}
							><ChevronDown class={cn('transition-transform', open === skill.id && 'rotate-180')} /></Button
						>
					</div>
					{#if open === skill.id}
						<div class="border-t bg-muted/30 px-4 py-3">
							{#if bodies[skill.id] === undefined}<p class="text-xs text-muted-foreground">{m['common.loading']()}</p>
							{:else}<Markdown source={bodies[skill.id]} class="text-sm" />{/if}
						</div>
					{/if}
				</li>
			{/each}
		</ul>
	{/if}
</section>
