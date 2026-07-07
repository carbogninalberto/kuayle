<script lang="ts">
	import { onMount } from 'svelte';
	import { appToast } from '$lib/features/toast/toast';
	import { Button } from '$lib/components/ui/button';
	import { getMe, updateProfile } from '$lib/api/auth';
	import { authState } from '$lib/features/auth/auth.state.svelte';
	import type { User } from '$lib/types/auth';

	let user = $state<User | null>(null);
	let name = $state('');
	let displayName = $state('');
	let avatarUrl = $state('');
	let saving = $state(false);

	onMount(async () => {
		user = authState.user ?? await getMe();
		name = user.name;
		displayName = user.display_name;
		avatarUrl = user.avatar_url ?? '';
	});

	async function saveProfile() {
		if (!user || !name.trim()) {
			appToast.error('Name is required');
			return;
		}
		saving = true;
		try {
			const updated = await updateProfile({
				name: name.trim(),
				display_name: displayName.trim(),
				avatar_url: avatarUrl.trim() === '' ? null : avatarUrl.trim()
			});
			user = updated;
			authState.setUser(updated);
			name = updated.name;
			displayName = updated.display_name;
			avatarUrl = updated.avatar_url ?? '';
			appToast.success('Profile updated');
		} catch (err: any) {
			appToast.apiError(err, 'Failed to update profile');
		} finally {
			saving = false;
		}
	}
</script>

<div class="mx-auto max-w-2xl px-8 py-10">
	<h1 class="text-2xl font-semibold text-[var(--color-text-primary)]">Profile</h1>
	<p class="mt-1 text-sm text-[var(--color-text-tertiary)]">Manage the personal information shown across Kuayle.</p>

	{#if user}
		<div class="mt-8 rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
			<div class="flex items-center justify-between gap-4 px-5 py-4">
				<div>
					<p class="text-sm font-medium text-[var(--color-text-primary)]">Email</p>
					<p class="text-xs text-[var(--color-text-tertiary)]">Used for sign in. Email changes are not supported yet.</p>
				</div>
				<span class="truncate text-sm text-[var(--color-text-secondary)]">{user.email}</span>
			</div>

			<div class="border-t border-[var(--app-border)]"></div>

			<div class="flex items-center justify-between gap-4 px-5 py-4">
				<div>
					<p class="text-sm font-medium text-[var(--color-text-primary)]">Name</p>
					<p class="text-xs text-[var(--color-text-tertiary)]">Your full name.</p>
				</div>
				<input
					type="text"
					bind:value={name}
					class="w-[240px] rounded-md border border-[var(--app-border)] bg-[var(--color-bg)] px-3 py-1.5 text-sm text-[var(--color-text-primary)] outline-none focus:border-[var(--app-accent)]"
				/>
			</div>

			<div class="border-t border-[var(--app-border)]"></div>

			<div class="flex items-center justify-between gap-4 px-5 py-4">
				<div>
					<p class="text-sm font-medium text-[var(--color-text-primary)]">Display name</p>
					<p class="text-xs text-[var(--color-text-tertiary)]">The shorter name shown in comments and mentions.</p>
				</div>
				<input
					type="text"
					bind:value={displayName}
					class="w-[240px] rounded-md border border-[var(--app-border)] bg-[var(--color-bg)] px-3 py-1.5 text-sm text-[var(--color-text-primary)] outline-none focus:border-[var(--app-accent)]"
				/>
			</div>

			<div class="border-t border-[var(--app-border)]"></div>

			<div class="flex items-center justify-between gap-4 px-5 py-4">
				<div>
					<p class="text-sm font-medium text-[var(--color-text-primary)]">Avatar URL</p>
					<p class="text-xs text-[var(--color-text-tertiary)]">Public image URL for your avatar.</p>
				</div>
				<input
					type="url"
					bind:value={avatarUrl}
					placeholder="https://"
					class="w-[240px] rounded-md border border-[var(--app-border)] bg-[var(--color-bg)] px-3 py-1.5 text-sm text-[var(--color-text-primary)] outline-none focus:border-[var(--app-accent)]"
				/>
			</div>
		</div>

		<div class="mt-4 flex justify-end">
			<Button onclick={saveProfile} disabled={saving || !name.trim()}>{saving ? 'Saving...' : 'Save profile'}</Button>
		</div>
	{:else}
		<div class="mt-8 flex justify-center py-8">
			<div class="h-5 w-5 animate-spin rounded-full border-2 border-[var(--color-text-tertiary)] border-t-transparent"></div>
		</div>
	{/if}
</div>
