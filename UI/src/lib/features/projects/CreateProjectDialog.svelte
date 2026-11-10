<script lang="ts">
	import * as Dialog from '$lib/components/ui/dialog';
	import * as Select from '$lib/components/ui/select';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';

	import type { Team } from '$lib/types/team';
	import { m, getLocale, setLocale } from '$lib/features/i18n/messages';

	let {
		open = $bindable(false),
		onsubmit,
		teams = [],
		defaultTeamId
	}: {
		open: boolean;
		onsubmit: (data: { name: string; description?: string; team_id?: string }) => void;
		teams?: Team[];
		defaultTeamId?: string;
	} = $props();

	let name = $state('');
	let description = $state('');
	let teamId = $state('');

	$effect(() => {
		if (open) {
			name = '';
			description = '';
			teamId = defaultTeamId ?? '';
		}
	});

	const selectedTeamLabel = $derived(
		teamId ? (teams.find((t) => t.id === teamId)?.name ?? m['projects.create.select_team']()) : m['projects.create.no_team']()
	);

	function handleSubmit(e: Event) {
		e.preventDefault();
		if (!name.trim()) return;
		onsubmit({
			name: name.trim(),
			description: description.trim() || undefined,
			team_id: teamId || undefined
		});
		open = false;
	}
</script>

<Dialog.Root bind:open>
	<Dialog.Content
		class="sm:max-w-[420px] border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-0 overflow-hidden rounded-xl"
	>
		<form onsubmit={handleSubmit}>
			<div class="px-5 pt-5 pb-4 space-y-4">
				<div>
					<h2 class="text-base font-semibold text-[var(--color-text-primary)]">
						{m['projects.create.title']()}
					</h2>
					<p class="mt-0.5 text-xs text-[var(--color-text-tertiary)]">
						{m['projects.create.description']()}
					</p>
				</div>

				<div class="space-y-1.5">
					<Label class="text-xs text-[var(--color-text-secondary)]">{m['cycles.field.name']()}</Label>
					<Input
						bind:value={name}
						placeholder={m['projects.create.name_placeholder']()}
						required
						class="bg-[var(--color-bg)] border-[var(--app-border)] text-[var(--color-text-primary)]"
					/>
				</div>

				{#if teams.length > 0}
					<div class="space-y-1.5">
						<Label class="text-xs text-[var(--color-text-secondary)]"
							>{m['projects.field.team']()} <span class="text-[var(--color-text-tertiary)]">{m['cycles.field.optional']()}</span
							></Label
						>
						<Select.Root
							type="single"
							value={teamId}
							onValueChange={(v) => (teamId = v ?? '')}
						>
							<Select.Trigger
								class="w-full bg-[var(--color-bg)] border-[var(--app-border)] text-[var(--color-text-primary)]"
							>
								{selectedTeamLabel}
							</Select.Trigger>
							<Select.Content>
								<Select.Item value="" label={m['projects.create.no_team']()}>{m['projects.create.no_team']()}</Select.Item>
								{#each teams as team}
									<Select.Item value={team.id} label={team.name}
										>{team.name}</Select.Item
									>
								{/each}
							</Select.Content>
						</Select.Root>
					</div>
				{/if}

				<div class="space-y-1.5">
					<Label class="text-xs text-[var(--color-text-secondary)]"
						>{m['cycles.field.description']()} <span class="text-[var(--color-text-tertiary)]">{m['cycles.field.optional']()}</span
						></Label
					>
					<Input
						bind:value={description}
						placeholder={m['projects.create.description_placeholder']()}
						class="bg-[var(--color-bg)] border-[var(--app-border)] text-[var(--color-text-primary)]"
					/>
				</div>
			</div>

			<div class="flex justify-end gap-2 border-t border-[var(--app-border)] px-5 py-3">
				<Button variant="outline" size="sm" type="button" onclick={() => (open = false)}
					>{m['common.cancel']()}</Button
				>
				<Button size="sm" type="submit" disabled={!name.trim()}>{m['projects.create.title']()}</Button>
			</div>
		</form>
	</Dialog.Content>
</Dialog.Root>
