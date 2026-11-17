<script lang="ts">
	import PageTitle from '$lib/components/shared/PageTitle.svelte';
	import SelectField from '$lib/components/shared/SelectField.svelte';
	import { onDestroy, onMount } from 'svelte';
	import { page } from '$app/state';
	import { getWorkspace } from '$lib/api/workspaces';
	import {
		imports,
		type ImportItem,
		type ImportJob,
		type ImportMapping,
		type ImportOptions,
		type ImportPreview
	} from '$lib/api/imports';
	import * as Alert from '$lib/components/ui/alert';
	import * as RadioGroup from '$lib/components/ui/radio-group';
	import * as Table from '$lib/components/ui/table';
	import { Badge } from '$lib/components/ui/badge';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Password } from '$lib/components/ui/password';
	import { Progress } from '$lib/components/ui/progress';
	import { appToast, getErrorMessage } from '$lib/features/toast/toast';
	import { buildImportMapping, mappingToTargets } from '$lib/features/imports/import-mapping';
	import { m } from '$lib/features/i18n/messages';
	import { TriangleAlert } from '@lucide/svelte';

	const slug = $derived(page.params.workspaceSlug ?? '');
	const sectionClass = 'mt-6 rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]';
	const rowClass = 'flex flex-col gap-2 px-5 py-4 sm:flex-row sm:items-center sm:justify-between';
	const fieldLabelClass = 'text-sm font-medium text-[var(--color-text-primary)]';
	const hintClass = 'text-xs text-[var(--color-text-tertiary)]';

	let canManage = $state(false);
	let loading = $state(true);
	let busy = $state(false);
	let error = $state('');
	let jobs = $state<ImportJob[]>([]);
	let selectedId = $state('');
	let options = $state<ImportOptions | null>(null);
	let preview = $state<ImportPreview | null>(null);
	let items = $state<ImportItem[]>([]);
	let pollTimer: ReturnType<typeof setTimeout> | null = null;
	let destroyed = false;

	let mode = $state<'cloud' | 'server'>('cloud');
	let baseUrl = $state('');
	let email = $state('');
	let token = $state('');
	let jql = $state('');
	let priorities = $state<Record<string, number>>({});
	let projectTeams = $state<Record<string, string>>({});
	let statusTargets = $state<Record<string, string>>({});
	let userTargets = $state<Record<string, string>>({});
	let epicMode = $state<'project' | 'parent'>('project');
	let sprintMode = $state<'cycle' | 'ignore'>('cycle');

	const job = $derived(jobs.find((candidate) => candidate.id === selectedId) ?? null);
	const progressValue = $derived(
		job && job.total > 0 ? Math.min(100, Math.round((job.processed / job.total) * 100)) : 0
	);
	const teamOptions = $derived((options?.teams ?? []).map((team) => ({ value: team.id, label: team.name })));
	const memberOptions = $derived(
		(options?.members ?? []).map((member) => ({ value: member.user_id, label: member.display_name || member.email }))
	);
	const priorityOptions = [
		{ value: 0, label: 'None' },
		{ value: 1, label: 'Urgent' },
		{ value: 2, label: 'High' },
		{ value: 3, label: 'Medium' },
		{ value: 4, label: 'Low' }
	];

	function clearPoll() {
		if (pollTimer) clearTimeout(pollTimer);
		pollTimer = null;
	}

	function formatTime(value: string | null): string {
		return value ? new Date(value).toLocaleString() : '—';
	}

	function applyMapping(mapping: ImportMapping | null | undefined) {
		const targets = mappingToTargets(mapping);
		projectTeams = targets.projectTeams;
		statusTargets = targets.statusTargets;
		userTargets = targets.userTargets;
		priorities = targets.priorities;
		epicMode = targets.epicMode;
		sprintMode = targets.sprintMode;
	}

	function buildMapping(): ImportMapping {
		return buildImportMapping({
			projectTeams, statusTargets, userTargets, priorities, epicMode, sprintMode
		});
	}

	function applyJob(updated: ImportJob) {
		jobs = jobs.map((candidate) => (candidate.id === updated.id ? updated : candidate));
		selectedId = updated.id;
		preview = updated.preview;
		if (updated.mapping) applyMapping(updated.mapping);
	}

	async function loadItems(id: string) {
		try {
			items = (await imports.items(slug, id)).items;
		} catch (err) {
			appToast.apiError(err, m['settings.import.failed_load']());
		}
	}

	function startPolling() {
		clearPoll();
		if (destroyed) return;
		pollTimer = setTimeout(async () => {
			if (!selectedId) return;
			try {
				const fresh = await imports.get(slug, selectedId);
				applyJob(fresh);
				if (['completed', 'failed', 'cancelled'].includes(fresh.status)) {
					await loadItems(fresh.id);
					return;
				}
			} catch (err) {
				appToast.apiError(err, m['settings.import.failed_load']());
			}
			startPolling();
		}, 2000);
	}

	async function load() {
		loading = true;
		error = '';
		try {
			const workspace = await getWorkspace(slug);
			canManage = workspace.current_user_role === 'owner' || workspace.current_user_role === 'admin';
			if (!canManage) return;
			const [list, targetOptions] = await Promise.all([imports.list(slug), imports.options(slug)]);
			jobs = list.imports;
			options = targetOptions;
			if (jobs.length && !selectedId) {
				const first = jobs[0];
				applyJob(first);
				if (first.status === 'running' || first.status === 'queued') startPolling();
				if (['completed', 'failed', 'cancelled'].includes(first.status)) await loadItems(first.id);
			}
		} catch (err) {
			error = getErrorMessage(err, m['settings.import.failed_load']());
		} finally {
			loading = false;
		}
	}

	async function selectJob(id: string) {
		clearPoll();
		selectedId = id;
		preview = null;
		items = [];
		try {
			const found = await imports.get(slug, id);
			applyJob(found);
			if (found.status === 'running' || found.status === 'queued') startPolling();
			if (['completed', 'failed', 'cancelled'].includes(found.status)) await loadItems(id);
		} catch (err) {
			error = getErrorMessage(err, m['settings.import.failed_load']());
		}
	}

	async function connect() {
		busy = true;
		error = '';
		try {
			const cloud = mode === 'cloud';
			const created = await imports.create(slug, {
				source: 'jira',
				source_site: cloud ? 'cloud' : baseUrl,
				config: { cloud, jql },
				credentials: cloud ? { base_url: 'https://atlassian.net', email, token } : { base_url: baseUrl, bearer_token: token }
			});
			jobs = [created, ...jobs];
			selectedId = created.id;
			preview = null;
			items = [];
			applyMapping(created.mapping);
			appToast.success(m['settings.import.connect']());
		} catch (err) {
			error = getErrorMessage(err, m['settings.import.failed_load']());
		} finally {
			busy = false;
		}
	}

	async function runPreview() {
		if (!job) return;
		busy = true;
		error = '';
		try {
			preview = await imports.preview(slug, job.id);
			applyJob(await imports.get(slug, job.id));
			appToast.success(m['settings.import.preview']());
		} catch (err) {
			error = getErrorMessage(err, m['settings.import.failed_load']());
		} finally {
			busy = false;
		}
	}

	async function saveMapping() {
		if (!job) return;
		busy = true;
		error = '';
		try {
			applyJob(await imports.mapping(slug, job.id, buildMapping()));
			appToast.success(m['settings.import.save_mapping']());
		} catch (err) {
			error = getErrorMessage(err, m['settings.import.failed_load']());
		} finally {
			busy = false;
		}
	}

	async function start() {
		if (!job) return;
		busy = true;
		error = '';
		try {
			applyJob(await imports.start(slug, job.id));
			startPolling();
		} catch (err) {
			error = getErrorMessage(err, m['settings.import.failed_load']());
		} finally {
			busy = false;
		}
	}

	async function cancel() {
		if (!job) return;
		clearPoll();
		busy = true;
		try {
			applyJob(await imports.cancel(slug, job.id));
			await loadItems(job.id);
		} catch (err) {
			appToast.apiError(err, m['settings.import.failed_load']());
		} finally {
			busy = false;
		}
	}

	function statusOptions(teamId: string) {
		return (options?.statuses[teamId] ?? []).map((status) => ({ value: status.id, label: status.name }));
	}

	const modeOptions = [
		{ value: 'cloud' as const, label: m['settings.import.cloud']() },
		{ value: 'server' as const, label: m['settings.import.server']() }
	];
	const unassignedOption = [{ value: '', label: m['settings.import.unassigned']() }];

	onMount(load);
	onDestroy(() => {
		destroyed = true;
		clearPoll();
	});
