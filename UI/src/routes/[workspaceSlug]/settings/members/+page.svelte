<script lang="ts">
	import { SettingsPageHeader } from '$lib/components/settings';
	import PageTitle from '$lib/components/shared/PageTitle.svelte';
	import Avatar from '$lib/components/shared/Avatar.svelte';
	import SelectField from '$lib/components/shared/SelectField.svelte';
	import { Input } from '$lib/components/ui/input/index.js';
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
	import { getCurrentWorkspace } from '$lib/features/workspaces/current-workspace.svelte';
	import { listRoles, type WorkspaceRole } from '$lib/api/workspace-roles';
	import { isBuiltinRole, roleName } from '$lib/features/workspaces/role-name';
	import RoleEditor from '$lib/features/workspaces/RoleEditor.svelte';
	import MemberTeamRoles from '$lib/features/workspaces/MemberTeamRoles.svelte';
	import { appToast } from '$lib/features/toast/toast';
	import { m, getLocale } from '$lib/features/i18n/messages';
	import { formatDate } from '$lib/utils/format';
	import { UserPlus, Trash2, Link2, Copy, Ban } from '@lucide/svelte';

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

	const current = getCurrentWorkspace();
	const owner = $derived(current.membership?.workspace.slug === slug && current.membership.workspace.current_user_role === 'owner');
	const canManageMembers = $derived(current.membership?.workspace.slug === slug && (current.membership.workspace.can_manage_members ?? ['owner', 'admin'].includes(current.membership.workspace.current_user_role)));
	let roles = $state<WorkspaceRole[]>([]);
	let rolesPending = $state(true);
	let rolesFailed = $state(false);
	let roleRequest = 0;
	let editor = $state<WorkspaceRole | 'create'>();
	let teamMember = $state<WorkspaceMember>();
	let changingMember = $state('');
	let trigger: HTMLElement | null = null;
	let createButton = $state<HTMLButtonElement | null>(null);
	const roleOptions = $derived(owner && roles.length
		? roles.filter(r => r.key !== 'owner').map(r => ({ value: r.key, label: roleName(r.key, r.name) }))
		: ['admin', 'member', 'guest'].map(key => ({ value: key, label: roleName(key) })));

	$effect(() => {
		const workspace = slug;
		roles = [];
		editor = undefined;
		teamMember = undefined;
		if (owner) void reloadRoles(workspace);
	});

	async function reloadRoles(workspace: string) {
		const request = ++roleRequest;
		rolesPending = true;
		rolesFailed = false;
		try {
			const result = await listRoles(workspace);
			if (request === roleRequest && slug === workspace && owner) roles = result;
		} catch {
			if (request === roleRequest) rolesFailed = true;
		} finally {
			if (request === roleRequest) rolesPending = false;
		}
	}

	function refreshRoles() {
		if (owner) void reloadRoles(slug);
	}

	function closeDialog() {
		editor = undefined;
		teamMember = undefined;
		requestAnimationFrame(() => (trigger?.isConnected ? trigger : createButton)?.focus());
	}

	onMount(async () => {
		try {
			members = await listMembers(slug);
		} catch (err: any) {
			appToast.apiError(err, m['settings.members.failed_invite']());
		} finally {
			loading = false;
		}
	});

	$effect(() => { if (canManageMembers) void loadInviteLinks(); });

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
		if (changingMember) return;
		changingMember = userId;
		try {
			await updateMemberRole(slug, userId, role);
			members = members.map((m) => (m.user_id === userId ? { ...m, role, role_name: roleOptions.find(o => o.value === role)?.label } : m));
			refreshRoles();
			appToast.success(m['settings.members.role_updated']());
		} catch (err: any) {
			appToast.apiError(err, m['settings.members.failed_update_role']());
		} finally { changingMember = ''; }
	}

	async function handleRemove(userId: string) {
		try {
			await removeMember(slug, userId);
			members = members.filter((m) => m.user_id !== userId);
			refreshRoles();
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
			refreshRoles();
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

<div class="settings-page mx-auto max-w-3xl px-4 py-6 sm:px-8 sm:py-10">
	<SettingsPageHeader title={m['settings.members.title']()}>
		{#snippet action()}
			{#if canManageMembers}
			<Button variant="plain" onclick={() => openInvite('email')}
				class="flex items-center gap-1.5 rounded-md bg-[var(--app-accent)] px-3 py-1.5 text-sm text-[var(--app-accent-foreground)] hover:bg-[var(--app-accent-hover)]"
			>
				<UserPlus size={14} />
				{m['settings.members.invite']()}
			</Button>
			{/if}
		{/snippet}
	</SettingsPageHeader>

	<div class="mt-8">
		{#if loading}
			<p role="status" class="text-sm text-muted-foreground">{m['common.loading']()}</p>
		{:else if members.length === 0}
			<p class="text-sm text-[var(--color-text-secondary)]">{m['settings.members.no_members']()}</p>
		{:else}
			<div class="overflow-hidden rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
				{#each members as member, i}
					<div
						class="flex flex-wrap items-center justify-between gap-3 px-4 py-3.5 {i > 0 ? 'border-t border-[var(--app-border)]' : ''}"
					>
						<div class="flex min-w-0 items-center gap-3">
							<Avatar name={member.name || member.email} avatarUrl={member.avatar_url} size="md" />
							<div class="min-w-0 break-words">
								<p class="text-sm font-medium text-[var(--color-text-primary)]">
									{member.name || m['settings.members.unnamed']()}
								</p>
								<p class="text-xs text-[var(--color-text-tertiary)]">{member.email}</p>
							</div>
						</div>
						<div class="flex max-w-full flex-wrap items-center gap-2 [&_[data-slot=select-trigger]]:max-w-full [&_[data-slot=select-value]]:block [&_[data-slot=select-value]]:truncate">
							{#if member.role === 'owner' || !canManageMembers}
								<span class="text-sm text-muted-foreground">{roleName(member.role, member.role_name)}</span>
							{:else}
								{@const selected = roleOptions.find(o => o.value === member.role)}
								<SelectField aria-label={m['settings.members.role']() + ' ' + (member.name || member.email)}
									bind:value={() => member.role, (role) => { void handleRoleChange(member.user_id, role); }}
									disabled={!!changingMember || (owner && !roles.length)}
									options={selected ? roleOptions : [...roleOptions, { value: member.role, label: roleName(member.role, member.role_name), disabled: true }]} />
								{#if owner}
									<Button variant="outline" size="sm" aria-label={m['settings.roles.team']() + ' ' + (member.name || member.email)}
										disabled={!roles.length} onclick={(e) => { trigger = e.currentTarget; teamMember = member; }}>{m['settings.roles.team']()}</Button>
								{/if}
							<Button variant="plain" onclick={() => handleRemove(member.user_id)}
								class="rounded p-1 text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-red-500"
								title={m['settings.members.remove']()}
							>
								<Trash2 size={14} />
							</Button>
							{/if}
						</div>
					</div>
				{/each}
			</div>
		{/if}
	</div>

	{#if owner}
		<section class="mt-10" aria-labelledby="roles-heading">
			<div class="flex items-center justify-between gap-2">
				<h2 id="roles-heading" class="text-lg font-semibold">{m['settings.roles.title']()}</h2>
				<Button bind:ref={createButton} size="sm" disabled={!roles.length} onclick={(e) => { trigger = e.currentTarget; editor = 'create'; }}>{m['settings.roles.create']()}</Button>
			</div>
			{#if rolesFailed}
				<p role="alert" class="my-3 text-sm">{m['settings.roles.load_failed']()}</p>
				<Button variant="outline" onclick={refreshRoles}>{m['settings.tokens.retry']()}</Button>
			{:else if rolesPending && !roles.length}
				<p role="status" class="mt-3 text-sm text-muted-foreground">{m['common.loading']()}</p>
			{:else}
				<table aria-labelledby="roles-heading" class="mt-3 w-full table-fixed text-left text-sm">
					<thead class="border-b text-xs text-muted-foreground"><tr>
						<th class="w-[46%] py-2 font-medium">{m['settings.members.role']()}</th>
						<th class="w-[18%] px-1 text-right font-medium">{m['common.members']()}</th>
						<th class="w-[18%] px-1 text-right font-medium">{m['settings.roles.team']()}</th>
						<th><span class="sr-only">{m['common.edit']()}</span></th>
					</tr></thead>
					<tbody>
						{#each roles as role (role.key)}
							<tr class="border-b">
								<td class="break-words py-3 pr-1"><span class="font-medium">{roleName(role.key, role.name)}</span>
									{#if role.description}<p class="mt-0.5 text-xs text-muted-foreground">{role.description}</p>{/if}</td>
								<td class="px-1 text-right tabular-nums">{role.member_count}</td>
								<td class="px-1 text-right tabular-nums">{role.team_assignments}</td>
								<td class="text-right">{#if !isBuiltinRole(role.key)}<Button variant="ghost" size="sm" onclick={(e) => { trigger = e.currentTarget; editor = role; }}>{m['common.edit']()}</Button>{/if}</td>
							</tr>
						{/each}
					</tbody>
				</table>
			{/if}
		</section>
		{#if editor}<RoleEditor {slug} {roles} role={editor === 'create' ? undefined : editor} onchanged={refreshRoles} onclose={closeDialog} />{/if}
		{#if teamMember}<MemberTeamRoles {slug} member={teamMember} options={roleOptions} onchanged={refreshRoles} onclose={closeDialog} />{/if}
	{/if}

	<!-- Invite links section -->
	{#if canManageMembers}
	<div class="mt-10">
		<div class="flex items-center justify-between">
			<div>
				<h2 class="text-lg font-semibold text-[var(--color-text-primary)]">{m['invite.links.title']()}</h2>
				<p class="mt-0.5 text-xs text-[var(--color-text-tertiary)]">{m['invite.links.desc']()}</p>
			</div>
			<Button variant="plain" onclick={() => openInvite('link')}
				class="flex items-center gap-1.5 rounded-md border border-[var(--app-border)] px-3 py-1.5 text-sm text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]"
			>
				<Link2 size={14} />
				{m['invite.links.create']()}
			</Button>
		</div>

		<div class="mt-4">
			{#if linksLoading}
				<p class="text-sm text-[var(--color-text-tertiary)]"></p>
			{:else if linksFailed}
				<p role="alert" class="text-sm text-[var(--color-text-secondary)]">{m['invite.links.failed_load']()}</p>
				<Button variant="plain" onclick={loadInviteLinks} class="text-sm text-[var(--app-accent)] hover:underline"
					>{m['invite.retry']()}</Button
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
								<Button variant="plain" onclick={() => handleRevokeLink(link.id)}
									disabled={revokingLinkId !== null}
									class="rounded p-1 text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-red-500"
									title={m['invite.links.revoke']()}
								>
									<Ban size={14} />
								</Button>
							{/if}
						</div>
					{/each}
				</div>
			{/if}
		</div>
	</div>
	{/if}
</div>

<!-- Invite Dialog (email / invite link tabs) -->
<Dialog.Root bind:open={showInvite}>
	<Dialog.Content
		class="settings-dialog sm:max-w-md"
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
					<Input variant="plain"
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
				<Button variant="plain" onclick={() => (inviteTab = 'email')}
					aria-pressed={inviteTab === 'email'}
					disabled={linkSubmitting}
					class="flex-1 rounded px-3 py-1.5 text-sm {inviteTab === 'email'
						? 'bg-[var(--color-bg-secondary)] font-medium text-[var(--color-text-primary)]'
						: 'text-[var(--color-text-tertiary)] hover:text-[var(--color-text-secondary)]'}"
				>
					{m['settings.members.email']()}
				</Button>
				<Button variant="plain" onclick={() => (inviteTab = 'link')}
					aria-pressed={inviteTab === 'link'}
					disabled={linkSubmitting}
					class="flex-1 rounded px-3 py-1.5 text-sm {inviteTab === 'link'
						? 'bg-[var(--color-bg-secondary)] font-medium text-[var(--color-text-primary)]'
						: 'text-[var(--color-text-tertiary)] hover:text-[var(--color-text-secondary)]'}"
				>
					{m['invite.links.tab']()}
				</Button>
			</div>

			<div class="space-y-4 py-4">
				{#if inviteTab === 'email'}
					<div>
						<label for="invite-email" class="mb-1 block text-sm text-[var(--color-text-secondary)]"
							>{m['settings.members.email']()}</label
						>
						<Input variant="plain"
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
						<SelectField
							id="invite-role"
							bind:value={inviteRole}
							class="w-full rounded-md border border-[var(--app-border)] bg-[var(--color-bg)] px-3 py-2 text-sm text-[var(--color-text-primary)] outline-none"
						 options={roleOptions} />
					</div>
				{:else}
					<div>
						<label for="link-role" class="mb-1 block text-sm text-[var(--color-text-secondary)]"
							>{m['invite.links.role']()}</label
						>
						<SelectField
							id="link-role"
							bind:value={linkRole}
							class="w-full rounded-md border border-[var(--app-border)] bg-[var(--color-bg)] px-3 py-2 text-sm text-[var(--color-text-primary)] outline-none"
						 options={[{ value: 'member' as const, label: m['common.role.member']() }, { value: 'guest' as const, label: m['common.role.guest']() }]} />
					</div>
					<div>
						<label for="link-expiry" class="mb-1 block text-sm text-[var(--color-text-secondary)]"
							>{m['invite.links.expires_in_days']()}</label
						>
						<Input variant="plain"
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
						<Input variant="plain"
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

<PageTitle parts={[m['settings.members.title'](), m['settings.title']()]} />
