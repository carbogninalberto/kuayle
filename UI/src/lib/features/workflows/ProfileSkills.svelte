<script lang="ts">
	import { onMount } from 'svelte';
	import { ChevronDown, FileUp, Plus, X } from '@lucide/svelte';
	import * as Tabs from '$lib/components/ui/tabs/index.js';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Checkbox } from '$lib/components/ui/checkbox/index.js';
	import { Textarea } from '$lib/components/ui/textarea/index.js';
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
		view = $state<'write' | 'preview'>('write'),
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

	async function preview(skill: AssistantSkill) {
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
		view = 'write';
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
		<div>
			<h3 id="profile-skills" class="text-sm font-medium">
				{m['profiles.section_skills']()}
				<span class="ml-1 text-xs font-normal text-muted-foreground"
					>{m['profiles.skills_count']({ count: selected.length })}</span
				>
			</h3>
			<p class="mt-0.5 text-xs text-muted-foreground">{m['profiles.skills_help']()}</p>
		</div>
		{#if editable && !writing}<Button size="sm" variant="outline" onclick={() => (writing = true)}
				><Plus />{m['profiles.skill_new']()}</Button
			>{/if}
	</div>

	{#if writing}
		<div class="space-y-2 rounded-lg border bg-muted/30 p-3" data-testid="profile-skill-editor">
			<div class="flex flex-wrap items-center justify-between gap-2">
				<Tabs.Root bind:value={view}>
					<Tabs.List>
						<Tabs.Trigger value="write">{m['profiles.write']()}</Tabs.Trigger>
						<Tabs.Trigger value="preview">{m['profiles.preview']()}</Tabs.Trigger>
					</Tabs.List>
				</Tabs.Root>
				<div class="flex items-center gap-1">
					<input
						bind:this={file}
						type="file"
						accept=".md,text/markdown,text/plain"
						class="hidden"
						onchange={importFile}
					/>
					<Button size="sm" variant="ghost" onclick={() => file?.click()}
						><FileUp />{m['profiles.skill_import']()}</Button
					>
					<Button
						size="icon-sm"
						variant="ghost"
						aria-label={m['common.cancel']()}
						onclick={() => {
							writing = false;
							source = SKILL_TEMPLATE;
						}}><X /></Button
					>
				</div>
			</div>
			{#if view === 'write'}
				<Textarea
					aria-label={m['profiles.skill_new']()}
					bind:value={source}
					spellcheck={false}
					class="min-h-72 resize-y bg-background font-mono text-[13px] leading-relaxed"
				/>
			{:else}
				<div class="min-h-72 space-y-2 rounded-md border bg-background px-4 py-3">
					<div class="flex flex-wrap items-baseline gap-2 border-b pb-2">
						<code class="text-sm font-medium">{parsed.name || '—'}</code>
						<span class="text-xs text-muted-foreground">{parsed.description}</span>
					</div>
					<Markdown source={parsed.body} class="text-sm" />
				</div>
			{/if}
			{#if parsed.problems.length > 0}
				<ul class="space-y-0.5 text-xs text-destructive" role="list">
					{#each parsed.problems as problem (problem)}<li>{problems[problem]()}</li>{/each}
				</ul>
			{:else}
				<p class="text-xs text-muted-foreground">{m['profiles.skill_format_help']()}</p>
			{/if}
			<div class="flex justify-end">
				<Button size="sm" onclick={saveSkill} disabled={saving || parsed.problems.length > 0}
					>{m['profiles.skill_save']()}</Button
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
							onclick={() => preview(skill)}
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
