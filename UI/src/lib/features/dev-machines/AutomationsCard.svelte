<script lang="ts">
	import { Plus, Trash2, Check, X } from '@lucide/svelte';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Switch } from '$lib/components/ui/switch';
	import { Textarea } from '$lib/components/ui/textarea';
	import SelectField from '$lib/components/shared/SelectField.svelte';
	import { automations as api, type AgentAutomation, type AgentAutomationRun } from '$lib/api/automations';
	import { listTeams } from '$lib/api/teams';
	import { listLabels } from '$lib/api/labels';
	import { listTeamStatuses } from '$lib/api/team-statuses';
	import type { Team } from '$lib/types/team';
	import type { Label as IssueLabel } from '$lib/types/label';
	import type { TeamStatus } from '$lib/types/team-status';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/paraglide/messages.js';

	// Rules delegate matching issues to a coding agent. Each start acts for one
	// user under their current permissions; the backend enforces every limit.
	let { slug, unattendedAllowed }: { slug: string; unattendedAllowed: boolean } = $props();
	let rules = $state<AgentAutomation[]>([]);
	let runs = $state<AgentAutomationRun[]>([]);
	let teams = $state<Team[]>([]);
	let labels = $state<IssueLabel[]>([]);
	let statuses = $state<TeamStatus[]>([]);
	let creating = $state(false);
	let busy = $state(false);
	let name = $state('');
	let teamId = $state('');
	let labelId = $state('any');
	let statusId = $state('any');
	let instructions = $state('');
	let hour = $state('any');
	let maxActive = $state('1');
	let unattended = $state(false);

	async function load() {
		try {
			[rules, runs, teams, labels] = await Promise.all([
				api.list(slug),
				api.runs(slug),
				listTeams(slug),
				listLabels(slug)
			]);
		} catch (error) {
			appToast.apiError(error, m['automations.title']());
		}
	}
	$effect(() => {
		void slug;
		void load();
	});
	$effect(() => {
		const team = teamId;
		statusId = 'any';
		statuses = [];
		if (team)
			void listTeamStatuses(slug, team)
				.then((list) => team === teamId && (statuses = list))
				.catch(() => (statuses = []));
	});

	const valid = $derived(
		name.trim() !== '' && teamId !== '' && instructions.trim() !== '' && (labelId !== 'any' || statusId !== 'any')
	);
	const pending = $derived(runs.filter((run) => run.status === 'pending_approval'));
	const recent = $derived(runs.filter((run) => run.status !== 'pending_approval').slice(0, 10));
	const hours = Array.from({ length: 24 }, (_, value) => ({
		value: String(value),
		label: `${String(value).padStart(2, '0')}:00 UTC`
	}));

	const statusLabels: Record<AgentAutomationRun['status'], () => string> = {
		pending_approval: m['automations.status_pending_approval'],
		starting: m['automations.status_starting'],
		started: m['automations.status_started'],
		dismissed: m['automations.status_dismissed'],
		failed: m['automations.status_failed']
	};
	const statusLabel = (status: AgentAutomationRun['status']) => statusLabels[status]();

	function trigger(rule: AgentAutomation) {
		const parts = [teams.find((team) => team.id === rule.team_id)?.name ?? ''];
		const label = labels.find((item) => item.id === rule.label_id)?.name;
		if (label) parts.push(m['automations.with_label']({ label }));
		if (rule.status_id) parts.push(m['automations.with_status']());
		parts.push(
			rule.run_hour != null
				? m['automations.at_hour']({ hour: String(rule.run_hour).padStart(2, '0') })
				: m['automations.any_time']()
		);
		if (rule.unattended) parts.push(m['automations.unattended_short']());
		return parts.filter(Boolean).join(' · ');
	}

	async function create() {
		if (!valid || busy) return;
		busy = true;
		try {
			const rule = await api.create(slug, {
				team_id: teamId,
				name: name.trim(),
				label_id: labelId === 'any' ? undefined : labelId,
				status_id: statusId === 'any' ? undefined : statusId,
				instructions: instructions.trim(),
				unattended,
				run_hour: hour === 'any' ? undefined : Number(hour),
				max_active: Number(maxActive)
			});
			rules = [...rules, rule];
			creating = false;
			name = instructions = '';
			labelId = hour = 'any';
			unattended = false;
			appToast.success(m['automations.created']());
		} catch (error) {
			appToast.apiError(error, m['automations.create']());
		} finally {
			busy = false;
		}
	}

	async function toggle(rule: AgentAutomation, enabled: boolean) {
		try {
			const updated = await api.setEnabled(slug, rule.id, enabled);
			rules = rules.map((item) => (item.id === rule.id ? updated : item));
		} catch (error) {
			appToast.apiError(error, m['automations.title']());
		}
	}

	async function remove(rule: AgentAutomation) {
		try {
			await api.remove(slug, rule.id);
			rules = rules.filter((item) => item.id !== rule.id);
		} catch (error) {
			appToast.apiError(error, m['automations.delete']());
		}
	}

	async function decide(run: AgentAutomationRun, approve: boolean) {
		try {
			const updated = await api.decide(slug, run.id, approve);
			runs = runs.map((item) => (item.id === run.id ? updated : item));
		} catch (error) {
			appToast.apiError(error, m['automations.title']());
		}
	}
</script>

<section
	aria-labelledby="automations-title"
	class="space-y-4 rounded-xl border border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-5"
