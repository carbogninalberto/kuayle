<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import type { WorkspaceMember } from '$lib/types/workspace';
	import { listMembers, updateMemberRole, removeMember, inviteMember } from '$lib/api/members';
	import * as Dialog from '$lib/components/ui/dialog';
	import { Button } from '$lib/components/ui/button';
	import * as Popover from '$lib/components/ui/popover';
	import { toast } from 'svelte-sonner';
	import { UserPlus, Trash2 } from 'lucide-svelte';

	const slug = $derived(page.params.workspaceSlug ?? '');

	let members = $state<WorkspaceMember[]>([]);
	let loading = $state(true);
	let showInvite = $state(false);
	let inviteEmail = $state('');
	let inviteRole = $state('member');

	const roles = ['owner', 'admin', 'member', 'guest'];

	onMount(async () => {
		try {
			members = await listMembers(slug);
		} finally {
			loading = false;
		}
	});

	async function handleRoleChange(userId: string, role: string) {
		try {
			await updateMemberRole(slug, userId, role);
			members = members.map((m) => (m.user_id === userId ? { ...m, role } : m));
			toast.success('Role updated');
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to update role');
		}
	}

	async function handleRemove(userId: string) {
		try {
			await removeMember(slug, userId);
			members = members.filter((m) => m.user_id !== userId);
			toast.success('Member removed');
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to remove member');
		}
	}

	async function handleInvite() {
		if (!inviteEmail.trim()) return;
		try {
			await inviteMember(slug, inviteEmail.trim(), inviteRole);
			toast.success('Member invited');
			showInvite = false;
			inviteEmail = '';
			inviteRole = 'member';
			members = await listMembers(slug);
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to invite member');
		}
	}
</script>

<div class="h-full">
	<div class="flex h-[49px] items-center justify-between border-b border-[var(--app-border)] px-6">
		<h1 class="text-sm font-medium text-[var(--color-text-primary)]">Members</h1>
		<button
			onclick={() => (showInvite = true)}
			class="flex items-center gap-1.5 rounded-md bg-[var(--app-accent)] px-3 py-1.5 text-sm text-white hover:bg-[var(--app-accent-hover)]"
		>
			<UserPlus size={14} />
			Invite member
		</button>
	</div>

	<div class="p-6">
		{#if loading}
			<p class="text-sm text-[var(--color-text-tertiary)]">Loading...</p>
		{:else if members.length === 0}
			<p class="text-sm text-[var(--color-text-secondary)]">No members found.</p>
		{:else}
			<div class="overflow-hidden rounded-lg border border-[var(--app-border)]">
				<table class="w-full text-sm">
					<thead>
						<tr class="border-b border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
							<th class="px-4 py-2.5 text-left text-xs font-medium text-[var(--color-text-tertiary)]">Member</th>
							<th class="px-4 py-2.5 text-left text-xs font-medium text-[var(--color-text-tertiary)]">Email</th>
							<th class="px-4 py-2.5 text-left text-xs font-medium text-[var(--color-text-tertiary)]">Role</th>
							<th class="px-4 py-2.5 text-right text-xs font-medium text-[var(--color-text-tertiary)]"></th>
						</tr>
					</thead>
					<tbody>
						{#each members as member}
							<tr class="border-b border-[var(--app-border)] last:border-b-0">
								<td class="px-4 py-3">
									<div class="flex items-center gap-2">
										<div class="flex h-7 w-7 items-center justify-center rounded-full bg-[var(--app-accent)] text-xs font-medium text-white">
											{(member.name || member.email).charAt(0).toUpperCase()}
										</div>
										<span class="font-medium text-[var(--color-text-primary)]">{member.name || 'Unnamed'}</span>
									</div>
								</td>
								<td class="px-4 py-3 text-[var(--color-text-secondary)]">{member.email}</td>
								<td class="px-4 py-3">
									<Popover.Root>
										<Popover.Trigger>
											<button class="rounded-md border border-[var(--app-border)] px-2.5 py-1 text-xs capitalize text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]">
												{member.role}
											</button>
										</Popover.Trigger>
										<Popover.Content class="w-36 p-1" align="start">
											{#each roles as role}
												<button
													onclick={() => handleRoleChange(member.user_id, role)}
													class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm capitalize text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] {member.role === role ? 'bg-[var(--color-bg-hover)]' : ''}"
												>
													{role}
												</button>
											{/each}
										</Popover.Content>
									</Popover.Root>
								</td>
								<td class="px-4 py-3 text-right">
									<button
										onclick={() => handleRemove(member.user_id)}
										class="rounded p-1 text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-red-500"
										title="Remove member"
									>
										<Trash2 size={14} />
									</button>
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}
	</div>
</div>

<!-- Invite Dialog -->
<Dialog.Root bind:open={showInvite}>
	<Dialog.Content class="sm:max-w-md">
		<Dialog.Header>
			<Dialog.Title>Invite member</Dialog.Title>
			<Dialog.Description>Invite a new member to this workspace by email.</Dialog.Description>
		</Dialog.Header>
		<div class="space-y-4 py-4">
			<div>
				<label for="invite-email" class="mb-1 block text-sm text-[var(--color-text-secondary)]">Email</label>
				<input
					id="invite-email"
					type="email"
					bind:value={inviteEmail}
					placeholder="user@example.com"
					class="w-full rounded-md border border-[var(--app-border)] bg-[var(--color-bg)] px-3 py-2 text-sm text-[var(--color-text-primary)] outline-none focus:border-[var(--app-accent)]"
				/>
			</div>
			<div>
				<label for="invite-role" class="mb-1 block text-sm text-[var(--color-text-secondary)]">Role</label>
				<select
					id="invite-role"
					bind:value={inviteRole}
					class="w-full rounded-md border border-[var(--app-border)] bg-[var(--color-bg)] px-3 py-2 text-sm text-[var(--color-text-primary)] outline-none"
				>
					<option value="admin">Admin</option>
					<option value="member">Member</option>
					<option value="guest">Guest</option>
				</select>
			</div>
		</div>
		<Dialog.Footer>
			<Button variant="outline" onclick={() => (showInvite = false)}>Cancel</Button>
			<Button onclick={handleInvite} disabled={!inviteEmail.trim()}>Send invite</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
