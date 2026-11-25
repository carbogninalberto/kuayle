<script lang="ts">
	import { LoaderCircle } from '@lucide/svelte';
	import * as Alert from '$lib/components/ui/alert';
	import * as AlertDialog from '$lib/components/ui/alert-dialog';
	import { Button } from '$lib/components/ui/button';
	import {
		applyInstanceRestart,
		getInstanceConfig,
		validateInstanceRestart,
		type InstanceConfig,
		type InstanceRestartChange,
		type InstanceRestartIssue,
		type InstanceRestartRun
	} from '$lib/api/instance-config';
	import { m } from '$lib/features/i18n/messages';
	import { appToast } from '$lib/features/toast/toast';
	import { issuesOfError, pendingNames, proposalOf, RESTART_POLL_MS, runInProgress, type Pending } from './restart';

	// The panel above the list: the drafted changes of restart-only settings
	// with their dry run and "Apply and restart", and, once applied, the state
	// of the restart until the API decides whether it came back healthy.
	let {
		config,
		pending,
		general,
		ondiscard,
		onissues,
		onconfig
	}: {
		config: InstanceConfig;
		pending: Pending;
		/** Problems of the configuration as a whole, from the last dry run. */
		general: string[];
		ondiscard: () => void;
		onissues: (issues: InstanceRestartIssue[]) => void;
		onconfig: (next: InstanceConfig) => void;
	} = $props();

	let checking = $state(false);
	let applying = $state(false);
	let checked = $state(false);
	let confirm = $state<InstanceRestartChange[] | null>(null);
	let dismissed = $state('');

	const count = $derived(pendingNames(pending).length);
	const run = $derived(config.restart.run);
	const waiting = $derived(runInProgress(run));
	const busy = $derived(checking || applying);
	const lastFailed = $derived(run?.status === 'rolled_back' && dismissed !== run.id ? run : undefined);

	// A draft changed since the last check has not been checked.
	$effect(() => {
		void proposalOf(pending);
		checked = false;
	});

	// While the API restarts, ask until it answers with a decided run. It does
	// not answer while it is down: the failures are the expected part.
	$effect(() => {
		const id = run?.id;
		if (!waiting || !id) return;
		let stopped = false;
		const timer = setInterval(async () => {
			try {
				const next = await getInstanceConfig();
				if (stopped) return;
				const decided = next.restart.run;
				if (decided?.id === id && runInProgress(decided)) return;
				stopped = true;
				clearInterval(timer);
				onconfig(next);
				if (decided?.status === 'confirmed') appToast.success(m['settings.instance_config.restart_done']());
			} catch {
				// The API is restarting.
			}
		}, RESTART_POLL_MS);
		return () => {
			stopped = true;
			clearInterval(timer);
		};
	});

	async function plan() {
		const result = await validateInstanceRestart(proposalOf(pending));
		onissues(result.issues);
		return result;
	}

	async function check() {
		if (busy) return;
		checking = true;
		try {
			const result = await plan();
			checked = result.valid;
			if (result.valid) appToast.success(m['settings.instance_config.restart_check_ok']());
			else appToast.error(m['settings.instance_config.restart_check_failed']());
		} catch (err) {
			appToast.apiError(err, m['settings.instance_config.restart_apply_failed']());
		} finally {
			checking = false;
		}
	}

	// The apply checks again; the dialog is the last look at what will change.
	async function review() {
		if (busy) return;
		checking = true;
		try {
			const result = await plan();
			checked = result.valid;
			if (result.valid) confirm = result.changes;
			else appToast.error(m['settings.instance_config.restart_check_failed']());
		} catch (err) {
			appToast.apiError(err, m['settings.instance_config.restart_apply_failed']());
		} finally {
			checking = false;
		}
	}

	async function apply() {
		if (applying) return;
		applying = true;
		try {
			const started: InstanceRestartRun = await applyInstanceRestart(proposalOf(pending));
			confirm = null;
			ondiscard();
			onconfig({ ...config, restart: { ...config.restart, run: started } });
		} catch (err) {
			confirm = null;
			const issues = issuesOfError(err);
			if (issues.length > 0) onissues(issues);
			appToast.apiError(err, m['settings.instance_config.restart_apply_failed']());
		} finally {
			applying = false;
		}
	}

	const shown = (value: string) => value || m['settings.instance_config.restart_empty']();
