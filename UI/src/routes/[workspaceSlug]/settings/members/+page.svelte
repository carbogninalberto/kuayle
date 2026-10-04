<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import type { WorkspaceMember } from '$lib/types/workspace';
	import { listMembers, updateMemberRole, removeMember, inviteMember } from '$lib/api/members';
	import {
		listInviteLinks,
		createInviteLink,
		revokeInviteLink,
		type InviteLink,
		type InviteLinkRole,
		type CreateInviteLinkRequest
	} from '$lib/api/invite';
	import * as Dialog from '$lib/components/ui/dialog';
	import { Button } from '$lib/components/ui/button';
	import * as Popover from '$lib/components/ui/popover';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/paraglide/messages.js';
	import { getLocale } from '$lib/paraglide/runtime.js';
	import { formatDate } from '$lib/utils/format';
	import { UserPlus, Trash2, Link2, Copy, Ban } from 'lucide-svelte';

	const slug = $derived(page.params.workspaceSlug ?? '');

	let members = $state<WorkspaceMember[]>([]);
	let loading = $state(true);
	let showInvite = $state(false);
	let inviteEmail = $state('');
	let inviteRole = $state('member');

	let inviteLinks = $state<InviteLink[]>([]);
	let linksLoading = $state(true);
	let inviteTab = $state<'email' | 'link'>('email');
	let linkRole = $state<InviteLinkRole>('member');
	let linkExpiryDays = $state<number | null | undefined>(7);
	let linkMaxUses = $state<number | null | undefined>();
	let linksFailed = $state(false);
	let revokingLinkId = $state<string | null>(null);
	const validLinkSettings = $derived(
		Number.isInteger(linkExpiryDays) &&
			linkExpiryDays! >= 1 &&
			linkExpiryDays! <= 365 &&
			(linkMaxUses == null || (Number.isSafeInteger(linkMaxUses) && linkMaxUses > 0))
	);
	let linkSubmitting = $state(false);
	let createdLinkUrl = $state<string | null>(null);

	const roles = ['owner', 'admin', 'member', 'guest'];

	onMount(async () => {
		try {
			members = await listMembers(slug);
		} catch (err: any) {
			appToast.apiError(err, m['settings.members.failed_invite']());
		} finally {
			loading = false;
		}
		await loadInviteLinks();
	});

	async function loadInviteLinks() {
		linksLoading = true;
		linksFailed = false;
		try {
			inviteLinks = await listInviteLinks(slug);
		} catch {
			linksFailed = true;
		} finally {
			linksLoading = false;
		}
	}

	async function handleRoleChange(userId: string, role: string) {
		try {
			await updateMemberRole(slug, userId, role);
			members = members.map((m) => (m.user_id === userId ? { ...m, role } : m));
			appToast.success(m['settings.members.role_updated']());
		} catch (err: any) {
			appToast.apiError(err, m['settings.members.failed_update_role']());
		}
	}

	async function handleRemove(userId: string) {
		try {
			await removeMember(slug, userId);
			members = members.filter((m) => m.user_id !== userId);
			appToast.success(m['settings.members.removed']());
		} catch (err: any) {
			appToast.apiError(err, m['settings.members.failed_remove']());
		}
	}

	async function handleInvite() {
		if (!inviteEmail.trim()) return;
		try {
			await inviteMember(slug, inviteEmail.trim(), inviteRole);
			appToast.success(m['settings.members.invited']());
			showInvite = false;
			inviteEmail = '';
			inviteRole = 'member';
			members = await listMembers(slug);
		} catch (err: any) {
			appToast.apiError(err, m['settings.members.failed_invite']());
		}
	}

	type LinkStatus = 'active' | 'expired' | 'revoked' | 'exhausted';

	function linkStatus(link: InviteLink): LinkStatus {
		if (link.revoked_at) return 'revoked';
		if (new Date(link.expires_at).getTime() < Date.now()) return 'expired';
		if (link.max_uses != null && link.use_count >= link.max_uses) return 'exhausted';
		return 'active';
	}

	function linkUsage(link: InviteLink): string {
		return link.max_uses != null
			? m['invite.links.usage']({ used: link.use_count, max: link.max_uses })
			: m['invite.links.usage_unlimited']({ used: link.use_count });
	}

	function openInvite(tab: 'email' | 'link') {
		inviteTab = tab;
		createdLinkUrl = null;
		if (tab === 'link') {
			linkRole = 'member';
			linkExpiryDays = 7;
			linkMaxUses = undefined;
		}
		showInvite = true;
	}

	async function handleCreateLink() {
		if (linkSubmitting || !validLinkSettings) return;
		linkSubmitting = true;
		try {
			const req: CreateInviteLinkRequest = {
				role: linkRole,
				expires_in_days: linkExpiryDays ?? undefined
			};
			if (linkMaxUses != null) req.max_uses = linkMaxUses;
			const link = await createInviteLink(slug, req);
			inviteLinks = [link, ...inviteLinks];
			createdLinkUrl = link.invite_url ?? null;
			appToast.success(m['invite.links.created_toast']());
		} catch (err: any) {
			appToast.apiError(err, m['invite.links.failed_create']());
		} finally {
			linkSubmitting = false;
		}
	}

	async function handleRevokeLink(id: string) {
		if (revokingLinkId) return;
		revokingLinkId = id;
		try {
			await revokeInviteLink(slug, id);
			inviteLinks = inviteLinks.map((l) => (l.id === id ? { ...l, revoked_at: new Date().toISOString() } : l));
			appToast.success(m['invite.links.revoked_toast']());
		} catch (err: any) {
			appToast.apiError(err, m['invite.links.failed_revoke']());
		} finally {
			revokingLinkId = null;
		}
	}

	async function copyCreatedLink() {
		if (!createdLinkUrl) return;
		try {
			await navigator.clipboard.writeText(createdLinkUrl);
			appToast.success(m['invite.links.copied']());
		} catch {
			appToast.error(m['invite.links.failed_copy']());
		}
	}
