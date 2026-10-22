<script lang="ts">
	import { m } from '$lib/paraglide/messages.js';
	import SelectField from '$lib/components/shared/SelectField.svelte';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Textarea } from '$lib/components/ui/textarea/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import type { CreateIssueRequest, IssuePriority } from '$lib/types/issue';
	import type { Team } from '$lib/types/team';
	import { getPriorityLabels } from '$lib/types/issue';
	import { teamStatusesState } from './team-statuses.state.svelte';

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
	let statusId = $state(teamStatusesState.defaultForCategory('backlog')?.id ?? '');
	let priority = $state<IssuePriority>(0);
	// svelte-ignore state_referenced_locally
	let teamId = $state(teams[0]?.id ?? '');

	function handleSubmit(e: Event) {
		e.preventDefault();
		onsubmit({
			title,
			description: description || undefined,
			status_id: statusId || undefined,
			priority,
			team_id: teamId
		});
	}
</script>

<form onsubmit={handleSubmit} class="space-y-4 p-4">
	<div>
		<Input
			type="text"
			bind:value={title}
			placeholder="Issue title"
			required
			class="w-full bg-transparent text-lg font-medium"
		/>
	</div>

	<div>
		<Textarea
			bind:value={description}
			placeholder="Add description..."
			rows={3}
			class="w-full"
		></Textarea>
	</div>

	<div class="flex flex-wrap gap-3">
		<SelectField
			aria-label={m['sharedComponents.create_issue.team']()} bind:value={teamId}
			class="rounded border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-2 py-1.5 text-sm text-[var(--color-text-secondary)]"
		 options={teams.map(team => ({ value: team.id, label: team.name }))} />

		<SelectField
			aria-label={m['sharedComponents.create_issue.status']()} bind:value={statusId}
			class="rounded border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-2 py-1.5 text-sm text-[var(--color-text-secondary)]"
		 options={teamStatusesState.statusOrder.map(status => ({ value: status.id, label: status.name }))} />

		<SelectField
			aria-label={m['issue.priority']()} bind:value={priority}
			class="rounded border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-2 py-1.5 text-sm text-[var(--color-text-secondary)]"
		 options={Object.entries(getPriorityLabels()).map(([value, label]) => ({ value: Number(value) as IssuePriority, label }))} />
	</div>

	<div class="flex justify-end gap-2">
		<Button variant="outline" size="sm"
			type="button"
			onclick={oncancel}
			class="rounded-md border px-3 py-1.5 text-sm text-[var(--color-text-secondary)]"
		>
			Cancel
		</Button>
		<Button variant="default" size="sm"
			type="submit"
			disabled={!title.trim() || !teamId}
			class="rounded-md px-3 py-1.5 text-sm disabled:opacity-50"
		>
			Create issue
		</Button>
	</div>
</form>