</script>

{#if waiting}
	<Alert.Root data-testid="instance-restart-progress" aria-live="polite">
		<LoaderCircle class="animate-spin" aria-hidden="true" />
		<Alert.Title>{m['settings.instance_config.restart_progress_title']()}</Alert.Title>
		<Alert.Description>{m['settings.instance_config.restart_progress_desc']()}</Alert.Description>
	</Alert.Root>
{:else}
	{#if lastFailed}
		<Alert.Root variant="destructive" data-testid="instance-restart-rolled-back">
			<Alert.Title>{m['settings.instance_config.restart_rolled_back_title']()}</Alert.Title>
			<Alert.Description>
				{m['settings.instance_config.restart_rolled_back_desc']({ reason: lastFailed.failure ?? '' })}
			</Alert.Description>
			<Alert.Action>
				<Button variant="outline" size="sm" onclick={() => (dismissed = lastFailed.id)}>
					{m['settings.instance_config.restart_dismiss']()}
				</Button>
			</Alert.Action>
		</Alert.Root>
	{/if}
	{#if config.restart.notice}
		<Alert.Root data-testid="instance-restart-notice">
			<Alert.Title>{m['settings.instance_config.restart_notice_title']()}</Alert.Title>
			<Alert.Description>{config.restart.notice}</Alert.Description>
		</Alert.Root>
	{/if}
	{#if count > 0}
		<div class="sticky top-2 z-10" data-testid="instance-restart-panel">
			<Alert.Root class="shadow-sm">
				<Alert.Title>{m['settings.instance_config.restart_pending_title']({ count })}</Alert.Title>
				<Alert.Description class="grid gap-2">
					<span>{m['settings.instance_config.restart_pending_desc']()}</span>
					{#if general.length > 0}
						<ul class="list-disc pl-4 text-destructive" data-testid="instance-restart-general">
							{#each general as message (message)}
								<li>{message}</li>
							{/each}
						</ul>
					{/if}
					{#if !config.restart.supported}
						<span class="text-muted-foreground" data-testid="instance-restart-unsupported">
							{m['settings.instance_config.restart_unsupported']()}
						</span>
					{/if}
					<div class="flex flex-wrap items-center gap-2">
						<Button variant="outline" size="sm" disabled={busy} onclick={check}>
							{checking ? m['settings.instance_config.restart_checking']() : m['settings.instance_config.restart_check']()}
						</Button>
						<Button size="sm" disabled={busy || !config.restart.supported} onclick={review}>
							{m['settings.instance_config.restart_apply']()}
						</Button>
						<Button variant="ghost" size="sm" disabled={busy} onclick={ondiscard}>
							{m['settings.instance_config.restart_discard_all']()}
						</Button>
						{#if checked}
							<span class="text-xs text-muted-foreground" aria-live="polite">
								{m['settings.instance_config.restart_check_ok']()}
							</span>
						{/if}
					</div>
				</Alert.Description>
			</Alert.Root>
		</div>
	{/if}
{/if}

<AlertDialog.Root
	open={confirm !== null}
	onOpenChange={(open) => {
		if (!open && !applying) confirm = null;
	}}
>
	<AlertDialog.Content>
		<AlertDialog.Header>
			<AlertDialog.Title>{m['settings.instance_config.restart_confirm_title']()}</AlertDialog.Title>
			<AlertDialog.Description>{m['settings.instance_config.restart_confirm_desc']()}</AlertDialog.Description>
		</AlertDialog.Header>
		<ul class="grid gap-1 text-sm" data-testid="instance-restart-changes">
			{#each confirm ?? [] as change (change.name)}
				<li class="break-all">
					<code class="font-mono font-medium">{change.name}</code>:
					<span class="font-mono">{shown(change.from)}</span> → <span class="font-mono">{shown(change.to)}</span>
				</li>
			{/each}
		</ul>
		<AlertDialog.Footer>
			<AlertDialog.Cancel disabled={applying}>{m['settings.cancel']()}</AlertDialog.Cancel>
			<Button onclick={apply} disabled={applying}>
				{m['settings.instance_config.restart_apply']()}
			</Button>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>
