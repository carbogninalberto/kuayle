<script lang="ts">
	import type { CreateIssueRequest, IssueStatus, IssuePriority } from '$lib/types/issue';
	import type { Team } from '$lib/types/team';
	import { STATUS_LABELS, PRIORITY_LABELS } from '$lib/types/issue';

	let {
		teams,
		onsubmit,
		oncancel
	}: {
		teams: Team[];
		onsubmit: (req: CreateIssueRequest) => void;
		oncancel: () => void;
	} = $props();

	let title = $state('');
	let description = $state('');
	let status = $state<IssueStatus>('backlog');
	let priority = $state<IssuePriority>(0);
	let teamId = $state(teams[0]?.id ?? '');

	function handleSubmit(e: Event) {
		e.preventDefault();
		onsubmit({
			title,
			description: description || undefined,
			status,
			priority,
			team_id: teamId
		});
	}
</script>

<form onsubmit={handleSubmit} class="space-y-4 p-4">
	<div>
		<input
			type="text"
			bind:value={title}
			placeholder="Issue title"
			required
			autofocus
			class="w-full bg-transparent text-lg font-medium text-[var(--color-text-primary)] outline-none placeholder:text-[var(--color-text-tertiary)]"
		/>
	</div>

	<div>
		<textarea
			bind:value={description}
			placeholder="Add description..."
			rows={3}
			class="w-full rounded border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-3 py-2 text-sm text-[var(--color-text-primary)] outline-none placeholder:text-[var(--color-text-tertiary)] focus:border-[var(--app-accent)]"
		></textarea>
	</div>

	<div class="flex flex-wrap gap-3">
		<select
			bind:value={teamId}
			class="rounded border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-2 py-1.5 text-sm text-[var(--color-text-secondary)]"
		>
			{#each teams as team}
				<option value={team.id}>{team.name}</option>
			{/each}
		</select>

		<select
			bind:value={status}
			class="rounded border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-2 py-1.5 text-sm text-[var(--color-text-secondary)]"
		>
			{#each Object.entries(STATUS_LABELS) as [value, label]}
				<option {value}>{label}</option>
			{/each}
		</select>

		<select
			bind:value={priority}
			class="rounded border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-2 py-1.5 text-sm text-[var(--color-text-secondary)]"
		>
			{#each Object.entries(PRIORITY_LABELS) as [value, label]}
				<option value={Number(value)}>{label}</option>
			{/each}
		</select>
	</div>

	<div class="flex justify-end gap-2">
		<button
			type="button"
			onclick={oncancel}
			class="rounded-md border border-[var(--app-border)] px-3 py-1.5 text-sm text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]"
		>
			Cancel
		</button>
		<button
			type="submit"
			disabled={!title.trim() || !teamId}
			class="rounded-md bg-[var(--app-accent)] px-3 py-1.5 text-sm text-white hover:bg-[var(--app-accent-hover)] disabled:opacity-50"
		>
			Create issue
		</button>
	</div>
</form>
