<script lang="ts">
	import { onMount } from 'svelte';
	import { listTeams } from '$lib/api/teams';
	import { listTeamRoles, setTeamRole, type TeamRole } from '$lib/api/workspace-roles';
	import { Button } from '$lib/components/ui/button';
	import * as Dialog from '$lib/components/ui/dialog';
	import SelectField from '$lib/components/shared/SelectField.svelte';
	import { m } from '$lib/features/i18n/messages';
	import { getErrorMessage } from '$lib/features/toast/toast';
	import type { Team } from '$lib/types/team';
	import type { WorkspaceMember } from '$lib/types/workspace';

	let {
		slug,
		member,
		options,
		onclose,
		onchanged
	}: {
		slug: string;
		member: WorkspaceMember;
		options: { value: string; label: string }[];
		onclose: () => void;
		onchanged: () => void;
	} = $props();
	let teams = $state<Team[]>([]);
	let overrides = $state<TeamRole[]>([]);
	let loading = $state(true);
	let loadFailed = $state(false);
	let busy = $state(false);
	let error = $state('');
	const choices = $derived([{ value: 'inherit', label: m['settings.roles.inherit']() }, ...options]);
	onMount(() => {
		void load();
	});

	async function load() {
		loading = true;
		loadFailed = false;
		try {
			[teams, overrides] = await Promise.all([listTeams(slug), listTeamRoles(slug, member.user_id)]);
		} catch {
			loadFailed = true;
		} finally {
			loading = false;
		}
	}

	async function change(team: string, role: string) {
		busy = true;
		error = '';
		try {
			await setTeamRole(slug, member.user_id, team, role === 'inherit' ? null : role);
			overrides = overrides.filter((o) => o.team_id !== team);
			if (role !== 'inherit') overrides.push({ team_id: team, role });
			onchanged();
		} catch (err) {
			error = getErrorMessage(err, m['settings.members.failed_update_role']());
		} finally {
			busy = false;
		}
	}
</script>

<Dialog.Root
	open
	onOpenChange={(next) => {
		if (!next && !busy) onclose();
	}}
>
	<Dialog.Content showCloseButton={!busy}>
		<Dialog.Header>
			<Dialog.Title>{m['settings.roles.team']()}</Dialog.Title>
			<Dialog.Description>{member.name || member.email}</Dialog.Description>
		</Dialog.Header>
		{#if loading}<p role="status">{m['common.loading']()}</p>
		{:else if loadFailed}<p role="alert">{m['settings.roles.load_failed']()}</p>
			<Button variant="outline" onclick={load}>{m['settings.tokens.retry']()}</Button>
		{:else if !teams.length}<p>{m['agents.identity.no_teams']()}</p>
		{:else}
			<div class="max-h-[55dvh] space-y-3 overflow-y-auto p-1">
				{#each teams as team (team.id)}
					<div
						class="grid min-w-0 items-center gap-2 sm:grid-cols-2 [&_[data-slot=select-trigger]]:max-w-full [&_[data-slot=select-value]]:block [&_[data-slot=select-value]]:truncate"
					>
						<label for={'override-' + team.id} class="break-words">{team.name}</label>
						<SelectField
							id={'override-' + team.id}
							options={choices}
							disabled={busy}
							bind:value={
								() => overrides.find((o) => o.team_id === team.id)?.role ?? 'inherit',
								(value) => {
									void change(team.id, value);
								}
							}
						/>
					</div>
				{/each}
			</div>
		{/if}
		{#if error}<p role="alert" class="text-destructive">{error}</p>{/if}
		<Dialog.Footer
			><Button variant="outline" disabled={busy} onclick={onclose}>{m['common.close']()}</Button></Dialog.Footer
		>
	</Dialog.Content>
</Dialog.Root>
