<script lang="ts">
	import { Input } from '$lib/components/ui/input/index.js';
	import { onMount } from 'svelte';
	import { appToast } from '$lib/features/toast/toast';
	import { Button } from '$lib/components/ui/button';
	import { getMe, updateProfile } from '$lib/api/auth';
	import { authState } from '$lib/features/auth/auth.state.svelte';
	import type { User } from '$lib/types/auth';
	import { Copy } from '@lucide/svelte';
	import Avatar from '$lib/components/shared/Avatar.svelte';
	import AvatarDialog from '$lib/features/profile/AvatarDialog.svelte';
	import { m } from '$lib/paraglide/messages.js';
	import { getLocale, setLocale } from '$lib/paraglide/runtime.js';

	let user = $state<User | null>(null);
	let name = $state('');
	let displayName = $state('');
	let avatarOpen = $state(false);
	let saving = $state(false);

	onMount(async () => {
		user = authState.user ?? await getMe();
		name = user.name;
		displayName = user.display_name;
	});

	async function saveProfile() {
		if (!user || !name.trim()) {
			appToast.error(m['settings.profile.name_required']());
			return;
		}
		saving = true;
		try {
			const updated = await updateProfile({
				name: name.trim(),
				display_name: displayName.trim()
			});
			user = updated;
			authState.setUser(updated);
			name = updated.name;
			displayName = updated.display_name;
			appToast.success(m['settings.profile.updated']());
		} catch (err: any) {
			appToast.apiError(err, m['settings.profile.failed_update']());
		} finally {
			saving = false;
		}
	}

	function avatarSaved(updated: User) {
		user = updated;
		authState.setUser(updated);
	}

	async function removeAvatar() {
		try {
			avatarSaved(await updateProfile({ avatar_url: null }));
			appToast.success(m['avatar.removed']());
		} catch (err: any) {
			appToast.apiError(err, m['avatar.error_upload']());
		}
	}

	async function copyUserId() {
		if (!user) return;
		try {
			await navigator.clipboard.writeText(user.id);
			appToast.success(m['settings.profile.id_copied']());
		} catch {
			appToast.error(m['settings.profile.failed_copy_id']());
		}
	}
</script>

<div class="mx-auto max-w-2xl px-8 py-10">
	<h1 class="text-2xl font-semibold text-[var(--color-text-primary)]">{m['settings.profile.title']()}</h1>
	<p class="mt-1 text-sm text-[var(--color-text-tertiary)]">{m['settings.profile.desc']()}</p>

	{#if user}
		<div class="mt-8 rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
			<div class="flex items-center justify-between gap-4 px-5 py-4">
				<div>
					<p class="text-sm font-medium text-[var(--color-text-primary)]">{m['settings.profile.email']()}</p>
					<p class="text-xs text-[var(--color-text-tertiary)]">{m['settings.profile.email_desc']()}</p>
				</div>
				<span class="truncate text-sm text-[var(--color-text-secondary)]">{user.email}</span>
			</div>

			<div class="border-t border-[var(--app-border)]"></div>

			<div class="flex items-center justify-between gap-4 px-5 py-4">
				<div>
					<p class="text-sm font-medium text-[var(--color-text-primary)]">{m['settings.profile.user_id']()}</p>
					<p class="text-xs text-[var(--color-text-tertiary)]">{m['settings.profile.user_id_desc']()}</p>
				</div>
				<div class="flex min-w-0 items-center gap-2">
					<span class="truncate font-mono text-xs text-[var(--color-text-secondary)]">{user.id}</span>
					<Button variant="outline" size="sm" onclick={copyUserId}>
						<Copy size={13} />
						{m['settings.profile.copy']()}
					</Button>
				</div>
			</div>

			<div class="border-t border-[var(--app-border)]"></div>

			<div class="flex items-center justify-between gap-4 px-5 py-4">
				<div>
					<p class="text-sm font-medium text-[var(--color-text-primary)]">{m['settings.profile.name']()}</p>
					<p class="text-xs text-[var(--color-text-tertiary)]">{m['settings.profile.name_desc']()}</p>
				</div>
				<Input variant="plain"
					type="text"
					bind:value={name}
					class="w-[240px] rounded-md border border-[var(--app-border)] bg-[var(--color-bg)] px-3 py-1.5 text-sm text-[var(--color-text-primary)] outline-none focus:border-[var(--app-accent)]"
				/>
			</div>

			<div class="border-t border-[var(--app-border)]"></div>

			<div class="flex items-center justify-between gap-4 px-5 py-4">
				<div>
					<p class="text-sm font-medium text-[var(--color-text-primary)]">{m['settings.profile.display_name']()}</p>
					<p class="text-xs text-[var(--color-text-tertiary)]">{m['settings.profile.display_name_desc']()}</p>
				</div>
				<Input variant="plain"
					type="text"
					bind:value={displayName}
					class="w-[240px] rounded-md border border-[var(--app-border)] bg-[var(--color-bg)] px-3 py-1.5 text-sm text-[var(--color-text-primary)] outline-none focus:border-[var(--app-accent)]"
				/>
			</div>

			<div class="border-t border-[var(--app-border)]"></div>

			<div class="flex items-center justify-between gap-4 px-5 py-4">
				<div>
					<p class="text-sm font-medium text-[var(--color-text-primary)]">{m['settings.profile.avatar_url']()}</p>
					<p class="text-xs text-[var(--color-text-tertiary)]">{m['settings.profile.avatar_url_desc']()}</p>
				</div>
				<div class="flex items-center gap-3">
					<Avatar name={user.display_name || user.name} avatarUrl={user.avatar_url} size="lg" />
					<Button variant="outline" size="sm" onclick={() => (avatarOpen = true)}>{m['avatar.change']()}</Button>
					{#if user.avatar_url}
						<Button variant="ghost" size="sm" onclick={removeAvatar}>{m['avatar.remove']()}</Button>
					{/if}
				</div>
			</div>
		</div>

		<div class="mt-4 flex justify-end">
			<Button onclick={saveProfile} disabled={saving || !name.trim()}>{saving ? m['settings.profile.saving']() : m['settings.profile.save_profile']()}</Button>
		</div>
		<AvatarDialog bind:open={avatarOpen} name={user.display_name || user.name} onSaved={avatarSaved} />
	{:else}
		<div class="mt-8 flex justify-center py-8">
			<div class="h-5 w-5 animate-spin rounded-full border-2 border-[var(--color-text-tertiary)] border-t-transparent"></div>
		</div>
	{/if}
</div>
