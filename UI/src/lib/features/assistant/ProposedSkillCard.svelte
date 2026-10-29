<script lang="ts" module>
	export interface ProposedSkill {
		skill_id: string;
		name: string;
		description: string;
		body: string;
		team_key: string | null;
		version: number;
		active_version: number | null;
	}

	const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

	/** The stored propose_skill result, or undefined when it is not a usable card. */
	export function proposedSkill(output: unknown): ProposedSkill | undefined {
		const card = output as Partial<Record<keyof ProposedSkill, unknown>> | null | undefined;
		if (
			!card ||
			typeof card.skill_id !== 'string' ||
			!uuidPattern.test(card.skill_id) ||
			typeof card.name !== 'string' ||
			typeof card.description !== 'string' ||
			typeof card.body !== 'string' ||
			typeof card.version !== 'number'
		)
			return undefined;
		return {
			skill_id: card.skill_id,
			name: card.name,
			description: card.description,
			body: card.body,
			team_key: typeof card.team_key === 'string' ? card.team_key : null,
			version: card.version,
			active_version: typeof card.active_version === 'number' ? card.active_version : null
		};
	}
</script>

<script lang="ts">
	import { BookOpenText, Settings } from '@lucide/svelte';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import * as Item from '$lib/components/ui/item/index.js';
	import { m } from '$lib/paraglide/messages.js';
	import Markdown from './Markdown.svelte';

	// The assistant's proposed skill is a draft: the card shows its full text,
	// rendered by the sanitizing Markdown renderer, and links to Settings where
	// an owner or admin activates it.
	let { skill, slug, id }: { skill: ProposedSkill; slug: string; id: string } = $props();

	const version = $derived(String(skill.version));
	const summary = $derived(
		skill.active_version === null
			? skill.team_key
				? m['assistant.skill_draft_new_team']({ version, team: skill.team_key })
				: m['assistant.skill_draft_new']({ version })
			: skill.team_key
				? m['assistant.skill_draft_update_team']({
						version,
						team: skill.team_key,
						active: String(skill.active_version)
					})
				: m['assistant.skill_draft_update']({ version, active: String(skill.active_version) })
	);
	const href = $derived(
		`/${encodeURIComponent(slug)}/settings/assistant?tab=skills&skill=${encodeURIComponent(skill.skill_id)}`
	);
</script>

<article aria-labelledby="proposed-skill-{id}" class="min-w-0">
	<Item.Root variant="outline" size="sm" class="items-start">
		<Item.Media variant="icon"><BookOpenText /></Item.Media>
		<Item.Content class="min-w-0 gap-1.5">
			<Item.Title class="flex flex-wrap items-center gap-2">
				<span id="proposed-skill-{id}" class="break-words"
					>{m['assistant.skill_proposed_title']({ name: skill.name })}</span
				>
				<Badge variant="outline" class="border-[var(--status-warning)]/40 font-normal text-[var(--status-warning)]"
					>{m['assistant.skill_proposed_badge']()}</Badge
				>
			</Item.Title>
			<Item.Description class="line-clamp-none break-words">{skill.description}</Item.Description>
			<p class="text-xs text-muted-foreground">{summary}</p>
			<div class="max-h-72 overflow-y-auto rounded-lg border bg-background px-3 py-1">
				<Markdown source={skill.body} class="text-sm" />
			</div>
			<div>
				<Button size="sm" variant="outline" {href}><Settings />{m['assistant.skill_review_link']()}</Button>
			</div>
		</Item.Content>
	</Item.Root>
</article>
