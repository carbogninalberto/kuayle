<script lang="ts">
	import { onMount } from 'svelte';
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
	import { i18n } from '$lib/i18n/index.svelte';
	import { formatDate, formatRelativeTime } from '$lib/utils/format';
	import { Plus, Trash2, KeyRound, Copy, ChevronDown, TriangleAlert } from 'lucide-svelte';

	type ExpiryChoice = '30d' | '90d' | '1y' | 'custom' | 'never';

	const EXPIRY_CHOICES: ExpiryChoice[] = ['30d', '90d', '1y', 'custom', 'never'];
	const DAY_MS = 24 * 60 * 60 * 1000;

	let tokens = $state<PersonalAccessToken[]>([]);
	let loading = $state(true);
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
	const todayStr = toLocalDateStr(new Date());
	const customDateValid = $derived(customDate !== '' && customDate >= todayStr);
	const canSubmit = $derived(
		name.trim().length > 0 && selectedScopes.size > 0 && (expiryChoice !== 'custom' || customDateValid) && !creating
	);

	function toLocalDateStr(d: Date): string {
		return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
	}

	onMount(async () => {
		try {
			tokens = await listTokens();
		} catch (err: any) {
			appToast.apiError(err, i18n.t('settings.tokens.failed_load'));
		} finally {
			loading = false;
		}
	});

	async function openCreate() {
		resetForm();
		showCreate = true;
		if (workspaces.length === 0) {
			try {
				workspaces = await listWorkspaces();
			} catch (err: any) {
				appToast.apiError(err, i18n.t('settings.tokens.failed_load_workspaces'));
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

	function computeExpiresAt(): string | undefined {
		const now = Date.now();
		switch (expiryChoice) {
			case '30d':
				return new Date(now + 30 * DAY_MS).toISOString();
			case '90d':
				return new Date(now + 90 * DAY_MS).toISOString();
			case '1y':
				return new Date(now + 365 * DAY_MS).toISOString();
			case 'custom':
				return new Date(`${customDate}T23:59:59`).toISOString();
			case 'never':
				return undefined;
		}
	}

	async function handleCreate(e: Event) {
		e.preventDefault();
		if (!canSubmit) return;
		creating = true;
		try {
			const created = await createToken({
				name: name.trim(),
				scopes: [...selectedScopes].sort(),
				...(selectedWorkspaces.length > 0 ? { workspace_slugs: selectedWorkspaces } : {}),
				...(expiryChoice === 'never' ? {} : { expires_at: computeExpiresAt() })
			});
			const { token: _plaintext, ...listed } = created;
			tokens = [listed, ...tokens];
			createdToken = created;
			showCreate = false;
			resetForm();
		} catch (err: any) {
			appToast.apiError(err, i18n.t('settings.tokens.failed_create'));
		} finally {
			creating = false;
		}
	}

	async function handleRevoke() {
		if (!tokenToRevoke) return;
		revoking = true;
		try {
			await revokeToken(tokenToRevoke.id);
			tokens = tokens.filter((t) => t.id !== tokenToRevoke?.id);
			appToast.success(i18n.t('settings.tokens.revoked'));
			tokenToRevoke = null;
		} catch (err: any) {
			appToast.apiError(err, i18n.t('settings.tokens.failed_revoke'));
		} finally {
			revoking = false;
		}
	}

	async function copyCreatedToken() {
		if (!createdToken) return;
		try {
			await navigator.clipboard.writeText(createdToken.token);
			appToast.success(i18n.t('settings.tokens.copied'));
		} catch {
			appToast.error(i18n.t('settings.tokens.failed_copy'));
		}
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

<div class="mx-auto max-w-2xl px-8 py-10">
	<div class="flex items-center justify-between">
		<h1 class="text-2xl font-semibold text-[var(--color-text-primary)]">{i18n.t('settings.tokens.title')}</h1>
		<button
			onclick={openCreate}
			class="flex items-center gap-1 rounded-md bg-[var(--app-accent)] px-3 py-1.5 text-sm text-[var(--app-accent-foreground)] hover:bg-[var(--app-accent-hover)]"
		>
			<Plus size={14} />
			{i18n.t('settings.tokens.new')}
		</button>
	</div>
	<p class="mt-1 text-sm text-[var(--color-text-secondary)]">{i18n.t('settings.tokens.desc')}</p>

	<div class="mt-8">
		{#if loading}
			<div class="flex h-64 items-center justify-center"></div>
		{:else if tokens.length === 0}
			<EmptyState
				title={i18n.t('settings.tokens.no_tokens')}
				description={i18n.t('settings.tokens.no_tokens_desc')}
				action={{ label: i18n.t('settings.tokens.new'), onclick: openCreate }}
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
									<Badge variant="destructive" class="text-[10px]">{i18n.t('settings.tokens.expired')}</Badge>
								{/if}
							</div>
							<div class="mt-1 flex flex-wrap items-center gap-1.5">
								<Badge variant="outline" class="font-mono text-[10px]">{token.token_prefix}</Badge>
								<Badge variant="secondary" class="text-[10px]">
									{i18n.t('settings.tokens.n_scopes', { n: token.scopes.length })}
								</Badge>
								<Badge variant="secondary" class="text-[10px]" title={token.workspace_slugs?.join(', ') ?? ''}>
									{token.workspace_slugs && token.workspace_slugs.length > 0
										? i18n.t('settings.tokens.n_workspaces', { n: token.workspace_slugs.length })
										: i18n.t('settings.tokens.all_workspaces')}
								</Badge>
							</div>
							<p class="mt-1 text-xs text-[var(--color-text-tertiary)]">
								{i18n.t('settings.tokens.created')}
								{formatRelativeTime(token.created_at, i18n.dateLocale)}
								· {token.expires_at
									? i18n.t('settings.tokens.expires', { time: formatDate(token.expires_at, i18n.dateLocale) })
									: i18n.t('settings.tokens.never_expires')}
								· {token.last_used_at
									? i18n.t('settings.tokens.last_used', {
											time: formatRelativeTime(token.last_used_at, i18n.dateLocale)
										})
									: i18n.t('settings.tokens.never_used')}
							</p>
						</div>
						<Button
							variant="ghost"
							size="icon-sm"
							onclick={() => (tokenToRevoke = token)}
							class="text-[var(--color-text-tertiary)] hover:text-[var(--color-error)]"
							aria-label={i18n.t('settings.tokens.revoke_button')}
						>
							<Trash2 size={14} />
						</Button>
					</div>
				{/each}
			</div>
		{/if}
	</div>
</div>

<Dialog.Root bind:open={showCreate}>
	<Dialog.Content
		class="overflow-hidden rounded-xl border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-0 sm:max-w-[560px]"
	>
		<form onsubmit={handleCreate}>
			<div class="max-h-[80vh] space-y-4 overflow-y-auto px-5 pt-5 pb-4">
				<div>
					<h2 class="text-base font-semibold text-[var(--color-text-primary)]">
						{i18n.t('settings.tokens.create_title')}
					</h2>
					<p class="mt-0.5 text-xs text-[var(--color-text-tertiary)]">{i18n.t('settings.tokens.create_desc')}</p>
				</div>

				<div class="space-y-1.5">
					<Label class="text-xs text-[var(--color-text-secondary)]">{i18n.t('settings.tokens.name')}</Label>
					<Input
						bind:value={name}
						placeholder={i18n.t('settings.tokens.name_placeholder')}
						required
						maxlength={100}
						class="border-[var(--app-border)] bg-[var(--color-bg)] text-[var(--color-text-primary)]"
					/>
				</div>

				<div class="space-y-1.5">
					<Label class="text-xs text-[var(--color-text-secondary)]">{i18n.t('settings.tokens.preset')}</Label>
					<Select.Root
						type="single"
						value={preset}
						onValueChange={(value) => value !== 'custom' && applyPreset(value as PresetKey)}
					>
						<Select.Trigger class="w-full border-[var(--app-border)] bg-[var(--color-bg)]">
							{i18n.t(`settings.tokens.preset_${preset}`)}
						</Select.Trigger>
						<Select.Content>
							{#each PRESET_KEYS as key}
								<Select.Item value={key} label={i18n.t(`settings.tokens.preset_${key}`)}>
									<div class="flex flex-col">
										<span>{i18n.t(`settings.tokens.preset_${key}`)}</span>
										<span class="text-[10px] text-[var(--color-text-tertiary)]"
											>{i18n.t(`settings.tokens.preset_${key}_desc`)}</span
										>
									</div>
								</Select.Item>
							{/each}
							<Select.Item value="custom" disabled label={i18n.t('settings.tokens.preset_custom')}>
								{i18n.t('settings.tokens.preset_custom')}
							</Select.Item>
						</Select.Content>
					</Select.Root>
					{#if preset === 'full_access'}
						<p class="flex items-start gap-1.5 text-xs text-[var(--color-error)]">
							<TriangleAlert size={13} class="mt-0.5 shrink-0" />
							{i18n.t('settings.tokens.preset_full_warning')}
						</p>
					{/if}
				</div>

				<div class="space-y-1.5">
					<Label class="text-xs text-[var(--color-text-secondary)]">{i18n.t('settings.tokens.permissions')}</Label>
					<div class="rounded-md border border-[var(--app-border)]">
						{#each mainRows as row, i (row.key)}
							{@const level = levelForScopes(row, selectedScopes)}
							<div class="flex items-center gap-3 px-3 py-2 {i > 0 ? 'border-t border-[var(--app-border)]' : ''}">
								<div class="min-w-0 flex-1">
									<span class="text-xs text-[var(--color-text-primary)]"
										>{i18n.t(`settings.tokens.res_${row.key}`)}</span
									>
									{#if row.note}
										<p class="text-[10px] text-[var(--color-text-tertiary)]">{i18n.t(`settings.tokens.${row.note}`)}</p>
									{/if}
								</div>
								<Select.Root
									type="single"
									value={level}
									onValueChange={(value) => value && setRowLevel(row, value as AccessLevel)}
								>
									<Select.Trigger class="w-32 shrink-0 border-[var(--app-border)] bg-[var(--color-bg)]">
										{i18n.t(`settings.tokens.access_${level}`)}
									</Select.Trigger>
									<Select.Content>
										<Select.Item value="none" label={i18n.t('settings.tokens.access_none')}>
											{i18n.t('settings.tokens.access_none')}
										</Select.Item>
										<Select.Item value="read" label={i18n.t('settings.tokens.access_read')}>
											{i18n.t('settings.tokens.access_read')}
										</Select.Item>
										{#if row.writeScopes.length > 0}
											<Select.Item value="write" label={i18n.t('settings.tokens.access_write')}>
												{i18n.t('settings.tokens.access_write')}
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
								<span class="text-xs text-[var(--color-text-secondary)]">{i18n.t('settings.tokens.advanced')}</span>
							</button>
							{#if showAdvanced}
								{#each advancedRows as row (row.key)}
									{@const level = levelForScopes(row, selectedScopes)}
									<div class="flex items-center gap-3 border-t border-[var(--app-border)] px-3 py-2">
										<div class="min-w-0 flex-1">
											<span class="text-xs text-[var(--color-text-primary)]"
												>{i18n.t(`settings.tokens.res_${row.key}`)}</span
											>
										</div>
										<Select.Root
											type="single"
											value={level}
											onValueChange={(value) => value && setRowLevel(row, value as AccessLevel)}
										>
											<Select.Trigger class="w-32 shrink-0 border-[var(--app-border)] bg-[var(--color-bg)]">
												{i18n.t(`settings.tokens.access_${level}`)}
											</Select.Trigger>
											<Select.Content>
												<Select.Item value="none" label={i18n.t('settings.tokens.access_none')}>
													{i18n.t('settings.tokens.access_none')}
												</Select.Item>
												<Select.Item value="read" label={i18n.t('settings.tokens.access_read')}>
													{i18n.t('settings.tokens.access_read')}
												</Select.Item>
												<Select.Item value="write" label={i18n.t('settings.tokens.access_write')}>
													{i18n.t('settings.tokens.access_write')}
												</Select.Item>
											</Select.Content>
										</Select.Root>
									</div>
								{/each}
							{/if}
						</div>
					</div>
					{#if selectedScopes.size === 0}
						<p class="text-xs text-[var(--color-error)]">{i18n.t('settings.tokens.scopes_required')}</p>
					{/if}
				</div>

				<div class="space-y-1.5">
					<Label class="text-xs text-[var(--color-text-secondary)]">{i18n.t('settings.tokens.workspaces')}</Label>
					<p class="text-[10px] text-[var(--color-text-tertiary)]">{i18n.t('settings.tokens.workspaces_desc')}</p>
					{#if workspaces.length > 0}
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
					<Label class="text-xs text-[var(--color-text-secondary)]">{i18n.t('settings.tokens.expiration')}</Label>
					<Select.Root
						type="single"
						value={expiryChoice}
						onValueChange={(value) => value && (expiryChoice = value as ExpiryChoice)}
					>
						<Select.Trigger class="w-full border-[var(--app-border)] bg-[var(--color-bg)]">
							{i18n.t(`settings.tokens.expiry_${expiryChoice}`)}
						</Select.Trigger>
						<Select.Content>
							{#each EXPIRY_CHOICES as choice}
								<Select.Item value={choice} label={i18n.t(`settings.tokens.expiry_${choice}`)}>
									{i18n.t(`settings.tokens.expiry_${choice}`)}
								</Select.Item>
							{/each}
						</Select.Content>
					</Select.Root>
					{#if expiryChoice === 'custom'}
						<Input
							type="date"
							bind:value={customDate}
							min={todayStr}
							required
							class="border-[var(--app-border)] bg-[var(--color-bg)] text-[var(--color-text-primary)]"
						/>
						{#if customDate !== '' && !customDateValid}
							<p class="text-xs text-[var(--color-error)]">{i18n.t('settings.tokens.expiry_invalid')}</p>
						{/if}
					{:else if expiryChoice === 'never'}
						<p class="flex items-start gap-1.5 text-xs text-[var(--color-warning,var(--color-error))]">
							<TriangleAlert size={13} class="mt-0.5 shrink-0" />
							{i18n.t('settings.tokens.expiry_never_warning')}
						</p>
					{/if}
				</div>
			</div>

			<div class="flex justify-end gap-2 border-t border-[var(--app-border)] px-5 py-3">
				<Button variant="outline" size="sm" type="button" onclick={() => (showCreate = false)}>
					{i18n.t('settings.cancel')}
				</Button>
				<Button size="sm" type="submit" disabled={!canSubmit}>
					{creating ? i18n.t('settings.creating') : i18n.t('settings.tokens.create_button')}
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
		<div class="space-y-4 px-5 pt-5 pb-4">
			<div>
				<h2 class="text-base font-semibold text-[var(--color-text-primary)]">
					{i18n.t('settings.tokens.created_title')}
				</h2>
			</div>
			<p
				class="flex items-start gap-1.5 rounded-md border border-[var(--color-error)]/40 bg-[var(--color-error)]/10 px-3 py-2 text-xs text-[var(--color-error)]"
			>
				<TriangleAlert size={13} class="mt-0.5 shrink-0" />
				{i18n.t('settings.tokens.created_warning')}
			</p>
			<div class="flex items-center gap-2">
				<code
					class="min-w-0 flex-1 truncate rounded-md border border-[var(--app-border)] bg-[var(--color-bg)] px-3 py-2 font-mono text-xs text-[var(--color-text-primary)]"
				>
					{createdToken?.token ?? ''}
				</code>
				<Button variant="outline" size="sm" onclick={copyCreatedToken}>
					<Copy size={13} />
					{i18n.t('settings.tokens.copy')}
				</Button>
			</div>
		</div>
		<div class="flex justify-end gap-2 border-t border-[var(--app-border)] px-5 py-3">
			<Button size="sm" onclick={() => (showCloseConfirm = true)}>{i18n.t('settings.tokens.done')}</Button>
		</div>
	</Dialog.Content>
</Dialog.Root>

<AlertDialog.Root bind:open={showCloseConfirm}>
	<AlertDialog.Content>
		<AlertDialog.Header>
			<AlertDialog.Title>{i18n.t('settings.tokens.close_confirm_title')}</AlertDialog.Title>
			<AlertDialog.Description>{i18n.t('settings.tokens.close_confirm_desc')}</AlertDialog.Description>
		</AlertDialog.Header>
		<AlertDialog.Footer>
			<AlertDialog.Cancel>{i18n.t('settings.cancel')}</AlertDialog.Cancel>
			<AlertDialog.Action onclick={() => (createdToken = null)}>
				{i18n.t('settings.tokens.close_confirm_button')}
			</AlertDialog.Action>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>

<AlertDialog.Root open={tokenToRevoke !== null} onOpenChange={(open) => !open && (tokenToRevoke = null)}>
	<AlertDialog.Content>
		<AlertDialog.Header>
			<AlertDialog.Title>{i18n.t('settings.tokens.revoke_title')}</AlertDialog.Title>
			<AlertDialog.Description>
				{i18n.t('settings.tokens.revoke_desc', { name: tokenToRevoke?.name ?? '' })}
			</AlertDialog.Description>
		</AlertDialog.Header>
		<AlertDialog.Footer>
			<AlertDialog.Cancel onclick={() => (tokenToRevoke = null)}>{i18n.t('settings.cancel')}</AlertDialog.Cancel>
			<AlertDialog.Action variant="destructive" onclick={handleRevoke} disabled={revoking}>
				{revoking ? i18n.t('settings.deleting') : i18n.t('settings.tokens.revoke_button')}
			</AlertDialog.Action>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>
