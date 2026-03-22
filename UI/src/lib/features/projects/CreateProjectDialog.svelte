<script lang="ts">
	import * as Dialog from '$lib/components/ui/dialog';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';

	import type { Team } from '$lib/types/team';

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
	<Dialog.Content class="sm:max-w-[420px] border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-0 overflow-hidden rounded-xl">
		<form onsubmit={handleSubmit}>
			<div class="px-5 pt-5 pb-4 space-y-4">
				<div>
					<h2 class="text-base font-semibold text-[var(--color-text-primary)]">Create project</h2>
					<p class="mt-0.5 text-xs text-[var(--color-text-tertiary)]">Projects group related issues together.</p>
				</div>

				<div class="space-y-1.5">
					<Label class="text-xs text-[var(--color-text-secondary)]">Name</Label>
					<Input
						bind:value={name}
						placeholder="e.g. Q1 Launch"
						required
						class="bg-[var(--color-bg)] border-[var(--app-border)] text-[var(--color-text-primary)]"
					/>
				</div>

				{#if teams.length > 0}
					<div class="space-y-1.5">
						<Label class="text-xs text-[var(--color-text-secondary)]">Team <span class="text-[var(--color-text-tertiary)]">(optional)</span></Label>
						<select
							bind:value={teamId}
							class="flex h-9 w-full rounded-md border border-[var(--app-border)] bg-[var(--color-bg)] px-3 py-1 text-sm text-[var(--color-text-primary)]"
						>
							<option value="">No team</option>
							{#each teams as team}
								<option value={team.id}>{team.name}</option>
							{/each}
						</select>
					</div>
				{/if}

				<div class="space-y-1.5">
					<Label class="text-xs text-[var(--color-text-secondary)]">Description <span class="text-[var(--color-text-tertiary)]">(optional)</span></Label>
					<Input
						bind:value={description}
						placeholder="What is this project about?"
						class="bg-[var(--color-bg)] border-[var(--app-border)] text-[var(--color-text-primary)]"
					/>
				</div>
			</div>

			<div class="flex justify-end gap-2 border-t border-[var(--app-border)] px-5 py-3">
				<Button variant="outline" size="sm" type="button" onclick={() => (open = false)}>Cancel</Button>
				<Button size="sm" type="submit" disabled={!name.trim()}>Create project</Button>
			</div>
		</form>
	</Dialog.Content>
</Dialog.Root>
