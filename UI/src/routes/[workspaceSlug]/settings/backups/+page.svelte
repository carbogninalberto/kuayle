<script lang="ts">
	import PageTitle from '$lib/components/shared/PageTitle.svelte';
	import SelectField from '$lib/components/shared/SelectField.svelte';
	import { onDestroy } from 'svelte';
	import { AlertTriangle, Copy, DatabaseBackup, Lock, RefreshCw, ShieldAlert } from '@lucide/svelte';
	import { m, getLocale } from '$lib/features/i18n/messages';
	import * as Alert from '$lib/components/ui/alert';
	import * as RadioGroup from '$lib/components/ui/radio-group';
	import * as Table from '$lib/components/ui/table';
	import * as Tooltip from '$lib/components/ui/tooltip';
	import { Badge } from '$lib/components/ui/badge';
	import { Button } from '$lib/components/ui/button';
	import { Checkbox } from '$lib/components/ui/checkbox';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Password } from '$lib/components/ui/password';
	import { Switch } from '$lib/components/ui/switch';
	import {
		getBackupSettings,
		listBackupRuns,
		runBackupNow,
		testBackupConnection,
		updateBackupSettings,
		verifyBackupRun,
		type BackupRun,
		type BackupSettings
	} from '$lib/api/backups';
	import {
		MAX_RETENTION,
		MIN_PASSPHRASE_LENGTH,
		MIN_RETENTION,
		buildConnectionTest,
		connectionTestNeedsKeys,
		buildSettingsPayload,
		formFromSettings,
		formatBytes,
		formatDuration,
		shortChecksum,
		shouldPoll,
		validateBackupForm,
		verifyBlock,
		type BackupForm,
		type BackupFormError
	} from '$lib/features/backups/backup-form';
	import { authState } from '$lib/features/auth/auth.state.svelte';
	import { copyText } from '$lib/features/assistant/message-text';
	import { appToast, getErrorMessage } from '$lib/features/toast/toast';

	const isSysadmin = $derived(authState.user?.is_sysadmin === true);

	let settings = $state<BackupSettings | null>(null);
	let form = $state<BackupForm | null>(null);
	let loadingSettings = $state(false);
	let loadError = $state('');
	let saving = $state(false);
	let saveError = $state('');
	let testing = $state(false);
	let connectionResult = $state<{ ok: boolean; text: string } | null>(null);
	let starting = $state(false);
	let runs = $state<BackupRun[]>([]);
	let loadingRuns = $state(false);
	let verifying = $state<string | null>(null);
	let loaded = false;
	let destroyed = false;
	let poll: ReturnType<typeof setTimeout> | null = null;
	// Stored keys only go to the saved endpoint (the API enforces it).
	const endpointNeedsKeys = $derived(
		!!form && !!settings && (settings.has_access_key || settings.has_secret_key) && connectionTestNeedsKeys(form, settings)
	);

	const weekdays = $derived.by(() => {
		const format = new Intl.DateTimeFormat(getLocale(), { weekday: 'long', timeZone: 'UTC' });
		// 2023-01-01 was a Sunday, matching weekday 0.
		return Array.from({ length: 7 }, (_, day) => ({ value: day, label: format.format(new Date(Date.UTC(2023, 0, 1 + day))) }));
	});
	const retentionOptions = $derived([
		{ value: 'count' as const, label: m['settings.backups.retention_count']() },
		{ value: 'days' as const, label: m['settings.backups.retention_days']() }
	]);

	$effect(() => {
		if (isSysadmin && !loaded) {
			loaded = true;
			void loadSettings();
			void loadRuns();
		}
	});

	onDestroy(() => {
		destroyed = true;
		clearPoll();
	});

	function clearPoll() {
		if (poll) clearTimeout(poll);
		poll = null;
	}

	async function loadSettings() {
		loadingSettings = true;
		loadError = '';
		try {
			settings = await getBackupSettings();
			form = formFromSettings(settings);
		} catch (err) {
			loadError = getErrorMessage(err, m['settings.backups.failed_load']());
		} finally {
			loadingSettings = false;
		}
	}

	async function loadRuns(quiet = false) {
		clearPoll();
		if (!quiet) loadingRuns = true;
		try {
			runs = await listBackupRuns(50);
		} catch (err) {
			if (!quiet) appToast.apiError(err, m['settings.backups.failed_load_runs']());
		} finally {
			loadingRuns = false;
		}
		// Follow running backups and uploads until they settle.
		if (shouldPoll(runs) && !destroyed) poll = setTimeout(() => void loadRuns(true), 3000);
	}

	function formErrorMessage(error: BackupFormError): string {
		switch (error) {
			case 'invalid_time':
				return m['settings.backups.invalid_time']();
			case 'missing_cron':
				return m['settings.backups.missing_cron']();
			case 'invalid_retention':
				return m['settings.backups.invalid_retention']();
			case 'short_passphrase':
				return m['settings.backups.short_passphrase']();
			case 'missing_passphrase':
				return m['settings.backups.missing_passphrase']();
		}
	}

	async function save() {
		if (!form || !settings) return;
		saveError = '';
		const invalid = validateBackupForm(form, settings);
		if (invalid) {
			saveError = formErrorMessage(invalid);
			return;
		}
		saving = true;
		try {
			settings = await updateBackupSettings(buildSettingsPayload(form));
			// Secrets never return to the form: inputs and clear flags reset.
			form = formFromSettings(settings);
			appToast.success(m['settings.backups.saved']());
		} catch (err) {
			saveError = getErrorMessage(err, m['settings.backups.failed_save']());
		} finally {
			saving = false;
		}
	}

	async function testConnection() {
		if (!form) return;
		testing = true;
		connectionResult = null;
		try {
			await testBackupConnection(buildConnectionTest(form));
			connectionResult = { ok: true, text: m['settings.backups.connection_ok']() };
		} catch (err) {
			connectionResult = { ok: false, text: getErrorMessage(err, m['settings.backups.connection_failed']()) };
		} finally {
			testing = false;
		}
	}

	async function backUpNow() {
		starting = true;
		try {
			await runBackupNow();
			appToast.success(m['settings.backups.backup_started']());
		} catch (err: any) {
			if (err?.status === 409 || err?.error?.code === 'BACKUP_RUNNING') appToast.info(m['settings.backups.backup_running']());
			else appToast.apiError(err, m['settings.backups.failed_run']());
		} finally {
			starting = false;
		}
		void loadRuns(true);
	}

	async function verify(run: BackupRun) {
		verifying = run.id;
		try {
			const updated = await verifyBackupRun(run.id);
			runs = runs.map((r) => (r.id === updated.id ? updated : r));
			if (updated.verify_error) appToast.error(m['settings.backups.verify_error']({ error: updated.verify_error }));
			else appToast.success(m['settings.backups.verify_ok']());
		} catch (err) {
			appToast.apiError(err, m['settings.backups.failed_verify']());
		} finally {
			verifying = null;
		}
	}

	async function copyChecksum(hash: string) {
		if (await copyText(hash)) appToast.success(m['settings.backups.copied']());
		else appToast.error(m['settings.backups.copy_failed']());
	}

	function formatTime(value?: string | null) {
		return value ? new Date(value).toLocaleString(getLocale(), { dateStyle: 'medium', timeStyle: 'short' }) : '';
	}

	function kindLabel(kind: BackupRun['kind']) {
		if (kind === 'scheduled') return m['settings.backups.kind_scheduled']();
		if (kind === 'manual') return m['settings.backups.kind_manual']();
		return m['settings.backups.kind_pre_upgrade']();
	}

	function statusLabel(status: BackupRun['status']) {
		if (status === 'running') return m['settings.backups.status_running']();
		if (status === 'succeeded') return m['settings.backups.status_succeeded']();
		return m['settings.backups.status_failed']();
	}

	function uploadLabel(status: BackupRun['upload_status']) {
		if (status === 'uploading') return m['settings.backups.upload_uploading']();
		if (status === 'uploaded') return m['settings.backups.upload_uploaded']();
		if (status === 'failed') return m['settings.backups.upload_failed']();
		return '';
	}

	function verifyBlockText(run: BackupRun) {
		const block = verifyBlock(run);
		if (block === 'pre_upgrade') return m['settings.backups.verify_pre_upgrade']();
		if (block === 'deleted') return m['settings.backups.removed']();
		if (block === 'not_succeeded') return m['settings.backups.verify_not_succeeded']();
		return '';
	}

	const sectionClass = 'mt-6 rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]';
	const rowClass = 'flex flex-col gap-3 px-5 py-4 sm:flex-row sm:items-center sm:justify-between';
	const fieldLabelClass = 'text-sm font-medium text-[var(--color-text-primary)]';
	const hintClass = 'text-xs text-[var(--color-text-tertiary)]';
