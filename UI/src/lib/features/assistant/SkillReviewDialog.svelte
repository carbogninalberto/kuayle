<script lang="ts">
	import * as Dialog from '$lib/components/ui/dialog/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { assistantSkills, type AssistantSkill, type AssistantSkillVersion } from '$lib/api/assistant-skills';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/paraglide/messages.js';
	import { diffLines } from './skill-diff';

	// Shows a draft against the version in effect before an admin activates it.
	// Both texts are only rendered as text.
	let {
		open = $bindable(false),
		slug,
		skill,
		draft,
		onactivated
	}: {
		open?: boolean;
		slug: string;
		/** The skill with the body in effect. */
		skill: AssistantSkill;
		draft: AssistantSkillVersion;
		onactivated: (skill: AssistantSkill) => void;
	} = $props();

	let activating = $state(false);
	// A draft skill has nothing in effect yet: show its whole text.
	const replaces = $derived(skill.status === 'active');
	const lines = $derived(
		replaces
			? diffLines(skill.body ?? '', draft.body)
			: draft.body.split('\n').map((text) => ({ kind: 'same' as const, text }))
	);

	async function activate() {
		if (activating) return;
		activating = true;
		try {
			const updated = await assistantSkills.activate(slug, skill.id, draft.version);
			appToast.success(m['assistant_skills.activated_toast']({ name: updated.name, version: String(draft.version) }));
			open = false;
			onactivated(updated);
		} catch (err) {
			appToast.apiError(err, m['assistant_skills.failed_save']());
		} finally {
			activating = false;
		}
	}
</script>

<Dialog.Root bind:open>
	<Dialog.Content class="max-h-[85vh] overflow-y-auto sm:max-w-2xl">
		<Dialog.Header>
			<Dialog.Title>{m['assistant_skills.review_title']({ version: String(draft.version) })}</Dialog.Title>
			<Dialog.Description>
				{replaces
					? m['assistant_skills.review_update']({ version: String(skill.current_version) })
					: m['assistant_skills.review_new']()}
			</Dialog.Description>
		</Dialog.Header>
		{#if replaces && draft.name !== skill.name}<p class="text-xs break-words">
				{m['assistant_skills.name_change']({ before: skill.name, after: draft.name })}
			</p>{/if}
		{#if replaces && draft.description !== skill.description}<p class="text-xs break-words">
				{m['assistant_skills.description_change']({ before: skill.description, after: draft.description })}
			</p>{/if}
		{#if !replaces}<p class="text-xs break-words text-muted-foreground">{draft.description}</p>{/if}
		<pre
			class="max-h-[50vh] overflow-auto rounded-lg border bg-muted/40 py-2 font-mono text-xs leading-relaxed whitespace-pre-wrap break-words">{#each lines as line, index (index)}<div
					class="px-3 {line.kind === 'add'
						? 'bg-[var(--status-success)]/10 text-[var(--status-success)]'
						: line.kind === 'del'
							? 'bg-destructive/10 text-destructive line-through'
							: ''}"
					aria-label={line.kind === 'add'
						? m['assistant_skills.diff_added']()
						: line.kind === 'del'
							? m['assistant_skills.diff_removed']()
							: undefined}
					role={line.kind === 'same' ? undefined : 'group'}><span aria-hidden="true" class="select-none"
						>{line.kind === 'add' ? '+ ' : line.kind === 'del' ? '- ' : '  '}</span
					>{line.text || ' '}</div>{/each}</pre>
		<Dialog.Footer class="gap-2">
			<Button variant="outline" onclick={() => (open = false)}>{m['assistant_skills.cancel']()}</Button>
			<Button disabled={activating} onclick={activate}>{m['assistant_skills.activate']()}</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
