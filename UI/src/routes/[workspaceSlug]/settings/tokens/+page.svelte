<script lang="ts">
	import { onMount } from 'svelte';
	import { beforeNavigate } from '$app/navigation';
	import {
		listTokens,
		createToken,
		revokeToken,
		type PersonalAccessToken,
		type CreatedPersonalAccessToken
	} from '$lib/api/tokens';
	import { listWorkspaces } from '$lib/api/workspaces';
	import type { Workspace } from '$lib/types/workspace';
	import {
		RESOURCE_ROWS,
		PRESET_KEYS,
		PRESET_SCOPES,
		applyLevel,
		levelForScopes,
		matchPreset,
		type AccessLevel,
		type PresetKey,
		type ResourceRow
	} from '$lib/features/tokens/scope-model';
	import * as Dialog from '$lib/components/ui/dialog';
	import * as AlertDialog from '$lib/components/ui/alert-dialog';
	import * as Select from '$lib/components/ui/select';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Badge } from '$lib/components/ui/badge';
	import { Checkbox } from '$lib/components/ui/checkbox';
	import EmptyState from '$lib/components/shared/EmptyState.svelte';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/paraglide/messages.js';
	import { getLocale } from '$lib/paraglide/runtime.js';
	import { expiresAt, localDateString, type ExpiryChoice } from '$lib/features/tokens/expiry';
	import { formatDate, formatRelativeTime } from '$lib/utils/format';
	import { Plus, Trash2, KeyRound, Copy, ChevronDown, TriangleAlert } from 'lucide-svelte';

	const tokenMessages: Record<string, () => string> = {
		preset_read_only: m['settings.tokens.preset_read_only'],
		preset_read_only_desc: m['settings.tokens.preset_read_only_desc'],
		preset_developer: m['settings.tokens.preset_developer'],
		preset_developer_desc: m['settings.tokens.preset_developer_desc'],
		preset_triage_bot: m['settings.tokens.preset_triage_bot'],
		preset_triage_bot_desc: m['settings.tokens.preset_triage_bot_desc'],
		preset_full_access: m['settings.tokens.preset_full_access'],
		preset_full_access_desc: m['settings.tokens.preset_full_access_desc'],
		preset_custom: m['settings.tokens.preset_custom'],
		preset_full_warning: m['settings.tokens.preset_full_warning'],
		access_none: m['settings.tokens.access_none'],
		access_read: m['settings.tokens.access_read'],
		access_write: m['settings.tokens.access_write'],
		res_issues: m['settings.tokens.res_issues'],
		res_comments: m['settings.tokens.res_comments'],
		res_projects: m['settings.tokens.res_projects'],
		res_cycles: m['settings.tokens.res_cycles'],
		res_labels: m['settings.tokens.res_labels'],
		res_teams: m['settings.tokens.res_teams'],
		res_members: m['settings.tokens.res_members'],
		res_workspace: m['settings.tokens.res_workspace'],
		res_templates: m['settings.tokens.res_templates'],
		res_views: m['settings.tokens.res_views'],
		res_analytics: m['settings.tokens.res_analytics'],
		res_notifications: m['settings.tokens.res_notifications'],
		res_account: m['settings.tokens.res_account'],
		res_assets: m['settings.tokens.res_assets'],
		res_dev_machines: m['settings.tokens.res_dev_machines'],
		comments_write_note: m['settings.tokens.comments_write_note'],
		expiry_30d: m['settings.tokens.expiry_30d'],
		expiry_90d: m['settings.tokens.expiry_90d'],
		expiry_1y: m['settings.tokens.expiry_1y'],
		expiry_custom: m['settings.tokens.expiry_custom'],
		expiry_never: m['settings.tokens.expiry_never'],
		expiry_invalid: m['settings.tokens.expiry_invalid'],
		expiry_never_warning: m['settings.tokens.expiry_never_warning'],
	};
	const EXPIRY_CHOICES: ExpiryChoice[] = ['30d', '90d', '1y', 'custom', 'never'];

	let tokens = $state<PersonalAccessToken[]>([]);
	let loading = $state(true);
	let loadFailed = $state(false);
	let workspacesLoading = $state(false);
	let workspacesFailed = $state(false);
	let showCreate = $state(false);
	let workspaces = $state<Workspace[]>([]);

	// Create form state
	let name = $state('');
	let selectedScopes = $state<Set<string>>(new Set(PRESET_SCOPES.read_only));
	let selectedWorkspaces = $state<string[]>([]);
	let expiryChoice = $state<ExpiryChoice>('30d');
	let customDate = $state('');
	let showAdvanced = $state(false);
	let creating = $state(false);

	let createdToken = $state<CreatedPersonalAccessToken | null>(null);
	let showCloseConfirm = $state(false);
	let tokenToRevoke = $state<PersonalAccessToken | null>(null);
	let revoking = $state(false);

	const preset = $derived(matchPreset(selectedScopes));
	const mainRows = RESOURCE_ROWS.filter((row) => !row.advanced);
	const advancedRows = RESOURCE_ROWS.filter((row) => row.advanced);
	const todayStr = $derived(localDateString(new Date()));
	const customDateValid = $derived(expiresAt('custom', customDate) !== null);
	const canSubmit = $derived(
		name.trim().length > 0 &&
			selectedScopes.size > 0 &&
			(expiryChoice !== 'custom' || customDateValid) &&
			!creating &&
			!workspacesLoading &&
			!workspacesFailed
	);

	beforeNavigate(({ cancel, willUnload }) => {
		if (creating || createdToken) {
			cancel();
			if (createdToken && !willUnload) showCloseConfirm = true;
		}
	});

	onMount(() => {
		loadTokens();
	});

	async function loadTokens() {
		loading = true;
		loadFailed = false;
		try {
			tokens = await listTokens();
		} catch (err: any) {
			loadFailed = true;
			appToast.apiError(err, m['settings.tokens.failed_load']());
		} finally {
			loading = false;
		}
	}

	async function openCreate() {
		resetForm();
		showCreate = true;
		if (workspaces.length === 0 || workspacesFailed) {
			workspacesLoading = true;
			workspacesFailed = false;
			try {
				workspaces = await listWorkspaces();
			} catch (err: any) {
				workspacesFailed = true;
				appToast.apiError(err, m['settings.tokens.failed_load_workspaces']());
			} finally {
				workspacesLoading = false;
			}
		}
	}

	function resetForm() {
		name = '';
		selectedScopes = new Set(PRESET_SCOPES.read_only);
		selectedWorkspaces = [];
		expiryChoice = '30d';
		customDate = '';
		showAdvanced = false;
	}

	async function handleCreate(e: Event) {
		e.preventDefault();
		if (!canSubmit) return;
		const expiration = expiresAt(expiryChoice, customDate);
		if (expiration === null) return;
		creating = true;
		try {
			const created = await createToken({
				name: name.trim(),
				scopes: [...selectedScopes].sort(),
				...(selectedWorkspaces.length > 0 ? { workspace_slugs: selectedWorkspaces } : {}),
				...(expiryChoice === 'never' ? {} : { expires_at: expiration })
			});
			const { token: _plaintext, ...listed } = created;
			tokens = [listed, ...tokens];
			createdToken = created;
			showCloseConfirm = false;
			showCreate = false;
			resetForm();
		} catch (err: any) {
			appToast.apiError(err, m['settings.tokens.failed_create']());
		} finally {
			creating = false;
		}
	}

	async function handleRevoke() {
		if (!tokenToRevoke || revoking) return;
		const tokenId = tokenToRevoke.id;
		revoking = true;
		try {
			await revokeToken(tokenId);
			tokens = tokens.filter((t) => t.id !== tokenId);
			appToast.success(m['settings.tokens.revoked']());
			tokenToRevoke = null;
		} catch (err: any) {
			appToast.apiError(err, m['settings.tokens.failed_revoke']());
		} finally {
			revoking = false;
		}
	}

	async function copyCreatedToken() {
		if (!createdToken) return;
		try {
			await navigator.clipboard.writeText(createdToken.token);
			appToast.success(m['settings.tokens.copied']());
		} catch {
			appToast.error(m['settings.tokens.failed_copy']());
		}
	}

	function confirmTokenSaved() {
		showCloseConfirm = false;
		createdToken = null;
	}

	function setRowLevel(row: ResourceRow, level: AccessLevel) {
		selectedScopes = applyLevel(selectedScopes, row, level);
	}

	function applyPreset(key: PresetKey) {
		selectedScopes = new Set(PRESET_SCOPES[key]);
	}

	function toggleWorkspace(slug: string) {
		if (selectedWorkspaces.includes(slug)) {
			selectedWorkspaces = selectedWorkspaces.filter((s) => s !== slug);
		} else {
			selectedWorkspaces = [...selectedWorkspaces, slug];
		}
	}

	function isExpired(token: PersonalAccessToken): boolean {
		return token.expires_at !== null && new Date(token.expires_at).getTime() <= Date.now();
	}
