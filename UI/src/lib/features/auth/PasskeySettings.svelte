<script lang="ts">
	// The account's passkeys: add, rename and remove.
	import { onMount } from 'svelte';
	import * as AlertDialog from '$lib/components/ui/alert-dialog';
	import * as Dialog from '$lib/components/ui/dialog';
	import { Badge } from '$lib/components/ui/badge';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { KeyRound, Pencil, Plus, Trash2 } from '@lucide/svelte';
	import { getConfig } from '$lib/api/invite';
	import { deletePasskey, listPasskeys, renamePasskey, type Passkey } from '$lib/api/passkeys';
	import { appToast } from '$lib/features/toast/toast';
	import { m, getLocale } from '$lib/features/i18n/messages';
	import { formatDate, formatRelativeTime } from '$lib/utils/format';
	import AddPasskeyDialog from './AddPasskeyDialog.svelte';
	import { apiErrorCode, passkeysSupported } from './webauthn';

	let passkeys = $state<Passkey[]>([]);
	let loading = $state(true);
	let enabled = $state(false);
	let supported = $state(false);
	let adding = $state(false);
	let renaming = $state<Passkey | null>(null);
	let newName = $state('');
	let removing = $state<Passkey | null>(null);
	let busy = $state(false);

	onMount(async () => {
		supported = passkeysSupported();
		try {
			const [config, listed] = await Promise.all([getConfig(), listPasskeys()]);
			enabled = config.passkeys_enabled === true;
			passkeys = listed;
		} catch (error) {
			appToast.apiError(error, m['passkeys.failed_load']());
		} finally {
			loading = false;
		}
	});

	function startRename(passkey: Passkey) {
		newName = passkey.name;
		renaming = passkey;
	}

	async function rename(event: Event) {
		event.preventDefault();
		if (!renaming || busy || !newName.trim()) return;
		busy = true;
		try {
			const updated = await renamePasskey(renaming.id, newName.trim());
			passkeys = passkeys.map((p) => (p.id === updated.id ? updated : p));
			renaming = null;
			appToast.success(m['passkeys.renamed']());
		} catch (error) {
			appToast.apiError(error, m['passkeys.failed_rename']());
		} finally {
			busy = false;
		}
	}

	async function remove() {
		if (!removing || busy) return;
		busy = true;
		try {
			await deletePasskey(removing.id);
			passkeys = passkeys.filter((p) => p.id !== removing?.id);
			removing = null;
			appToast.success(m['passkeys.removed']());
		} catch (error) {
			if (apiErrorCode(error) === 'LAST_PASSKEY') appToast.error(m['passkeys.last_passkey']());
			else appToast.apiError(error, m['passkeys.failed_remove']());
		} finally {
			busy = false;
		}
	}
</script>

