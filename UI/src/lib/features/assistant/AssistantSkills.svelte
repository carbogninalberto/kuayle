<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { ArrowLeft, Bot, Plus, UserRound, Users, Building2, PencilLine, Trash2, History } from '@lucide/svelte';
	import { Alert, AlertDescription, AlertAction } from '$lib/components/ui/alert/index.js';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import * as CodeBlock from '$lib/components/ui/code-block/index.js';
	import * as Item from '$lib/components/ui/item/index.js';
	import { Switch } from '$lib/components/ui/switch/index.js';
	import SelectField from '$lib/components/shared/SelectField.svelte';
	import ConfirmDialog from '$lib/components/shared/ConfirmDialog.svelte';
	import {
		assistantSkills,
		type AssistantSkill,
		type AssistantSkillList,
		type AssistantSkillVersion
	} from '$lib/api/assistant-skills';
	import { listTeams } from '$lib/api/teams';
	import type { Team } from '$lib/types/team';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/paraglide/messages.js';
	import { getLocale } from '$lib/paraglide/runtime.js';
	import Markdown from './Markdown.svelte';
	import SkillEditorDialog from './SkillEditorDialog.svelte';
	import SkillReviewDialog from './SkillReviewDialog.svelte';

	// Settings → Assistant → Skills. Write controls are shown to owners and
	// admins (`can_manage`); the server enforces every write on its own.
	let { slug }: { slug: string } = $props();

	let list = $state<AssistantSkillList | null>(null);
	let failed = $state(false);
	let teams = $state<Team[]>([]);
	let scope = $state('all');
	let toggling = $state<string | null>(null);

	let skill = $state<AssistantSkill | null>(null);
	let versions = $state<AssistantSkillVersion[]>([]);
	let expanded = $state<Record<number, boolean>>({});
	let busy = $state(false);

	let creating = $state(false);
	let editing = $state(false);
	let reviewing = $state<AssistantSkillVersion | null>(null);
	let reviewOpen = $state(false);
	let deleting = $state(false);

	// The open skill lives in the URL so chat cards can deep-link to it.
	const selected = $derived(page.url.searchParams.get('skill'));
	const manage = $derived(list?.can_manage ?? false);
	const scopes = $derived.by(() => {
		const seen = new Map<string, string>();
		for (const entry of list?.skills ?? [])
			if (entry.team_id) seen.set(entry.team_id, entry.team_name ?? entry.team_key ?? '');
		return [
			{ value: 'all', label: m['assistant_skills.filter_all']() },
			{ value: 'workspace', label: m['assistant_skills.scope_workspace']() },
			...[...seen].map(([value, label]) => ({ value, label }))
		];
	});
	const shown = $derived(
		(list?.skills ?? []).filter((entry) =>
			scope === 'all' ? true : scope === 'workspace' ? entry.team_id === null : entry.team_id === scope
		)
	);
	// The draft waiting for activation: a pending edit, or a draft skill itself.
	const draft = $derived.by(() => {
		if (!skill) return null;
		const version = skill.status === 'draft' ? skill.current_version : skill.pending_version;
		return versions.find((entry) => entry.version === version) ?? null;
	});

	// Editing continues from a pending draft when there is one.
	const editFrom = $derived(
		skill && draft && skill.status === 'active'
			? draft
			: { name: skill?.name ?? '', description: skill?.description ?? '', body: skill?.body ?? '' }
	);

	function review(version: AssistantSkillVersion) {
		reviewing = version;
		reviewOpen = true;
	}

	const when = (at: string) => new Date(at).toLocaleString(getLocale(), { dateStyle: 'medium', timeStyle: 'short' });
	const scopeLabel = (entry: AssistantSkill) =>
		entry.team_id ? (entry.team_name ?? entry.team_key ?? '') : m['assistant_skills.scope_workspace']();
	const activatable = (entry: AssistantSkillVersion) =>
		!!skill &&
		entry.status === 'draft' &&
		(entry.version > skill.current_version || (skill.status === 'draft' && entry.version === skill.current_version));

	async function load() {
		failed = false;
		try {
			list = await assistantSkills.list(slug);
			if (list.can_manage && !teams.length) teams = await listTeams(slug);
		} catch (err) {
			failed = true;
			appToast.apiError(err, m['assistant_skills.failed_load']());
		}
	}

	let detailRequest = 0;
	async function loadDetail(id: string) {
		const request = ++detailRequest;
		try {
			const [one, history] = await Promise.all([assistantSkills.get(slug, id), assistantSkills.versions(slug, id)]);
			if (request !== detailRequest) return;
			skill = one;
			versions = history.versions;
		} catch (err) {
			if (request !== detailRequest) return;
			appToast.apiError(err, m['assistant_skills.failed_load']());
			void open(null);
		}
	}

	$effect(() => {
		const id = selected;
		skill = null;
		versions = [];
		expanded = {};
		if (id) void loadDetail(id);
		else detailRequest++;
	});

	function open(id: string | null) {
		const url = new URL(page.url);
		url.searchParams.set('tab', 'skills');
		if (id) url.searchParams.set('skill', id);
		else url.searchParams.delete('skill');
		return goto(url.pathname + url.search, { noScroll: true, keepFocus: !id });
	}

	// After a write, refresh the list and the open skill.
	async function refresh(updated: AssistantSkill) {
		await load();
		if (selected === updated.id) await loadDetail(updated.id);
		else await open(updated.id);
	}

	async function toggle(entry: AssistantSkill, enabled: boolean) {
		if (toggling) return;
		toggling = entry.id;
		try {
			const updated = await assistantSkills.setEnabled(slug, entry.id, enabled);
			if (list) list.skills = list.skills.map((one) => (one.id === updated.id ? { ...one, ...updated } : one));
			if (skill?.id === updated.id) skill = { ...skill, enabled: updated.enabled };
			appToast.success(
				updated.enabled
					? m['assistant_skills.enabled_toast']({ name: updated.name })
					: m['assistant_skills.disabled_toast']({ name: updated.name })
			);
		} catch (err) {
			appToast.apiError(err, m['assistant_skills.failed_save']());
		} finally {
			toggling = null;
		}
	}

	async function restore(version: number) {
		if (!skill || busy) return;
		busy = true;
		try {
			const updated = await assistantSkills.restore(slug, skill.id, version);
			appToast.success(
				m['assistant_skills.restored_toast']({ version: String(version), next: String(updated.latest_version) })
			);
			await refresh(updated);
		} catch (err) {
			appToast.apiError(err, m['assistant_skills.failed_save']());
		} finally {
			busy = false;
		}
	}

	async function remove() {
		if (!skill || busy) return;
		const { id, name } = skill;
		busy = true;
		try {
			await assistantSkills.remove(slug, id);
			deleting = false;
			appToast.success(m['assistant_skills.deleted_toast']({ name }));
			await load();
			await open(null);
		} catch (err) {
			appToast.apiError(err, m['assistant_skills.failed_save']());
		} finally {
			busy = false;
		}
	}

	onMount(() => {
		void load();
	});