</script>

<div class="mx-auto max-w-2xl px-4 py-10 sm:px-8">
	<div class="flex items-center justify-between">
		<h1 class="text-2xl font-semibold text-[var(--color-text-primary)]">{m['settings.tokens.title']()}</h1>
		<button
			onclick={openCreate}
			disabled={loading || creating}
			class="flex items-center gap-1 rounded-md bg-[var(--app-accent)] px-3 py-1.5 text-sm text-[var(--app-accent-foreground)] hover:bg-[var(--app-accent-hover)]"
		>
			<Plus size={14} />
			{m['settings.tokens.new']()}
		</button>
	</div>
	<p class="mt-1 text-sm text-[var(--color-text-secondary)]">{m['settings.tokens.desc']()}</p>

	<div class="mt-8">
		{#if loading}
			<div class="flex h-64 items-center justify-center"></div>
		{:else if loadFailed}
			<p role="alert">{m['settings.tokens.failed_load']()}</p>
			<Button onclick={loadTokens}>{m['settings.tokens.retry']()}</Button>
		{:else if tokens.length === 0}
			<EmptyState
				title={m['settings.tokens.no_tokens']()}
				description={m['settings.tokens.no_tokens_desc']()}
				action={{ label: m['settings.tokens.new'](), onclick: openCreate }}
			/>
		{:else}
			<div class="rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
				{#each tokens as token, i (token.id)}
					<div class="flex items-center gap-4 px-5 py-4 {i > 0 ? 'border-t border-[var(--app-border)]' : ''}">
						<div class="min-w-0 flex-1">
							<div class="flex items-center gap-2">
								<KeyRound size={14} class="shrink-0 text-[var(--color-text-tertiary)]" />
								<span class="truncate text-sm font-medium text-[var(--color-text-primary)]">{token.name}</span>
								{#if isExpired(token)}
									<Badge variant="destructive" class="text-[10px]">{m['settings.tokens.expired']()}</Badge>
								{/if}
							</div>
							<div class="mt-1 flex flex-wrap items-center gap-1.5">
								<Badge variant="outline" class="font-mono text-[10px]">{token.token_prefix}</Badge>
								<Badge variant="secondary" class="text-[10px]">
									{m['settings.tokens.n_scopes']({ n: token.scopes.length })}
								</Badge>
								<Badge variant="secondary" class="text-[10px]" title={token.workspace_slugs?.join(', ') ?? ''}>
									{token.workspace_slugs && token.workspace_slugs.length > 0
										? m['settings.tokens.n_workspaces']({ n: token.workspace_slugs.length })
										: m['settings.tokens.all_workspaces']()}
								</Badge>
							</div>
							<p class="mt-1 text-xs text-[var(--color-text-tertiary)]">
								{m['settings.tokens.created']()}
								{formatRelativeTime(token.created_at, getLocale())}
								· {token.expires_at
									? m['settings.tokens.expires']({ time: formatDate(token.expires_at, getLocale()) })
									: m['settings.tokens.never_expires']()}
								· {token.last_used_at
									? m['settings.tokens.last_used']({
											time: formatRelativeTime(token.last_used_at, getLocale())
										})
									: m['settings.tokens.never_used']()}
							</p>
						</div>
						<Button
							variant="ghost"
							size="icon-sm"
							onclick={() => (tokenToRevoke = token)}
							class="text-[var(--color-text-tertiary)] hover:text-[var(--color-error)]"
							aria-label={m['settings.tokens.revoke_button']()}
						>
							<Trash2 size={14} />
						</Button>
					</div>
				{/each}
			</div>
		{/if}
	</div>
</div>

<Dialog.Root
	open={showCreate}
	onOpenChange={(open) => {
		if (!creating) showCreate = open;
	}}
>
	<Dialog.Content
		class="overflow-hidden rounded-xl border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-0 sm:max-w-[560px]"
		showCloseButton={!creating}
		interactOutsideBehavior={creating ? 'ignore' : 'close'}
		escapeKeydownBehavior={creating ? 'ignore' : 'close'}
	>
		<form onsubmit={handleCreate}>
			<div class="max-h-[80vh] space-y-4 overflow-y-auto px-5 pt-5 pb-4">
				<div>
					<Dialog.Title class="text-base font-semibold text-[var(--color-text-primary)]">
						{m['settings.tokens.create_title']()}
					</Dialog.Title>
					<Dialog.Description class="mt-0.5 text-xs text-[var(--color-text-tertiary)]"
						>{m['settings.tokens.create_desc']()}</Dialog.Description
					>
				</div>

				<div class="space-y-1.5">
					<Label for="token-name" class="text-xs text-[var(--color-text-secondary)]"
						>{m['settings.tokens.name']()}</Label
					>
					<Input
						id="token-name"
						bind:value={name}
						placeholder={m['settings.tokens.name_placeholder']()}
						required
						maxlength={100}
						class="border-[var(--app-border)] bg-[var(--color-bg)] text-[var(--color-text-primary)]"
					/>
				</div>

				<div class="space-y-1.5">
					<Label class="text-xs text-[var(--color-text-secondary)]">{m['settings.tokens.preset']()}</Label>
					<Select.Root
						type="single"
						value={preset}
						onValueChange={(value) => PRESET_KEYS.includes(value as PresetKey) && applyPreset(value as PresetKey)}
					>
						<Select.Trigger
							aria-label={m['settings.tokens.preset']()}
							class="w-full border-[var(--app-border)] bg-[var(--color-bg)]"
						>
							{tokenMessages[`preset_${preset}`]()}
						</Select.Trigger>
						<Select.Content>
							{#each PRESET_KEYS as key}
								<Select.Item value={key} label={tokenMessages[`preset_${key}`]()}>
									<div class="flex flex-col">
										<span>{tokenMessages[`preset_${key}`]()}</span>
										<span class="text-[10px] text-[var(--color-text-tertiary)]"
											>{tokenMessages[`preset_${key}_desc`]()}</span
										>
									</div>
								</Select.Item>
							{/each}
							<Select.Item value="custom" disabled label={m['settings.tokens.preset_custom']()}>
								{m['settings.tokens.preset_custom']()}
							</Select.Item>
						</Select.Content>
					</Select.Root>
					{#if preset === 'full_access'}
						<p class="flex items-start gap-1.5 text-xs text-[var(--color-error)]">
							<TriangleAlert size={13} class="mt-0.5 shrink-0" />
							{m['settings.tokens.preset_full_warning']()}
						</p>
					{/if}
				</div>

				<div class="space-y-1.5">
					<Label class="text-xs text-[var(--color-text-secondary)]">{m['settings.tokens.permissions']()}</Label>
					<div class="rounded-md border border-[var(--app-border)]">
						{#each mainRows as row, i (row.key)}
							{@const level = levelForScopes(row, selectedScopes)}
							<div class="flex items-center gap-3 px-3 py-2 {i > 0 ? 'border-t border-[var(--app-border)]' : ''}">
								<div class="min-w-0 flex-1">
									<span class="text-xs text-[var(--color-text-primary)]">{tokenMessages[`res_${row.key}`]()}</span>
									{#if row.note}
										<p class="text-[10px] text-[var(--color-text-tertiary)]">{tokenMessages[`${row.note}`]()}</p>
									{/if}
								</div>
								<Select.Root
									type="single"
									value={level}
									onValueChange={(value) => value && setRowLevel(row, value as AccessLevel)}
								>
									<Select.Trigger
										aria-label={tokenMessages[`res_${row.key}`]()}
										class="w-32 shrink-0 border-[var(--app-border)] bg-[var(--color-bg)]"
									>
										{tokenMessages[`access_${level}`]()}
									</Select.Trigger>
									<Select.Content>
										<Select.Item value="none" label={m['settings.tokens.access_none']()}>
											{m['settings.tokens.access_none']()}
										</Select.Item>
										<Select.Item value="read" label={m['settings.tokens.access_read']()}>
											{m['settings.tokens.access_read']()}
										</Select.Item>
										{#if row.writeScopes.length > 0}
											<Select.Item value="write" label={m['settings.tokens.access_write']()}>
												{m['settings.tokens.access_write']()}
											</Select.Item>
										{/if}
									</Select.Content>
								</Select.Root>
							</div>
						{/each}
						<div class="border-t border-[var(--app-border)]">
							<button
								type="button"
								class="flex w-full cursor-pointer items-center gap-2 px-3 py-2 text-left hover:bg-[var(--color-bg-hover)]"
								onclick={() => (showAdvanced = !showAdvanced)}
							>
								<ChevronDown
									size={12}
									class="shrink-0 text-[var(--color-text-tertiary)] transition-transform {showAdvanced
										? ''
										: '-rotate-90'}"
								/>
								<span class="text-xs text-[var(--color-text-secondary)]">{m['settings.tokens.advanced']()}</span>
							</button>
							{#if showAdvanced}
								{#each advancedRows as row (row.key)}
									{@const level = levelForScopes(row, selectedScopes)}
									<div class="flex items-center gap-3 border-t border-[var(--app-border)] px-3 py-2">
										<div class="min-w-0 flex-1">
											<span class="text-xs text-[var(--color-text-primary)]">{tokenMessages[`res_${row.key}`]()}</span>
										</div>
										<Select.Root
											type="single"
											value={level}
											onValueChange={(value) => value && setRowLevel(row, value as AccessLevel)}
										>
											<Select.Trigger
												aria-label={tokenMessages[`res_${row.key}`]()}
												class="w-32 shrink-0 border-[var(--app-border)] bg-[var(--color-bg)]"
											>
												{tokenMessages[`access_${level}`]()}
											</Select.Trigger>
											<Select.Content>
												<Select.Item value="none" label={m['settings.tokens.access_none']()}>
													{m['settings.tokens.access_none']()}
												</Select.Item>
												<Select.Item value="read" label={m['settings.tokens.access_read']()}>
													{m['settings.tokens.access_read']()}
												</Select.Item>
												<Select.Item value="write" label={m['settings.tokens.access_write']()}>
													{m['settings.tokens.access_write']()}
												</Select.Item>
											</Select.Content>
										</Select.Root>
									</div>
								{/each}
							{/if}
						</div>
					</div>
					{#if selectedScopes.size === 0}
						<p class="text-xs text-[var(--color-error)]">{m['settings.tokens.scopes_required']()}</p>
					{/if}
				</div>

				<div class="space-y-1.5">
					<Label class="text-xs text-[var(--color-text-secondary)]">{m['settings.tokens.workspaces']()}</Label>
					<p class="text-[10px] text-[var(--color-text-tertiary)]">{m['settings.tokens.workspaces_desc']()}</p>
					{#if workspacesLoading}
						<p role="status">{m['common.loading']()}</p>
					{:else if workspacesFailed}
						<p role="alert">{m['settings.tokens.failed_load_workspaces']()}</p>
						<Button type="button" onclick={openCreate}>{m['settings.tokens.retry']()}</Button>
					{:else if workspaces.length > 0}
						<div class="grid grid-cols-2 gap-2">
							{#each workspaces as workspace (workspace.id)}
								<div
									class="flex items-center gap-2 rounded-md border border-[var(--app-border)] px-3 py-2 hover:bg-[var(--color-bg-hover)]"
								>
									<Checkbox
										checked={selectedWorkspaces.includes(workspace.slug)}
										onCheckedChange={() => toggleWorkspace(workspace.slug)}
										aria-label={workspace.name}
									/>
									<span class="truncate text-xs text-[var(--color-text-primary)]">{workspace.name}</span>
								</div>
							{/each}
						</div>
					{/if}
				</div>

				<div class="space-y-1.5">
					<Label class="text-xs text-[var(--color-text-secondary)]">{m['settings.tokens.expiration']()}</Label>
					<Select.Root
						type="single"
						value={expiryChoice}
						onValueChange={(value) => value && (expiryChoice = value as ExpiryChoice)}
					>
						<Select.Trigger
							aria-label={m['settings.tokens.expiration']()}
							class="w-full border-[var(--app-border)] bg-[var(--color-bg)]"
						>
							{tokenMessages[`expiry_${expiryChoice}`]()}
						</Select.Trigger>
						<Select.Content>
							{#each EXPIRY_CHOICES as choice}
								<Select.Item value={choice} label={tokenMessages[`expiry_${choice}`]()}>
									{tokenMessages[`expiry_${choice}`]()}
								</Select.Item>
							{/each}
						</Select.Content>
					</Select.Root>
					{#if expiryChoice === 'custom'}
						<Input
							aria-label={m['settings.tokens.expiry_custom']()}
							type="date"
							bind:value={customDate}
							min={todayStr}
							required
							class="border-[var(--app-border)] bg-[var(--color-bg)] text-[var(--color-text-primary)]"
						/>
						{#if customDate !== '' && !customDateValid}
							<p class="text-xs text-[var(--color-error)]">{m['settings.tokens.expiry_invalid']()}</p>
						{/if}
					{:else if expiryChoice === 'never'}
						<p class="flex items-start gap-1.5 text-xs text-[var(--color-warning,var(--color-error))]">
							<TriangleAlert size={13} class="mt-0.5 shrink-0" />
							{m['settings.tokens.expiry_never_warning']()}
						</p>
					{/if}
				</div>
			</div>

			<div class="flex justify-end gap-2 border-t border-[var(--app-border)] px-5 py-3">
				<Button variant="outline" size="sm" type="button" onclick={() => (showCreate = false)} disabled={creating}>
					{m['settings.cancel']()}
				</Button>
				<Button size="sm" type="submit" disabled={!canSubmit}>
					{creating ? m['settings.creating']() : m['settings.tokens.create_button']()}
				</Button>
			</div>
		</form>
	</Dialog.Content>
</Dialog.Root>

<Dialog.Root open={createdToken !== null}>
	<Dialog.Content
		class="overflow-hidden rounded-xl border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-0 sm:max-w-[480px]"
		interactOutsideBehavior="ignore"
		escapeKeydownBehavior="ignore"
		showCloseButton={false}
	>
		{#if showCloseConfirm}
			<div class="space-y-4 px-5 pt-5 pb-4">
				<Dialog.Title class="text-base font-semibold text-[var(--color-text-primary)]">
					{m['settings.tokens.close_confirm_title']()}
				</Dialog.Title>
				<Dialog.Description class="text-xs text-[var(--color-text-secondary)]"
					>{m['settings.tokens.close_confirm_desc']()}</Dialog.Description
				>
			</div>
			<div class="flex justify-end gap-2 border-t border-[var(--app-border)] px-5 py-3">
				<Button variant="outline" size="sm" onclick={() => (showCloseConfirm = false)}>
					{m['settings.cancel']()}
				</Button>
				<Button size="sm" onclick={confirmTokenSaved}>{m['settings.tokens.close_confirm_button']()}</Button>
			</div>
		{:else}
			<div class="space-y-4 px-5 pt-5 pb-4">
				<div>
					<Dialog.Title class="text-base font-semibold text-[var(--color-text-primary)]">
						{m['settings.tokens.created_title']()}
					</Dialog.Title>
				</div>
				<Dialog.Description
					class="flex items-start gap-1.5 rounded-md border border-[var(--color-error)]/40 bg-[var(--color-error)]/10 px-3 py-2 text-xs text-[var(--color-error)]"
				>
					<TriangleAlert size={13} class="mt-0.5 shrink-0" />
					{m['settings.tokens.created_warning']()}
				</Dialog.Description>
				<div class="flex items-center gap-2">
					<code
						class="min-w-0 flex-1 select-all break-all rounded-md border border-[var(--app-border)] bg-[var(--color-bg)] px-3 py-2 font-mono text-xs text-[var(--color-text-primary)]"
					>
						{createdToken?.token ?? ''}
					</code>
					<Button variant="outline" size="sm" onclick={copyCreatedToken}>
						<Copy size={13} />
						{m['settings.tokens.copy']()}
					</Button>
				</div>
			</div>
			<div class="flex justify-end gap-2 border-t border-[var(--app-border)] px-5 py-3">
				<Button size="sm" onclick={() => (showCloseConfirm = true)}>{m['settings.tokens.done']()}</Button>
			</div>
		{/if}
	</Dialog.Content>
</Dialog.Root>

<AlertDialog.Root
	open={tokenToRevoke !== null}
	onOpenChange={(open) => {
		if (!open && !revoking) tokenToRevoke = null;
	}}
>
	<AlertDialog.Content>
		<AlertDialog.Header>
			<AlertDialog.Title>{m['settings.tokens.revoke_title']()}</AlertDialog.Title>
			<AlertDialog.Description>
				{m['settings.tokens.revoke_desc']({ name: tokenToRevoke?.name ?? '' })}
			</AlertDialog.Description>
		</AlertDialog.Header>
		<AlertDialog.Footer>
			<AlertDialog.Cancel disabled={revoking} onclick={() => (tokenToRevoke = null)}
				>{m['settings.cancel']()}</AlertDialog.Cancel
			>
			<Button variant="destructive" onclick={handleRevoke} disabled={revoking}>
				{revoking ? m['settings.deleting']() : m['settings.tokens.revoke_button']()}
			</Button>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>