</script>

<div class="mx-auto max-w-4xl px-8 py-10">
	<div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
		<div>
			<h1 class="text-2xl font-semibold text-[var(--color-text-primary)]">{m['settings.backups.title']()}</h1>
			<p class="mt-1 text-sm text-[var(--color-text-tertiary)]">{m['settings.backups.desc']()}</p>
		</div>
		{#if isSysadmin && form}
			<Button onclick={backUpNow} disabled={starting}>
				<DatabaseBackup size={14} />
				{starting ? m['settings.starting']() : m['settings.backups.back_up_now']()}
			</Button>
		{/if}
	</div>

	{#if !isSysadmin}
		<div class={sectionClass}>
			<div class="flex items-start gap-3 px-5 py-4">
				<AlertTriangle size={18} class="mt-0.5 shrink-0 text-[var(--color-text-tertiary)]" />
				<div>
					<h2 class="text-sm font-medium text-[var(--color-text-primary)]">{m['settings.backups.title']()}</h2>
					<p class="mt-1 text-xs text-[var(--color-text-tertiary)]">{m['settings.backups.non_sysadmin']()}</p>
				</div>
			</div>
		</div>
	{:else if loadError}
		<Alert.Root variant="destructive" class="mt-8">
			<AlertTriangle />
			<Alert.Description>{loadError}</Alert.Description>
			<Alert.Action>
				<Button variant="outline" size="sm" onclick={loadSettings}>{m['settings.tokens.retry']()}</Button>
			</Alert.Action>
		</Alert.Root>
	{:else if !form || !settings}
		<p class="mt-8 text-sm text-[var(--color-text-secondary)]">{loadingSettings ? m['settings.checking']() : ''}</p>
	{:else}
		<form
			onsubmit={(event) => {
				event.preventDefault();
				void save();
			}}
		>
			<!-- Schedule -->
			<section class={sectionClass} aria-labelledby="backup-schedule-heading">
				<div class={rowClass}>
					<div>
						<h2 id="backup-schedule-heading" class={fieldLabelClass}>
							<Label for="backup-enabled" class={fieldLabelClass}>{m['settings.backups.enabled']()}</Label>
						</h2>
						<p id="backup-enabled-desc" class={hintClass}>{m['settings.backups.enabled_desc']({ dir: settings.backup_dir })}</p>
					</div>
					<Switch id="backup-enabled" bind:checked={form.enabled} aria-describedby="backup-enabled-desc" />
				</div>
				<div class="border-t border-[var(--app-border)]"></div>
				<div class="grid gap-4 px-5 py-4">
					<div class="grid gap-2">
						<span id="backup-frequency-label" class={fieldLabelClass}>{m['settings.backups.frequency']()}</span>
						<RadioGroup.Root bind:value={form.schedule_kind} class="flex flex-wrap gap-4" aria-labelledby="backup-frequency-label">
							{#each [['daily', m['settings.backups.daily']()], ['weekly', m['settings.backups.weekly']()], ['cron', m['settings.backups.cron']()]] as [value, label] (value)}
								<div class="flex items-center gap-2">
									<RadioGroup.Item value={value} id="backup-kind-{value}" />
									<Label for="backup-kind-{value}" class="text-sm font-normal">{label}</Label>
								</div>
							{/each}
						</RadioGroup.Root>
					</div>
					{#if form.schedule_kind === 'cron'}
						<div class="grid gap-1.5">
							<Label for="backup-cron" class={fieldLabelClass}>{m['settings.backups.cron_expression']()}</Label>
							<Input id="backup-cron" bind:value={form.schedule_cron} placeholder="0 3 * * *" class="max-w-xs font-mono" aria-describedby="backup-cron-help" autocomplete="off" />
							<p id="backup-cron-help" class={hintClass}>{m['settings.backups.cron_help']()}</p>
						</div>
					{:else}
						<div class="flex flex-wrap gap-4">
							<div class="grid gap-1.5">
								<Label for="backup-time" class={fieldLabelClass}>{m['settings.backups.time_utc']()}</Label>
								<Input id="backup-time" bind:value={form.schedule_time} placeholder="02:00" inputmode="numeric" class="w-28 font-mono" autocomplete="off" />
							</div>
							{#if form.schedule_kind === 'weekly'}
								<div class="grid gap-1.5">
									<span id="backup-weekday-label" class={fieldLabelClass}>{m['settings.backups.weekday']()}</span>
									<SelectField bind:value={form.schedule_weekday} options={weekdays} aria-labelledby="backup-weekday-label" />
								</div>
							{/if}
						</div>
					{/if}
					{#if settings.enabled && settings.next_run_at}
						<p class="text-xs text-[var(--color-text-secondary)]" data-testid="backup-next-run">
							{m['settings.backups.next_run']({ time: formatTime(settings.next_run_at) })}
						</p>
					{/if}
				</div>
				<div class="border-t border-[var(--app-border)]"></div>
				<div class="flex flex-wrap items-end gap-4 px-5 py-4">
					<div class="grid gap-1.5">
						<span id="backup-retention-label" class={fieldLabelClass}>{m['settings.backups.retention']()}</span>
						<SelectField bind:value={form.retention_kind} options={retentionOptions} aria-labelledby="backup-retention-label" />
					</div>
					<div class="grid gap-1.5">
						<Label for="backup-retention-value" class={fieldLabelClass}>{m['settings.backups.retention_value']()}</Label>
						<Input id="backup-retention-value" type="number" min={MIN_RETENTION} max={MAX_RETENTION} step={1} bind:value={form.retention_value} class="w-28" />
					</div>
				</div>
			</section>

			<!-- S3 destination -->
			<section class={sectionClass} aria-labelledby="backup-s3-heading">
				<div class={rowClass}>
					<div>
						<h2 id="backup-s3-heading" class={fieldLabelClass}>
							<Label for="backup-s3-enabled" class={fieldLabelClass}>{m['settings.backups.s3']()}</Label>
						</h2>
						<p id="backup-s3-desc" class={hintClass}>{m['settings.backups.s3_desc']()}</p>
					</div>
					<Switch id="backup-s3-enabled" bind:checked={form.s3_enabled} aria-describedby="backup-s3-desc" />
				</div>
				{#if form.s3_enabled}
					<div class="border-t border-[var(--app-border)]"></div>
					<div class="grid gap-4 px-5 py-4 sm:grid-cols-2">
						<div class="grid gap-1.5 sm:col-span-2">
							<Label for="backup-s3-endpoint" class={fieldLabelClass}>{m['settings.backups.endpoint']()}</Label>
							<Input id="backup-s3-endpoint" bind:value={form.s3_endpoint} placeholder="https://s3.example.com" autocomplete="off" />
						</div>
						<div class="grid gap-1.5">
							<Label for="backup-s3-bucket" class={fieldLabelClass}>{m['settings.backups.bucket']()}</Label>
							<Input id="backup-s3-bucket" bind:value={form.s3_bucket} autocomplete="off" />
						</div>
						<div class="grid gap-1.5">
							<Label for="backup-s3-region" class={fieldLabelClass}>{m['settings.backups.region']()}</Label>
							<Input id="backup-s3-region" bind:value={form.s3_region} placeholder="us-east-1" autocomplete="off" />
						</div>
						<div class="grid gap-1.5 sm:col-span-2">
							<Label for="backup-s3-prefix" class={fieldLabelClass}>{m['settings.backups.prefix']()}</Label>
							<Input id="backup-s3-prefix" bind:value={form.s3_prefix} placeholder="kuayle/" autocomplete="off" />
						</div>
						<div class="grid gap-1.5">
							<Label for="backup-s3-access-key" class={fieldLabelClass}>{m['settings.backups.access_key']()}</Label>
							<Password
								id="backup-s3-access-key"
								bind:value={form.s3_access_key}
								placeholder={settings.has_access_key ? m['settings.backups.secret_saved']() : ''}
								disabled={form.clear_s3_credentials}
								autocomplete="off"
							/>
						</div>
						<div class="grid gap-1.5">
							<Label for="backup-s3-secret-key" class={fieldLabelClass}>{m['settings.backups.secret_key']()}</Label>
							<Password
								id="backup-s3-secret-key"
								bind:value={form.s3_secret_key}
								placeholder={settings.has_secret_key ? m['settings.backups.secret_saved']() : ''}
								disabled={form.clear_s3_credentials}
								autocomplete="new-password"
							/>
						</div>
						{#if settings.has_access_key || settings.has_secret_key}
							<div class="flex items-center gap-2 sm:col-span-2">
								<Checkbox id="backup-clear-s3" bind:checked={form.clear_s3_credentials} />
								<Label for="backup-clear-s3" class="text-sm font-normal">{m['settings.backups.clear_credentials']()}</Label>
							</div>
						{/if}
						<div class="flex flex-wrap items-center gap-3 sm:col-span-2">
							<Button
								type="button"
								variant="outline"
								size="sm"
								onclick={testConnection}
								disabled={testing}
								aria-describedby={endpointNeedsKeys ? 'backup-endpoint-needs-keys' : undefined}
							>
								{testing ? m['settings.backups.testing']() : m['settings.backups.test_connection']()}
							</Button>
							{#if endpointNeedsKeys}
								<p id="backup-endpoint-needs-keys" class="text-sm text-[var(--color-text-secondary)]">
									{m['settings.backups.endpoint_needs_keys']()}
								</p>
							{/if}
							{#if connectionResult}
								<p role="status" class="text-sm break-words {connectionResult.ok ? 'text-[var(--color-success)]' : 'text-destructive'}">
									{connectionResult.text}
								</p>
							{/if}
						</div>
					</div>
				{/if}
			</section>

			<!-- Encryption -->
			<section class={sectionClass} aria-labelledby="backup-encryption-heading">
				<div class={rowClass}>
					<div>
						<h2 id="backup-encryption-heading" class={fieldLabelClass}>
							<Label for="backup-encryption" class={fieldLabelClass}>{m['settings.backups.encryption']()}</Label>
						</h2>
						<p id="backup-encryption-desc" class={hintClass}>{m['settings.backups.encryption_desc']()}</p>
					</div>
					<Switch id="backup-encryption" bind:checked={form.encryption_enabled} aria-describedby="backup-encryption-desc" />
				</div>
				{#if form.encryption_enabled || settings.has_passphrase}
					<div class="border-t border-[var(--app-border)]"></div>
					<div class="grid gap-4 px-5 py-4">
						<Alert.Root>
							<ShieldAlert />
							<Alert.Title>{m['settings.backups.passphrase_warning_title']()}</Alert.Title>
							<Alert.Description>{m['settings.backups.passphrase_warning']()}</Alert.Description>
						</Alert.Root>
						<div class="grid max-w-sm gap-1.5">
							<Label for="backup-passphrase" class={fieldLabelClass}>{m['settings.backups.passphrase']()}</Label>
							<Password
								id="backup-passphrase"
								bind:value={form.passphrase}
								placeholder={settings.has_passphrase ? m['settings.backups.secret_saved']() : ''}
								minlength={MIN_PASSPHRASE_LENGTH}
								disabled={form.clear_passphrase}
								autocomplete="new-password"
								aria-describedby="backup-passphrase-hint"
							/>
							<p id="backup-passphrase-hint" class={hintClass}>{m['settings.backups.passphrase_hint']()}</p>
						</div>
						{#if settings.has_passphrase}
							<div class="flex items-center gap-2">
								<Checkbox id="backup-clear-passphrase" bind:checked={form.clear_passphrase} />
								<Label for="backup-clear-passphrase" class="text-sm font-normal">{m['settings.backups.clear_passphrase']()}</Label>
							</div>
						{/if}
					</div>
				{/if}
			</section>

			<div class="mt-4 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-end">
				{#if saveError}
					<p role="alert" class="text-sm break-words text-destructive sm:mr-auto">{saveError}</p>
				{/if}
				<Button type="submit" disabled={saving}>{saving ? m['settings.saving']() : m['settings.save']()}</Button>
			</div>
		</form>

		<!-- History -->
		<section class={sectionClass} aria-labelledby="backup-history-heading">
			<div class="flex items-center justify-between gap-3 border-b border-[var(--app-border)] px-5 py-4">
				<h2 id="backup-history-heading" class={fieldLabelClass}>{m['settings.backups.history']()}</h2>
				<Button variant="ghost" size="icon-sm" onclick={() => loadRuns()} disabled={loadingRuns} aria-label={m['settings.backups.history']()}>
					<RefreshCw class={loadingRuns ? 'animate-spin' : ''} />
				</Button>
			</div>
			{#if runs.length === 0}
				<p class="px-5 py-4 text-sm text-[var(--color-text-secondary)]">
					{loadingRuns ? m['settings.checking']() : m['settings.backups.no_runs']()}
				</p>
			{:else}
				<Tooltip.Provider delayDuration={200}>
					<Table.Root>
						<Table.Header>
							<Table.Row>
								<Table.Head class="pl-5">{m['settings.backups.col_started']()}</Table.Head>
								<Table.Head>{m['settings.backups.col_kind']()}</Table.Head>
								<Table.Head>{m['settings.backups.col_status']()}</Table.Head>
								<Table.Head>{m['settings.backups.col_size']()}</Table.Head>
								<Table.Head>{m['settings.backups.col_duration']()}</Table.Head>
								<Table.Head>{m['settings.backups.col_destination']()}</Table.Head>
								<Table.Head>{m['settings.backups.col_checksum']()}</Table.Head>
								<Table.Head>{m['settings.backups.col_details']()}</Table.Head>
								<Table.Head class="pr-5 text-right"><span class="sr-only">{m['settings.backups.col_actions']()}</span></Table.Head>
							</Table.Row>
						</Table.Header>
						<Table.Body>
							{#each runs as run (run.id)}
								{@const block = verifyBlock(run)}
								<Table.Row data-testid="backup-run">
									<Table.Cell class="pl-5 whitespace-nowrap">{formatTime(run.started_at)}</Table.Cell>
									<Table.Cell class="whitespace-nowrap">{kindLabel(run.kind)}</Table.Cell>
									<Table.Cell>
										<div class="flex flex-col items-start gap-1">
											<Badge variant={run.status === 'failed' ? 'destructive' : run.status === 'running' ? 'outline' : 'secondary'}>
												{statusLabel(run.status)}
											</Badge>
											{#if uploadLabel(run.upload_status)}
												<Badge variant={run.upload_status === 'failed' ? 'destructive' : 'outline'}>{uploadLabel(run.upload_status)}</Badge>
											{/if}
										</div>
									</Table.Cell>
									<Table.Cell class="whitespace-nowrap">{formatBytes(run.size_bytes)}</Table.Cell>
									<Table.Cell class="whitespace-nowrap">{formatDuration(run.duration_ms)}</Table.Cell>
									<Table.Cell class="max-w-48 truncate font-mono text-xs" title={run.destination}>{run.destination}</Table.Cell>
									<Table.Cell>
										{#if run.checksum_sha256}
											<div class="flex items-center gap-1">
												<code class="font-mono text-xs" title={run.checksum_sha256}>{shortChecksum(run.checksum_sha256)}</code>
												<Button
													variant="ghost"
													size="icon-xs"
													aria-label={m['settings.backups.copy_checksum']()}
													onclick={() => copyChecksum(run.checksum_sha256 ?? '')}
												>
													<Copy />
												</Button>
											</div>
										{/if}
									</Table.Cell>
									<Table.Cell class="max-w-64 text-xs whitespace-normal">
										<div class="flex flex-col items-start gap-1">
											{#if run.encrypted}
												<Badge variant="outline"><Lock />{m['settings.backups.encrypted']()}</Badge>
											{/if}
											{#if run.deleted_at}
												<Badge variant="outline">{m['settings.backups.removed']()}</Badge>
											{/if}
											{#if run.verified_at && !run.verify_error}
												<Badge variant="secondary">{m['settings.backups.verified_at']({ time: formatTime(run.verified_at) })}</Badge>
											{/if}
											{#if run.error}
												<p class="break-words text-destructive">{run.error}</p>
											{/if}
											{#if run.verify_error}
												<p class="break-words text-destructive">{m['settings.backups.verify_error']({ error: run.verify_error })}</p>
											{/if}
										</div>
									</Table.Cell>
									<Table.Cell class="pr-5 text-right">
										{#if block}
											<Tooltip.Root>
												<Tooltip.Trigger>
													{#snippet child({ props })}
														<!-- aria-disabled keeps the button focusable so the reason stays reachable. -->
														<Button
															{...props}
															variant="outline"
															size="sm"
															aria-disabled="true"
															class="cursor-not-allowed opacity-50"
														>
															{m['settings.backups.verify']()}<span class="sr-only">: {verifyBlockText(run)}</span>
														</Button>
													{/snippet}
												</Tooltip.Trigger>
												<Tooltip.Content class="max-w-64">{verifyBlockText(run)}</Tooltip.Content>
											</Tooltip.Root>
										{:else}
											<Button variant="outline" size="sm" onclick={() => verify(run)} disabled={verifying !== null}>
												{verifying === run.id ? m['settings.backups.verifying']() : m['settings.backups.verify']()}
											</Button>
										{/if}
									</Table.Cell>
								</Table.Row>
							{/each}
						</Table.Body>
					</Table.Root>
				</Tooltip.Provider>
			{/if}
		</section>
	{/if}
</div>

<PageTitle parts={[m['settings.backups.title'](), m['settings.title']()]} />