</script>

<div class="mx-auto w-full max-w-4xl space-y-6 px-4 py-8 sm:px-8 sm:py-10">
	<h1 class="text-2xl font-semibold text-[var(--color-text-primary)]">{m['settings.import.title']()}</h1>
	<p class="text-sm text-[var(--color-text-secondary)]">{m['settings.import.subtitle']()}</p>

	{#if loading}
		<div class="flex h-40 items-center justify-center text-[var(--color-text-tertiary)]">…</div>
	{:else if !canManage}
		<p class="rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-4 py-3 text-sm text-[var(--color-text-secondary)]">
			{m['settings.import.admin_only']()}
		</p>
	{:else}
		{#if error}
			<Alert.Root variant="destructive">
				<TriangleAlert />
				<Alert.Description>{error}</Alert.Description>
			</Alert.Root>
		{/if}

		<section class={sectionClass}>
			<div class="px-5 py-4">
				<h2 class="text-base font-medium text-[var(--color-text-primary)]">{m['settings.import.connect']()}</h2>
			</div>
			<div class="space-y-4 border-t border-[var(--app-border)] px-5 py-4">
				<div class="grid gap-4 sm:grid-cols-2">
					<div class="space-y-1.5">
						<Label for="import-mode">{m['settings.import.cloud']()} / {m['settings.import.server']()}</Label>
						<SelectField id="import-mode" bind:value={mode} options={modeOptions} />
					</div>
					<div class="space-y-1.5">
						<Label for="import-base-url">{m['settings.import.base_url']()}</Label>
						<Input id="import-base-url" bind:value={baseUrl} disabled={mode === 'cloud'} placeholder="https://jira.example.com" />
					</div>
					<div class="space-y-1.5">
						<Label for="import-email">{m['settings.import.email']()}</Label>
						<Input id="import-email" bind:value={email} disabled={mode !== 'cloud'} autocomplete="off" />
					</div>
					<div class="space-y-1.5">
						<Label for="import-token">{m['settings.import.token']()}</Label>
						<Password id="import-token" bind:value={token} autocomplete="off" />
					</div>
				</div>
				<div class="space-y-1.5">
					<Label for="import-jql">{m['settings.import.jql']()}</Label>
					<Input id="import-jql" bind:value={jql} placeholder="project = KEY" />
				</div>
				<div>
					<Button onclick={connect} disabled={busy || !token}>{m['settings.import.connect_action']()}</Button>
				</div>
			</div>
		</section>

		{#if jobs.length}
			<section class={sectionClass}>
				<div class="space-y-1 border-b border-[var(--app-border)] px-5 py-4">
					<h2 class="text-base font-medium text-[var(--color-text-primary)]">{m['settings.import.existing_job']()}</h2>
					<div class="flex flex-wrap gap-2">
						{#each jobs as existing (existing.id)}
							<Button variant={existing.id === selectedId ? 'default' : 'outline'} size="sm" onclick={() => selectJob(existing.id)}>
								{existing.source} · {existing.status}
							</Button>
						{/each}
					</div>
				</div>

				{#if job}
					<div class="space-y-4 px-5 py-4">
						<div class="flex flex-wrap items-center justify-between gap-2">
							<div class="flex items-center gap-2">
								<span class="text-sm text-[var(--color-text-secondary)]">{job.status}</span>
								<Badge variant={job.status === 'failed' ? 'destructive' : 'secondary'}>{job.processed}/{job.total}</Badge>
							</div>
							{#if job.status === 'queued' || job.status === 'running'}
								<Button variant="outline" size="sm" onclick={cancel} disabled={busy}>{m['settings.import.cancel']()}</Button>
							{/if}
						</div>
						{#if job.status === 'running' || job.status === 'queued'}
							<div class="space-y-1">
								<p class={hintClass}>{m['settings.import.progress']()}</p>
								<Progress value={progressValue} max={100} />
							</div>
						{/if}
						{#if job.error}
							<p class="text-sm text-destructive">{job.error}</p>
						{/if}
						<div class="flex flex-wrap gap-2">
							<Button variant="outline" size="sm" onclick={runPreview} disabled={busy}>{m['settings.import.preview_action']()}</Button>
							{#if job.status === 'previewed'}
								<Button size="sm" onclick={start} disabled={busy}>{m['settings.import.start']()}</Button>
							{/if}
						</div>
					</div>

					{#if preview}
						<div class="space-y-3 border-t border-[var(--app-border)] px-5 py-4">
							<h3 class={fieldLabelClass}>{m['settings.import.counts']()}</h3>
							<div class="flex flex-wrap gap-2 text-sm text-[var(--color-text-secondary)]">
								{#each Object.entries(preview.counts) as [key, count]}
									<Badge variant="outline">{key}: {count}</Badge>
								{/each}
							</div>
							{#if preview.unmapped_fields.length}
								<h3 class={fieldLabelClass}>{m['settings.import.unmapped']()}</h3>
								<ul class="list-disc space-y-1 pl-5 text-sm text-[var(--color-text-secondary)]">
									{#each preview.unmapped_fields as field}<li>{field}</li>{/each}
								</ul>
							{/if}
						</div>

						{#if job.status === 'previewed' || job.status === 'draft'}
							<div class="space-y-4 border-t border-[var(--app-border)] px-5 py-4">
								<h3 class={fieldLabelClass}>{m['settings.import.mapping']()}</h3>

								{#each preview.projects as sourceProject}
									<div class={rowClass}>
										<span class="text-sm">{m['settings.import.project']()}: {sourceProject.name} ({sourceProject.key})</span>
										<div class="w-full sm:w-64">
											<SelectField bind:value={projectTeams[sourceProject.external_id]} options={[...unassignedOption, ...teamOptions]} placeholder={m['settings.import.unassigned']()} />
										</div>
									</div>
								{/each}

								{#each preview.statuses as sourceStatus}
									<div class={rowClass}>
										<span class="text-sm">{m['settings.import.status']()}: {sourceStatus}</span>
										<div class="w-full sm:w-64">
											<SelectField bind:value={statusTargets[sourceStatus.toLowerCase()]} options={statusOptions(Object.values(projectTeams).find(Boolean) ?? '')} placeholder={m['settings.import.unassigned']()} />
										</div>
									</div>
								{/each}

								{#each preview.users as sourceUser}
									<div class={rowClass}>
										<span class="text-sm">{m['settings.import.user']()}: {sourceUser.display_name || sourceUser.email}</span>
										<div class="w-full sm:w-64">
											<SelectField bind:value={userTargets[sourceUser.email.toLowerCase()]} options={[...unassignedOption, ...memberOptions]} placeholder={m['settings.import.unassigned']()} />
										</div>
									</div>
								{/each}

								{#each preview.priorities as sourcePriority}
									<div class={rowClass}>
										<span class="text-sm">{m['settings.import.priority']()}: {sourcePriority}</span>
										<div class="w-full sm:w-64">
											<SelectField bind:value={priorities[sourcePriority.toLowerCase()]} options={priorityOptions} placeholder={m['settings.import.unassigned']()} />
										</div>
									</div>
								{/each}

								{#each preview.labels as sourceLabel}
									<div class={rowClass}>
										<span class="text-sm">{m['settings.import.labels']()}: {sourceLabel}</span>
										<span class={hintClass}>{m['settings.import.create_label']()}</span>
									</div>
								{/each}

								<div class={rowClass}>
									<span class="text-sm">{m['settings.import.epic_mode']()}</span>
									<RadioGroup.Root bind:value={epicMode} orientation="horizontal">
										<div class="flex items-center gap-2">
											<RadioGroup.Item value="project" id="epic-project" />
											<Label for="epic-project">{m['settings.import.epic_project']()}</Label>
										</div>
										<div class="flex items-center gap-2">
											<RadioGroup.Item value="parent" id="epic-parent" />
											<Label for="epic-parent">{m['settings.import.epic_parent']()}</Label>
										</div>
									</RadioGroup.Root>
								</div>
								<div class={rowClass}>
									<span class="text-sm">{m['settings.import.sprint_mode']()}</span>
									<RadioGroup.Root bind:value={sprintMode} orientation="horizontal">
										<div class="flex items-center gap-2">
											<RadioGroup.Item value="cycle" id="sprint-cycle" />
											<Label for="sprint-cycle">{m['settings.import.sprint_cycle']()}</Label>
										</div>
										<div class="flex items-center gap-2">
											<RadioGroup.Item value="ignore" id="sprint-ignore" />
											<Label for="sprint-ignore">{m['settings.import.sprint_ignore']()}</Label>
										</div>
									</RadioGroup.Root>
								</div>

								<div>
									<Button variant="outline" size="sm" onclick={saveMapping} disabled={busy}>{m['settings.import.save_mapping']()}</Button>
								</div>
							</div>
						{/if}
					{/if}

					{#if ['completed', 'failed', 'cancelled'].includes(job.status)}
						<div class="space-y-3 border-t border-[var(--app-border)] px-5 py-4">
							<h3 class={fieldLabelClass}>{m['settings.import.error_report']()}</h3>
							{#if items.length === 0}
								<p class={hintClass}>{m['settings.import.no_items']()}</p>
							{:else}
								<Table.Root>
									<Table.Header>
										<Table.Row>
											<Table.Head>{m['settings.import.columns.item']()}</Table.Head>
											<Table.Head>{m['settings.import.columns.outcome']()}</Table.Head>
											<Table.Head>{m['settings.import.columns.reason']()}</Table.Head>
											<Table.Head>{m['settings.import.col.created']()}</Table.Head>
										</Table.Row>
									</Table.Header>
									<Table.Body>
										{#each items as item}
											<Table.Row data-testid="import-item">
												<Table.Cell>{item.external_type} {item.external_id}</Table.Cell>
												<Table.Cell><Badge variant={item.outcome === 'failed' ? 'destructive' : 'secondary'}>{item.outcome}</Badge></Table.Cell>
												<Table.Cell>{item.reason}</Table.Cell>
												<Table.Cell>{formatTime(item.created_at)}</Table.Cell>
											</Table.Row>
										{/each}
									</Table.Body>
								</Table.Root>
							{/if}
						</div>
					{/if}
				{/if}
			</section>
		{:else}
			<p class={hintClass}>{m['settings.import.no_jobs']()}</p>
		{/if}
	{/if}
</div>

<PageTitle parts={[m['settings.import.title'](), m['settings.title']()]} />