</script>

{#snippet badges(entry: AssistantSkill)}
	<Badge variant="outline" class="font-normal"
		>{#if entry.team_id}<Users class="size-3" />{:else}<Building2 class="size-3" />{/if}{scopeLabel(entry)}</Badge
	>
	<Badge variant={entry.status === 'active' ? 'secondary' : 'outline'} class="font-normal"
		>{entry.status === 'active' ? m['assistant_skills.status_active']() : m['assistant_skills.status_draft']()}</Badge
	>
	<Badge variant="outline" class="font-normal tabular-nums"
		>{m['assistant_skills.version_short']({ version: String(entry.current_version) })}</Badge
	>
	{#if entry.pending_version}
		<Badge variant="outline" class="border-[var(--status-warning)]/40 font-normal text-[var(--status-warning)]"
			>{m['assistant_skills.pending']({ version: String(entry.pending_version) })}</Badge
		>
	{/if}
	<Badge variant="outline" class="font-normal text-muted-foreground"
		>{#if entry.author_kind === 'assistant'}<Bot class="size-3" />{m[
				'assistant_skills.by_assistant'
			]()}{:else}<UserRound class="size-3" />{m['assistant_skills.by_user']()}{/if}</Badge
	>
{/snippet}

{#snippet enabledControl(entry: AssistantSkill)}
	{#if manage}
		<Switch
			checked={entry.enabled}
			disabled={toggling !== null}
			aria-label={m['assistant_skills.toggle_label']({ name: entry.name })}
			onCheckedChange={(enabled) => toggle(entry, enabled)}
		/>
	{:else}
		<Badge variant={entry.enabled ? 'secondary' : 'outline'} class="font-normal">
			{entry.enabled ? m['assistant_skills.state_enabled']() : m['assistant_skills.state_disabled']()}
		</Badge>
	{/if}
{/snippet}

{#if selected}
	<div class="space-y-5">
		<Button variant="ghost" size="sm" class="-ml-2" onclick={() => open(null)}
			><ArrowLeft />{m['assistant_skills.back']()}</Button
		>
		{#if skill}
			<section aria-labelledby="assistant-skill-heading" class="space-y-3">
				<div class="flex flex-wrap items-start justify-between gap-3">
					<div class="min-w-0 space-y-2">
						<h2 id="assistant-skill-heading" class="text-lg font-semibold break-words">{skill.name}</h2>
						<div class="flex flex-wrap items-center gap-1.5">{@render badges(skill)}</div>
					</div>
					<div class="flex shrink-0 items-center gap-2">{@render enabledControl(skill)}</div>
				</div>
				<p class="text-sm break-words text-muted-foreground">{skill.description}</p>
				<p class="text-xs text-muted-foreground">{m['assistant_skills.updated']({ time: when(skill.updated_at) })}</p>
				{#if manage}
					<div class="flex flex-wrap gap-2">
						<Button variant="outline" size="sm" onclick={() => (editing = true)}
							><PencilLine />{m['assistant_skills.edit']()}</Button
						>
						<Button variant="outline" size="sm" class="text-destructive" onclick={() => (deleting = true)}
							><Trash2 />{m['assistant_skills.delete']()}</Button
						>
					</div>
				{/if}
				{#if draft}
					<Alert class="border-[var(--status-warning)]/40 bg-[var(--status-warning)]/5">
						<AlertDescription class="text-foreground">
							{skill.status === 'draft'
								? m['assistant_skills.draft_skill']()
								: m['assistant_skills.draft_pending']({ version: String(draft.version) })}
						</AlertDescription>
						{#if manage}
							<AlertAction>
								<Button size="sm" onclick={() => review(draft)}>{m['assistant_skills.review_draft']()}</Button>
							</AlertAction>
						{/if}
					</Alert>
				{/if}
				<div class="rounded-xl border bg-card px-4 py-2">
					<Markdown source={skill.body ?? ''} class="text-sm" />
				</div>
			</section>

			<section aria-labelledby="assistant-skill-history" class="space-y-3">
				<h3 id="assistant-skill-history" class="flex items-center gap-2 text-sm font-medium">
					<History class="size-4" />{m['assistant_skills.history']()}
				</h3>
				<Item.Group aria-labelledby="assistant-skill-history" class="gap-0 rounded-xl border bg-card">
					{#each versions as entry, index (entry.version)}
						{#if index > 0}<Item.Separator />{/if}
						<Item.Root
							role="listitem"
							aria-labelledby="assistant-skill-version-{entry.version}"
							class="flex-wrap items-start"
						>
							<Item.Content class="min-w-0">
								<Item.Title class="flex flex-wrap items-center gap-2">
									<span id="assistant-skill-version-{entry.version}"
										>{m['assistant_skills.version_label']({ version: String(entry.version) })}</span
									>
									<Badge variant={entry.status === 'active' ? 'secondary' : 'outline'} class="font-normal"
										>{entry.status === 'active'
											? m['assistant_skills.status_active']()
											: m['assistant_skills.status_draft']()}</Badge
									>
									{#if entry.version === skill.current_version && skill.status === 'active'}
										<Badge variant="outline" class="font-normal">{m['assistant_skills.in_effect']()}</Badge>
									{/if}
								</Item.Title>
								<Item.Description class="line-clamp-none text-xs">
									{entry.author_kind === 'assistant'
										? m['assistant_skills.by_assistant']()
										: m['assistant_skills.by_user']()}
									· {when(entry.created_at)}
								</Item.Description>
							</Item.Content>
							<Item.Actions class="flex-wrap">
								<Button
									variant="link"
									size="sm"
									aria-expanded={expanded[entry.version] ?? false}
									aria-controls="assistant-skill-version-{entry.version}-text"
									onclick={() => (expanded[entry.version] = !expanded[entry.version])}
									>{expanded[entry.version]
										? m['assistant_skills.hide_text']()
										: m['assistant_skills.show_text']()}</Button
								>
								{#if manage && activatable(entry)}
									<Button size="sm" variant="outline" disabled={busy} onclick={() => review(entry)}
										>{m['assistant_skills.activate']()}</Button
									>
								{/if}
								{#if manage && entry.version !== skill.current_version}
									<Button size="sm" variant="outline" disabled={busy} onclick={() => restore(entry.version)}
										>{m['assistant_skills.restore']()}</Button
									>
								{/if}
							</Item.Actions>
							{#if expanded[entry.version]}
								<div id="assistant-skill-version-{entry.version}-text" class="basis-full space-y-1">
									{#if entry.name !== skill.name || entry.description !== skill.description}
										<p class="text-xs break-words text-muted-foreground">{entry.name} — {entry.description}</p>
									{/if}
									<CodeBlock.Root size="sm"
										><CodeBlock.Content code={entry.body} wrap class="max-h-64 text-xs" /></CodeBlock.Root
									>
								</div>
							{/if}
						</Item.Root>
					{/each}
				</Item.Group>
			</section>

			<SkillEditorDialog bind:open={editing} {slug} {skill} initial={editFrom} onsaved={refresh} />
			{#if reviewing}
				<SkillReviewDialog bind:open={reviewOpen} {slug} {skill} draft={reviewing} onactivated={refresh} />
			{/if}
			{#if deleting}
				<ConfirmDialog
					title={m['assistant_skills.delete_title']({ name: skill.name })}
					message={m['assistant_skills.delete_message']()}
					confirmLabel={m['assistant_skills.delete']()}
					onconfirm={remove}
					oncancel={() => (deleting = false)}
				/>
			{/if}
		{:else}
			<p role="status" class="text-sm text-muted-foreground">{m['assistant_skills.loading']()}</p>
		{/if}
	</div>
{:else if list}
	<section aria-labelledby="assistant-skills-heading" class="space-y-3">
		<div class="flex flex-wrap items-center justify-between gap-2">
			<h2 id="assistant-skills-heading" class="text-sm font-medium">{m['assistant_skills.heading']()}</h2>
			<div class="flex flex-wrap items-center gap-2">
				{#if list.skills.length}
					<SelectField bind:value={scope} options={scopes} aria-label={m['assistant_skills.filter_label']()} />
				{/if}
				{#if manage}
					<Button size="sm" onclick={() => (creating = true)}><Plus />{m['assistant_skills.new']()}</Button>
				{/if}
			</div>
		</div>
		<p class="text-xs text-muted-foreground">
			{manage ? m['assistant_skills.hint']() : m['assistant_skills.admin_only']()}
		</p>
		{#if shown.length}
			<Item.Group aria-label={m['assistant_skills.list_label']()} class="gap-0 rounded-xl border bg-card">
				{#each shown as entry, index (entry.id)}
					{#if index > 0}<Item.Separator />{/if}
					<Item.Root role="listitem" aria-labelledby="assistant-skill-{entry.id}" class="items-start">
						<Item.Content class="min-w-0 gap-1.5">
							<Item.Title class="max-w-full">
								<Button
									id="assistant-skill-{entry.id}"
									variant="link"
									class="h-auto max-w-full justify-start p-0 text-left text-sm font-medium whitespace-normal break-words text-foreground"
									onclick={() => open(entry.id)}>{entry.name}</Button
								>
							</Item.Title>
							<Item.Description class="line-clamp-2 text-xs break-words">{entry.description}</Item.Description>
							<div class="flex flex-wrap items-center gap-1.5">{@render badges(entry)}</div>
							<p class="text-xs text-muted-foreground">
								{m['assistant_skills.updated']({ time: when(entry.updated_at) })}
							</p>
						</Item.Content>
						<Item.Actions>{@render enabledControl(entry)}</Item.Actions>
					</Item.Root>
				{/each}
			</Item.Group>
		{:else}
			<p class="rounded-xl border border-dashed px-4 py-6 text-center text-sm text-muted-foreground">
				{list.skills.length ? m['assistant_skills.empty_filter']() : m['assistant_skills.empty']()}
			</p>
		{/if}
	</section>
	<SkillEditorDialog bind:open={creating} {slug} {teams} onsaved={refresh} />
{:else if failed}
	<Button variant="outline" onclick={load}>{m['assistant_skills.reload']()}</Button>
{:else}
	<p role="status" class="text-sm text-muted-foreground">{m['assistant_skills.loading']()}</p>
{/if}