<section class="mt-10" aria-labelledby="passkeys-heading">
	<div class="flex items-start justify-between gap-4">
		<div>
			<h2 id="passkeys-heading" class="text-lg font-semibold text-[var(--color-text-primary)]">{m['passkeys.title']()}</h2>
			<p class="mt-1 text-sm text-[var(--color-text-tertiary)]">{m['passkeys.desc']()}</p>
		</div>
		{#if enabled && supported}
			<Button size="sm" class="shrink-0" onclick={() => (adding = true)}>
				<Plus size={14} />
				{m['passkeys.add']()}
			</Button>
		{/if}
	</div>

	{#if !loading && !enabled}
		<p class="mt-3 text-sm text-[var(--color-text-secondary)]">{m['passkeys.disabled']()}</p>
	{:else if !loading && !supported}
		<p class="mt-3 text-sm text-[var(--color-text-secondary)]">{m['passkeys.unsupported']()}</p>
	{/if}

	<div class="mt-4 rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
		{#if loading}
			<div class="flex justify-center py-6">
				<div
					class="h-5 w-5 animate-spin rounded-full border-2 border-[var(--color-text-tertiary)] border-t-transparent"
				></div>
			</div>
		{:else if passkeys.length === 0}
			<p class="px-5 py-6 text-center text-sm text-[var(--color-text-tertiary)]">{m['passkeys.empty']()}</p>
		{:else}
			<ul class="divide-y divide-[var(--app-border)]">
				{#each passkeys as passkey (passkey.id)}
					<li class="flex items-center justify-between gap-4 px-5 py-4">
						<div class="flex min-w-0 items-center gap-3">
							<KeyRound size={16} class="shrink-0 text-[var(--color-text-tertiary)]" aria-hidden="true" />
							<div class="min-w-0">
								<p class="flex flex-wrap items-center gap-2 text-sm font-medium text-[var(--color-text-primary)]">
									<span class="truncate">{passkey.name}</span>
									{#if passkey.current}<Badge variant="secondary">{m['passkeys.current']()}</Badge>{/if}
									{#if passkey.backed_up}<Badge variant="outline">{m['passkeys.synced']()}</Badge>{/if}
								</p>
								<p class="text-xs text-[var(--color-text-tertiary)]">
									{m['passkeys.created']({ date: formatDate(passkey.created_at, getLocale()) })}
									·
									{passkey.last_used_at
										? m['passkeys.last_used']({ date: formatRelativeTime(passkey.last_used_at, getLocale()) })
										: m['passkeys.never_used']()}
								</p>
							</div>
						</div>
						<div class="flex shrink-0 items-center gap-1">
							<Button
								variant="ghost"
								size="icon-sm"
								aria-label={m['passkeys.rename_named']({ name: passkey.name })}
								onclick={() => startRename(passkey)}
							>
								<Pencil size={14} />
							</Button>
							<Button
								variant="ghost"
								size="icon-sm"
								aria-label={m['passkeys.remove_named']({ name: passkey.name })}
								onclick={() => (removing = passkey)}
							>
								<Trash2 size={14} />
							</Button>
						</div>
					</li>
				{/each}
			</ul>
		{/if}
	</div>
</section>

<AddPasskeyDialog bind:open={adding} onAdded={(passkey) => (passkeys = [...passkeys, passkey])} />

<Dialog.Root
	open={renaming !== null}
	onOpenChange={(open) => {
		if (!open && !busy) renaming = null;
	}}
>
	<Dialog.Content
		class="overflow-hidden rounded-xl border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-0 sm:max-w-[440px]"
	>
		<form onsubmit={rename}>
			<div class="space-y-4 px-5 pt-5 pb-4">
				<Dialog.Title class="text-base font-semibold text-[var(--color-text-primary)]"
					>{m['passkeys.rename_title']()}</Dialog.Title
				>
				<div class="space-y-1.5">
					<Label for="passkey-rename" class="text-xs text-[var(--color-text-secondary)]">{m['passkeys.name_label']()}</Label>
					<Input id="passkey-rename" bind:value={newName} maxlength={64} required disabled={busy} />
				</div>
			</div>
			<div class="flex justify-end gap-2 border-t border-[var(--app-border)] px-5 py-3">
				<Button variant="outline" size="sm" type="button" disabled={busy} onclick={() => (renaming = null)}
					>{m['common.cancel']()}</Button
				>
				<Button size="sm" type="submit" disabled={busy || !newName.trim()}>{m['common.save']()}</Button>
			</div>
		</form>
	</Dialog.Content>
</Dialog.Root>

<AlertDialog.Root
	open={removing !== null}
	onOpenChange={(open) => {
		if (!open && !busy) removing = null;
	}}
>
	<AlertDialog.Content>
		<AlertDialog.Header>
			<AlertDialog.Title>{m['passkeys.remove_title']()}</AlertDialog.Title>
			<AlertDialog.Description>{m['passkeys.remove_desc']({ name: removing?.name ?? '' })}</AlertDialog.Description>
		</AlertDialog.Header>
		<AlertDialog.Footer>
			<AlertDialog.Cancel disabled={busy} onclick={() => (removing = null)}>{m['common.cancel']()}</AlertDialog.Cancel>
			<Button variant="destructive" onclick={remove} disabled={busy}>{m['common.remove']()}</Button>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>
