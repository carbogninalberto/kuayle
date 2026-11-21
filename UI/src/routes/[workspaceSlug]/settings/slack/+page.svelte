<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { slack, slackInstallKey, type SlackStatus, type SlackChannel } from '$lib/api/slack';
	import { getWorkspace } from '$lib/api/workspaces';
	import { listTeams } from '$lib/api/teams';
	import { listProjects } from '$lib/api/projects';
	import { Button } from '$lib/components/ui/button';
	import { Label } from '$lib/components/ui/label';
	import PageTitle from '$lib/components/shared/PageTitle.svelte';
	import SelectField from '$lib/components/shared/SelectField.svelte';
	import { m } from '$lib/features/i18n/messages';
	import { appToast } from '$lib/features/toast/toast';

	const slug = $derived(page.params.workspaceSlug ?? '');
	let status = $state<SlackStatus>();
	let channels = $state<SlackChannel[]>([]);
	let targets = $state<{ value: string; label: string }[]>([]);
	let target = $state('');
	let existing = $derived(status?.bindings.find((b) => b.team_id === target || b.project_id === target));
	let channel = $derived(existing?.channel_id || 'none');
	let channelOptions = $derived([
		{ value: 'none', label: m['issue.history.none']() },
		...(existing && !channels.some((c) => c.id === existing?.channel_id)
			? [{ value: existing.channel_id, label: existing.channel_id, disabled: true }] : []),
		...channels.map((c) => ({ value: c.id, label: '#' + c.name }))
	]);
	let teams = new Set<string>();
	let busy = $state(true);
	let denied = $state(false);
	let failed = $state(false);

	async function load() {
		busy = true;
		failed = false;
		try {
			const workspace = await getWorkspace(slug);
			denied = !['owner', 'admin'].includes(workspace.current_user_role ?? '');
			if (denied) return;
			status = await slack.status(slug);
			if (!status.install || !status.configured) return;
			const [available, teamList, projects] = await Promise.all([slack.channels(slug), listTeams(slug), listProjects(slug)]);
			channels = available;
			teams = new Set(teamList.map((t) => t.id));
			targets = [
				...teamList.map((t) => ({ value: t.id, label: m['prefs.team']() + ' · ' + t.name })),
				...projects.map((p) => ({ value: p.id, label: m['prefs.project']() + ' · ' + p.name }))
			];
			target = targets[0]?.value ?? '';
		} catch {
			failed = true;
		} finally {
			busy = false;
		}
	}
	onMount(() => { void load(); });

	async function connect() {
		busy = true;
		try {
			const { url } = await slack.start(slug);
			const destination = new URL(url);
			if (destination.origin !== 'https://slack.com' || !destination.searchParams.get('state')) throw new Error();
			sessionStorage.setItem(slackInstallKey, JSON.stringify({ slug, state: destination.searchParams.get('state') }));
			window.location.assign(destination.href);
		} catch (err) {
			appToast.apiError(err, m['settings.backups.connection_failed']());
			busy = false;
		}
	}
	async function disconnect() {
		if (!confirm(m['slack.disconnect_confirm']())) return;
		busy = true;
		try {
			await slack.disconnect(slug);
			await load();
		} catch (err) {
			appToast.apiError(err, m['assistant.error']());
		} finally {
			busy = false;
		}
	}
	async function save() {
		if (!status || !target) return;
		busy = true;
		const binding = {
			...(teams.has(target) ? { team_id: target } : { project_id: target }),
			channel_id: channel === 'none' ? '' : channel
		};
		try {
			await slack.setBinding(slug, binding);
			status.bindings = status.bindings.filter((b) => b.team_id !== target && b.project_id !== target);
			if (binding.channel_id) status.bindings.push(binding);
			appToast.success(m['wiki.save_state.saved']());
		} catch (err) {
			appToast.apiError(err, m['assistant.error']());
		} finally {
			busy = false;
		}
	}
</script>

<PageTitle parts={['Slack', m['settings.title']()]} />
<div class="mx-auto max-w-3xl space-y-6 p-6">
	<h2 class="text-lg font-semibold">Slack</h2>
	{#if denied}
		<p>{m['settings.tokens.access_none']()}</p>
	{:else if status && !status.configured}
		<p>{m['assistant_settings.unavailable']()}</p>
	{:else if status?.install}
		<div class="flex flex-wrap items-center justify-between gap-3">
			<p class="min-w-0 break-words text-sm"><span class="text-muted-foreground">{m['settings.version.status_connected']()}</span> · {status.install.name}</p>
			<Button variant="outline" size="sm" disabled={busy} onclick={disconnect}>{m['settings.github.disconnect']()}</Button>
		</div>
		{#if !failed && target}
			<div class="grid gap-4 sm:grid-cols-2">
				<div class="min-w-0 space-y-2 [&>button]:w-full [&_[data-slot=select-value]]:truncate">
					<Label for="slack-target">{m['settings.labels.scope']()}</Label>
					<SelectField id="slack-target" options={targets} bind:value={() => target, (next) => { target = next; channel = existing?.channel_id || 'none'; }} disabled={busy} />
				</div>
				<div class="min-w-0 space-y-2 [&>button]:w-full [&_[data-slot=select-value]]:truncate">
					<Label for="slack-channel">{m['agent_messages.filter_channel']()}</Label>
					<SelectField id="slack-channel" options={channelOptions} bind:value={channel} disabled={busy} />
				</div>
			</div>
			{#if !channels.length}<p class="text-sm text-muted-foreground">{m['slack.invite_channel']()}</p>{/if}
			<Button size="sm" disabled={busy || channel === (existing?.channel_id || 'none')} onclick={save}>{m['common.save']()}</Button>
		{:else if !busy && !failed}
			<p class="text-sm text-muted-foreground">{m['slack.empty_targets']()}</p>
		{/if}
	{:else if status}
		<Button disabled={busy} onclick={connect}>{m['settings.import.connect_action']()}</Button>
	{:else if busy}
		<p role="status">{m['common.loading']()}</p>
	{/if}
	{#if failed}
		<p role="alert">{m['assistant.error']()}</p>
		<Button variant="outline" size="sm" disabled={busy} onclick={load}>{m['settings.tokens.retry']()}</Button>
	{/if}
</div>