</script>

<div class="mx-auto max-w-2xl px-8 py-10">
	<div class="flex items-center justify-between">
		<h1 class="text-2xl font-semibold text-[var(--color-text-primary)]">{m['settings.members.title']()}</h1>
		<button
			onclick={() => openInvite('email')}
			class="flex items-center gap-1.5 rounded-md bg-[var(--app-accent)] px-3 py-1.5 text-sm text-[var(--app-accent-foreground)] hover:bg-[var(--app-accent-hover)]"
		>
			<UserPlus size={14} />
			{m['settings.members.invite']()}
		</button>
	</div>

	<div class="mt-8">
		{#if loading}
			<p class="text-sm text-[var(--color-text-tertiary)]"></p>
		{:else if members.length === 0}
			<p class="text-sm text-[var(--color-text-secondary)]">{m['settings.members.no_members']()}</p>
		{:else}
			<div class="overflow-hidden rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
				{#each members as member, i}
					<div
						class="flex items-center justify-between px-5 py-3.5 {i > 0 ? 'border-t border-[var(--app-border)]' : ''}"
					>
						<div class="flex items-center gap-3">
							<div
								class="flex h-8 w-8 items-center justify-center rounded-full bg-[var(--app-accent)] text-xs font-medium text-[var(--app-accent-foreground)]"
							>
								{(member.name || member.email).charAt(0).toUpperCase()}
							</div>
							<div>
								<p class="text-sm font-medium text-[var(--color-text-primary)]">
									{member.name || m['settings.members.unnamed']()}
								</p>
								<p class="text-xs text-[var(--color-text-tertiary)]">{member.email}</p>
							</div>
						</div>
						<div class="flex items-center gap-2">
							<Popover.Root>
								<Popover.Trigger>
									<button
										class="rounded-md border border-[var(--app-border)] px-2.5 py-1 text-xs capitalize text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]"
									>
										{(m as unknown as Record<string, () => string>)['common.role.' + member.role]()}
									</button>
								</Popover.Trigger>
								<Popover.Content class="w-36 p-1" align="end">
									{#each roles as role}
										<button
											onclick={() => handleRoleChange(member.user_id, role)}
											class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm capitalize text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] {member.role ===
											role
												? 'bg-[var(--color-bg-hover)]'
												: ''}"
										>
											{(m as unknown as Record<string, () => string>)['common.role.' + role]()}
										</button>
									{/each}
								</Popover.Content>
							</Popover.Root>
							<button
								onclick={() => handleRemove(member.user_id)}
								class="rounded p-1 text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-red-500"
								title={m['settings.members.remove']()}
							>
								<Trash2 size={14} />
							</button>
						</div>
					</div>
				{/each}
			</div>
		{/if}
	</div>

	<!-- Invite links section -->
	<div class="mt-10">
		<div class="flex items-center justify-between">
			<div>
				<h2 class="text-lg font-semibold text-[var(--color-text-primary)]">{m['invite.links.title']()}</h2>
				<p class="mt-0.5 text-xs text-[var(--color-text-tertiary)]">{m['invite.links.desc']()}</p>
			</div>
			<button
				onclick={() => openInvite('link')}
				class="flex items-center gap-1.5 rounded-md border border-[var(--app-border)] px-3 py-1.5 text-sm text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]"
			>
				<Link2 size={14} />
				{m['invite.links.create']()}
			</button>
		</div>

		<div class="mt-4">
			{#if linksLoading}
				<p class="text-sm text-[var(--color-text-tertiary)]"></p>
			{:else if linksFailed}
				<p role="alert" class="text-sm text-[var(--color-text-secondary)]">{m['invite.links.failed_load']()}</p>
				<button onclick={loadInviteLinks} class="text-sm text-[var(--app-accent)] hover:underline"
					>{m['invite.retry']()}</button
				>
			{:else if inviteLinks.length === 0}
				<p class="text-sm text-[var(--color-text-secondary)]">{m['invite.links.empty']()}</p>
			{:else}
				<div class="overflow-hidden rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
					{#each inviteLinks as link, i}
						{@const status = linkStatus(link)}
						<div
							class="flex items-center justify-between px-5 py-3.5 {i > 0 ? 'border-t border-[var(--app-border)]' : ''}"
						>
							<div class="flex min-w-0 items-center gap-3">
								<Link2 size={14} class="shrink-0 text-[var(--color-text-tertiary)]" />
								<div class="min-w-0">
									<div class="flex items-center gap-2">
										<span class="text-sm font-medium text-[var(--color-text-primary)]"
											>{(m as unknown as Record<string, () => string>)['common.role.' + link.role]()}</span
										>
										<span
											class="rounded-full px-2 py-0.5 text-[10px] font-medium {status === 'active'
												? 'bg-[var(--app-accent)]/10 text-[var(--app-accent)]'
												: 'bg-[var(--color-bg-tertiary)] text-[var(--color-text-tertiary)]'}"
										>
											{(m as unknown as Record<string, () => string>)['invite.links.status.' + status]()}
										</span>
									</div>
									<p class="mt-0.5 text-xs text-[var(--color-text-tertiary)]">
										{linkUsage(link)} · {m['invite.links.expires']({ date: formatDate(link.expires_at, getLocale()) })}
									</p>
								</div>
							</div>
							{#if status === 'active'}
								<button
									onclick={() => handleRevokeLink(link.id)}
									disabled={revokingLinkId !== null}
									class="rounded p-1 text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-red-500"
									title={m['invite.links.revoke']()}
								>
									<Ban size={14} />
								</button>
							{/if}
						</div>
					{/each}
				</div>
			{/if}
		</div>
	</div>
</div>

<!-- Invite Dialog (email / invite link tabs) -->
<Dialog.Root bind:open={showInvite}>
	<Dialog.Content
		class="sm:max-w-md"
		showCloseButton={!linkSubmitting}
		interactOutsideBehavior={linkSubmitting ? 'ignore' : 'close'}
		escapeKeydownBehavior={linkSubmitting ? 'ignore' : 'close'}
	>
		{#if createdLinkUrl}
			<Dialog.Header>
				<Dialog.Title>{m['invite.links.created_title']()}</Dialog.Title>
				<Dialog.Description>{m['invite.links.created_desc']()}</Dialog.Description>
			</Dialog.Header>
			<div class="space-y-4 py-4">
				<div class="flex items-center gap-2">
					<input
						readonly
						aria-label={m['invite.links.created_title']()}
						value={createdLinkUrl}
						class="w-full rounded-md border border-[var(--app-border)] bg-[var(--color-bg)] px-3 py-2 font-mono text-xs text-[var(--color-text-primary)] outline-none"
						onfocus={(e) => (e.target as HTMLInputElement).select()}
					/>
					<Button variant="outline" onclick={copyCreatedLink} class="shrink-0">
						<Copy size={14} />
						{m['invite.links.copy']()}
					</Button>
				</div>
			</div>
			<Dialog.Footer>
				<Button onclick={() => (showInvite = false)}>{m['invite.links.done']()}</Button>
			</Dialog.Footer>
		{:else}
			<Dialog.Header>
				<Dialog.Title>{m['settings.members.invite_title']()}</Dialog.Title>
				<Dialog.Description>
					{inviteTab === 'email' ? m['settings.members.invite_desc']() : m['invite.links.create_desc']()}
				</Dialog.Description>
			</Dialog.Header>

			<div class="mt-4 flex rounded-md border border-[var(--app-border)] bg-[var(--color-bg)] p-0.5">
				<button
					onclick={() => (inviteTab = 'email')}
					aria-pressed={inviteTab === 'email'}
					disabled={linkSubmitting}
					class="flex-1 rounded px-3 py-1.5 text-sm {inviteTab === 'email'
						? 'bg-[var(--color-bg-secondary)] font-medium text-[var(--color-text-primary)]'
						: 'text-[var(--color-text-tertiary)] hover:text-[var(--color-text-secondary)]'}"
				>
					{m['settings.members.email']()}
				</button>
				<button
					onclick={() => (inviteTab = 'link')}
					aria-pressed={inviteTab === 'link'}
					disabled={linkSubmitting}
					class="flex-1 rounded px-3 py-1.5 text-sm {inviteTab === 'link'
						? 'bg-[var(--color-bg-secondary)] font-medium text-[var(--color-text-primary)]'
						: 'text-[var(--color-text-tertiary)] hover:text-[var(--color-text-secondary)]'}"
				>
					{m['invite.links.tab']()}
				</button>
			</div>

			<div class="space-y-4 py-4">
				{#if inviteTab === 'email'}
					<div>
						<label for="invite-email" class="mb-1 block text-sm text-[var(--color-text-secondary)]"
							>{m['settings.members.email']()}</label
						>
						<input
							id="invite-email"
							type="email"
							bind:value={inviteEmail}
							placeholder={m['settings.members.email_placeholder']()}
							class="w-full rounded-md border border-[var(--app-border)] bg-[var(--color-bg)] px-3 py-2 text-sm text-[var(--color-text-primary)] outline-none focus:border-[var(--app-accent)]"
						/>
					</div>
					<div>
						<label for="invite-role" class="mb-1 block text-sm text-[var(--color-text-secondary)]"
							>{m['settings.members.role']()}</label
						>
						<select
							id="invite-role"
							bind:value={inviteRole}
							class="w-full rounded-md border border-[var(--app-border)] bg-[var(--color-bg)] px-3 py-2 text-sm text-[var(--color-text-primary)] outline-none"
						>
							<option value="admin">{m['settings.role_admin']()}</option>
							<option value="member">{m['settings.role_member']()}</option>
							<option value="guest">{m['common.role.guest']()}</option>
						</select>
					</div>
				{:else}
					<div>
						<label for="link-role" class="mb-1 block text-sm text-[var(--color-text-secondary)]"
							>{m['invite.links.role']()}</label
						>
						<select
							id="link-role"
							bind:value={linkRole}
							class="w-full rounded-md border border-[var(--app-border)] bg-[var(--color-bg)] px-3 py-2 text-sm text-[var(--color-text-primary)] outline-none"
						>
							<option value="member">{m['common.role.member']()}</option>
							<option value="guest">{m['common.role.guest']()}</option>
						</select>
					</div>
					<div>
						<label for="link-expiry" class="mb-1 block text-sm text-[var(--color-text-secondary)]"
							>{m['invite.links.expires_in_days']()}</label
						>
						<input
							id="link-expiry"
							type="number"
							max="365"
							step="1"
							min="1"
							bind:value={linkExpiryDays}
							class="w-full rounded-md border border-[var(--app-border)] bg-[var(--color-bg)] px-3 py-2 text-sm text-[var(--color-text-primary)] outline-none focus:border-[var(--app-accent)]"
						/>
					</div>
					<div>
						<label for="link-max-uses" class="mb-1 block text-sm text-[var(--color-text-secondary)]"
							>{m['invite.links.max_uses']()}</label
						>
						<input
							id="link-max-uses"
							type="number"
							min="1"
							bind:value={linkMaxUses}
							placeholder={m['invite.links.max_uses_placeholder']()}
							class="w-full rounded-md border border-[var(--app-border)] bg-[var(--color-bg)] px-3 py-2 text-sm text-[var(--color-text-primary)] outline-none focus:border-[var(--app-accent)]"
						/>
					</div>
				{/if}
			</div>
			<Dialog.Footer>
				<Button variant="outline" disabled={linkSubmitting} onclick={() => (showInvite = false)}
					>{m['settings.cancel']()}</Button
				>
				{#if inviteTab === 'email'}
					<Button onclick={handleInvite} disabled={!inviteEmail.trim()}>{m['settings.members.send_invite']()}</Button>
				{:else}
					<Button onclick={handleCreateLink} disabled={linkSubmitting || !validLinkSettings}
						>{m['invite.links.create_button']()}</Button
					>
				{/if}
			</Dialog.Footer>
		{/if}
	</Dialog.Content>
</Dialog.Root>
