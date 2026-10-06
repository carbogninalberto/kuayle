<script lang="ts">
	import * as Dialog from '$lib/components/ui/dialog';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import type { CreateTeamInput } from '$lib/types/team';
	import PrivacyNotice from './PrivacyNotice.svelte';
	import { m } from '$lib/paraglide/messages.js';
	import { getLocale, setLocale } from '$lib/paraglide/runtime.js';

	let {
		open = $bindable(false),
		onsubmit
	}: {
		open: boolean;
		onsubmit: (data: CreateTeamInput) => Promise<boolean>;
	} = $props();

	let generation = 0;
	let isPrivate = $state(false);
	let acknowledged = $state(false);
	let submitting = $state(false);
	let name = $state('');
	let key = $state('');
	let description = $state('');
	let keyManuallyEdited = $state(false);

	$effect(() => {
		generation++;
		if (open) {
			name = '';
			isPrivate = false; acknowledged = false; submitting = false;
			key = '';
			description = '';
			keyManuallyEdited = false;
		}
	});

	// Auto-generate key from name (first 3 chars uppercase)
	$effect(() => {
		if (!keyManuallyEdited && name) {
			key = name
				.replace(/[^a-zA-Z]/g, '')
				.slice(0, 3)
				.toUpperCase();
		}
	});

	async function handleSubmit(e: Event) {
		e.preventDefault();
		if (!name.trim() || !key.trim() || submitting || (isPrivate && !acknowledged)) return;
		const version = generation;
		submitting = true;
		const success = await onsubmit({
			name: name.trim(),
			key: key.trim().toUpperCase(),
			description: description.trim() || undefined,
			is_private: isPrivate, acknowledge_privacy_limitations: isPrivate && acknowledged
		});
		if (version !== generation) return;
		submitting = false;
		if (success) open = false;
	}
</script>

<Dialog.Root bind:open>
	<Dialog.Content class="sm:max-w-[420px] border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-0 max-h-[85dvh] overflow-y-auto rounded-xl">
		<form onsubmit={handleSubmit}>
			<div class="px-5 pt-5 pb-4 space-y-4">
				<div>
					<Dialog.Title class="text-base font-semibold text-[var(--color-text-primary)]">{m['sidebar.create_team_title']()}</Dialog.Title>
					<p class="mt-0.5 text-xs text-[var(--color-text-tertiary)]">{m['sidebar.create_team_desc']()}</p>
				</div>

				<div class="space-y-1.5">
					<Label class="text-xs text-[var(--color-text-secondary)]">{m['sidebar.name']()}</Label>
					<Input
						bind:value={name}
						placeholder="e.g. Engineering"
						required
						class="bg-[var(--color-bg)] border-[var(--app-border)] text-[var(--color-text-primary)]"
					/>
				</div>

				<div class="space-y-1.5">
					<Label class="text-xs text-[var(--color-text-secondary)]">{m['sidebar.identifier']()}</Label>
					<Input
						bind:value={key}
						placeholder="e.g. ENG"
						required
						maxlength={10}
						oninput={() => (keyManuallyEdited = true)}
						class="bg-[var(--color-bg)] border-[var(--app-border)] text-[var(--color-text-primary)] uppercase"
					/>
					<p class="text-[10px] text-[var(--color-text-tertiary)]">{m['sidebar.identifier_desc']()}</p>
				</div>

				<div class="space-y-1.5">
					<Label class="text-xs text-[var(--color-text-secondary)]">{m['sidebar.description_optional']()}</Label>
					<Input
						bind:value={description}
						placeholder={m['sidebar.team_desc_placeholder']()}
						class="bg-[var(--color-bg)] border-[var(--app-border)] text-[var(--color-text-primary)]"
					/>
				</div>
				<div class="space-y-3 border-t border-[var(--app-border)] pt-3">
					<label class="flex items-center gap-2 text-sm"><input type="checkbox" bind:checked={isPrivate} disabled={submitting} /><span>{m['privacy.private']()}</span></label>
					{#if isPrivate}
						<PrivacyNotice />
						<label class="flex items-start gap-2 text-xs"><input type="checkbox" bind:checked={acknowledged} disabled={submitting} class="mt-0.5" /><span>{m['privacy.acknowledge']()}</span></label>
					{:else}<p class="text-xs text-[var(--color-text-secondary)]">{m['privacy.public_description']()}</p>{/if}
				</div>
			</div>

			<div class="flex justify-end gap-2 border-t border-[var(--app-border)] px-5 py-3">
				<Button variant="outline" size="sm" type="button" onclick={() => (open = false)}>{m['sidebar.cancel']()}</Button>
				<Button size="sm" type="submit" disabled={!name.trim() || !key.trim() || submitting || (isPrivate && !acknowledged)}>{m['sidebar.create_team_title']()}</Button>
			</div>
		</form>
	</Dialog.Content>
</Dialog.Root>