>
	<div class="flex items-start justify-between gap-4">
		<div>
			<h2 id="automations-title" class="text-sm font-semibold">{m['automations.title']()}</h2>
			<p class="text-xs text-[var(--color-text-tertiary)]">{m['automations.description']()}</p>
		</div>
		<Button type="button" variant="outline" onclick={() => (creating = !creating)}
			><Plus />{m['automations.new']()}</Button
		>
	</div>

	{#if creating}
		<div class="grid gap-3 rounded-lg border border-[var(--app-border)] p-3 sm:grid-cols-2">
			<div class="space-y-1">
				<Label for="automation-name">{m['automations.name']()}</Label><Input
					id="automation-name"
					bind:value={name}
					maxlength={100}
				/>
			</div>
			<div class="space-y-1">
				<Label>{m['automations.team']()}</Label><SelectField
					bind:value={teamId}
					aria-label={m['automations.team']()}
					options={[
						{ value: '', label: m['automations.choose_team']() },
						...teams.map((team) => ({ value: team.id, label: team.name }))
					]}
				/>
			</div>
			<div class="space-y-1">
				<Label>{m['automations.label']()}</Label><SelectField
					bind:value={labelId}
					aria-label={m['automations.label']()}
					options={[
						{ value: 'any', label: m['automations.any_label']() },
						...labels.map((label) => ({ value: label.id, label: label.name }))
					]}
				/>
			</div>
			<div class="space-y-1">
				<Label>{m['automations.status']()}</Label><SelectField
					bind:value={statusId}
					aria-label={m['automations.status']()}
					disabled={!teamId}
					options={[
						{ value: 'any', label: m['automations.any_status']() },
						...statuses.map((status) => ({ value: status.id, label: status.name }))
					]}
				/>
			</div>
			<div class="space-y-1 sm:col-span-2">
				<Label for="automation-instructions">{m['automations.instructions']()}</Label><Textarea
					id="automation-instructions"
					bind:value={instructions}
					rows={3}
					maxlength={4000}
				/>
			</div>
			<div class="space-y-1">
				<Label>{m['automations.schedule']()}</Label><SelectField
					bind:value={hour}
					aria-label={m['automations.schedule']()}
					options={[{ value: 'any', label: m['automations.any_time']() }, ...hours]}
				/>
			</div>
			<div class="space-y-1">
				<Label>{m['automations.max_active']()}</Label><SelectField
					bind:value={maxActive}
					aria-label={m['automations.max_active']()}
					options={['1', '2', '3', '4', '5'].map((value) => ({ value, label: value }))}
				/>
			</div>
			<div class="space-y-1 sm:col-span-2">
				<label class="flex items-center gap-2 text-sm"
					><Switch bind:checked={unattended} disabled={!unattendedAllowed} />{m['automations.unattended']()}</label
				>
				<p class="text-xs text-[var(--color-text-tertiary)]">
					{unattendedAllowed ? m['automations.unattended_help']() : m['automations.unattended_disabled']()}
				</p>
			</div>
			{#if labelId === 'any' && statusId === 'any'}<p class="text-xs text-[var(--color-text-tertiary)] sm:col-span-2">
					{m['automations.trigger_required']()}
				</p>{/if}
			<div class="flex justify-end gap-2 sm:col-span-2">
				<Button type="button" variant="ghost" onclick={() => (creating = false)}>{m['common.cancel']()}</Button>
				<Button type="button" disabled={!valid || busy} onclick={() => void create()}
					>{m['automations.create']()}</Button
				>
			</div>
		</div>
	{/if}

	{#if rules.length === 0}
		<p class="text-xs text-[var(--color-text-tertiary)]">{m['automations.none']()}</p>
	{:else}
		<ul class="space-y-2">
			{#each rules as rule (rule.id)}
				<li class="flex items-center justify-between gap-3 rounded-lg border border-[var(--app-border)] p-3">
					<div class="min-w-0">
						<p class="truncate text-sm font-medium">{rule.name}</p>
						<p class="truncate text-xs text-[var(--color-text-tertiary)]">{trigger(rule)}</p>
					</div>
					<div class="flex shrink-0 items-center gap-2">
						<Switch
							aria-label={m['automations.enabled']({ name: rule.name })}
							checked={rule.enabled}
							onCheckedChange={(value) => void toggle(rule, value)}
						/>
						<Button
							type="button"
							size="icon"
							variant="ghost"
							aria-label={m['automations.delete_named']({ name: rule.name })}
							onclick={() => void remove(rule)}><Trash2 /></Button
						>
					</div>
				</li>
			{/each}
		</ul>
	{/if}

	{#if pending.length}
		<div class="space-y-2" role="region" aria-label={m['automations.pending']()}>
			<h3 class="text-xs font-semibold">{m['automations.pending']()}</h3>
			{#each pending as run (run.id)}
				<div class="flex items-center justify-between gap-3 rounded-lg border border-[var(--app-border)] p-3">
					<div class="min-w-0">
						<p class="truncate text-sm">{run.issue_identifier} · {run.issue_title}</p>
						<p class="truncate text-xs text-[var(--color-text-tertiary)]">{run.automation_name}</p>
					</div>
					<div class="flex shrink-0 gap-2">
						<Button type="button" size="sm" variant="outline" onclick={() => void decide(run, false)}
							><X />{m['automations.dismiss']()}</Button
						>
						<Button type="button" size="sm" onclick={() => void decide(run, true)}
							><Check />{m['automations.approve']()}</Button
						>
					</div>
				</div>
			{/each}
		</div>
	{/if}

	{#if recent.length}
		<div class="space-y-1">
			<h3 class="text-xs font-semibold">{m['automations.recent']()}</h3>
			{#each recent as run (run.id)}
				<p class="truncate text-xs text-[var(--color-text-tertiary)]">
					{run.issue_identifier} · {run.automation_name} · {statusLabel(run.status)}{run.error ? ` — ${run.error}` : ''}
				</p>
			{/each}
		</div>
	{/if}
</section>
