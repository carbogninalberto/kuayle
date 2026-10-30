<script lang="ts">
	import * as Dialog from '$lib/components/ui/dialog';
	import * as Tabs from '$lib/components/ui/tabs';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Progress } from '$lib/components/ui/progress';
	import { Upload } from '@lucide/svelte';
	import { updateProfile, uploadAvatar } from '$lib/api/auth';
	import type { User } from '$lib/types/auth';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/paraglide/messages.js';
	import { AVATAR_TYPES, validateAvatarFile, validateAvatarUrl, type AvatarIssue } from './avatar-rules';

	let {
		open = $bindable(false),
		name,
		onSaved
	}: { open: boolean; name: string; onSaved: (user: User) => void } = $props();

	let tab = $state<'upload' | 'url'>('upload');
	let file = $state<File | null>(null);
	let fileUrl = $state('');
	let url = $state('');
	let error = $state('');
	let previewBroken = $state(false);
	let progress = $state(0);
	let saving = $state(false);
	let fileInput = $state<HTMLInputElement | null>(null);

	const issueText = (issue: AvatarIssue) => m[`avatar.error_${issue}`]();
	const urlIssue = $derived(url.trim() ? validateAvatarUrl(url) : null);
	const previewSrc = $derived(tab === 'upload' ? fileUrl : url.trim() && !urlIssue ? url.trim() : '');
	const canSave = $derived(!saving && (tab === 'upload' ? !!file : !!url.trim() && !urlIssue));

	$effect(() => {
		if (!open) reset();
	});
	$effect(() => {
		url;
		previewBroken = false;
	});

	function reset() {
		if (fileUrl) URL.revokeObjectURL(fileUrl);
		file = null;
		fileUrl = '';
		url = '';
		error = '';
		progress = 0;
		saving = false;
		previewBroken = false;
		tab = 'upload';
	}

	function pick(e: Event) {
		const picked = (e.currentTarget as HTMLInputElement).files?.[0];
		(e.currentTarget as HTMLInputElement).value = '';
		if (!picked) return;
		if (fileUrl) URL.revokeObjectURL(fileUrl);
		const issue = validateAvatarFile(picked);
		if (issue) {
			file = null;
			fileUrl = '';
			error = issueText(issue);
			return;
		}
		error = '';
		file = picked;
		fileUrl = URL.createObjectURL(picked);
	}

	async function save() {
		if (!canSave) return;
		error = '';
		saving = true;
		progress = 0;
		try {
			const user =
				tab === 'upload' && file
					? await uploadAvatar(file, (fraction) => (progress = Math.round(fraction * 100)))
					: await updateProfile({ avatar_url: url.trim() });
			appToast.success(m['avatar.updated']());
			onSaved(user);
			open = false;
		} catch (err: any) {
			error = err?.error?.message && err.status && err.status < 500 ? err.error.message : m['avatar.error_upload']();
			saving = false;
		}
	}
</script>

<Dialog.Root bind:open>
	<Dialog.Content class="sm:max-w-md">
		<Dialog.Header>
			<Dialog.Title>{m['avatar.title']()}</Dialog.Title>
			<Dialog.Description>{m['avatar.description']()}</Dialog.Description>
		</Dialog.Header>

		<Tabs.Root bind:value={tab}>
			<Tabs.List class="w-full">
				<Tabs.Trigger value="upload" disabled={saving}>{m['avatar.tab_upload']()}</Tabs.Trigger>
				<Tabs.Trigger value="url" disabled={saving}>{m['avatar.tab_url']()}</Tabs.Trigger>
			</Tabs.List>

			<div class="mt-4 flex items-start gap-4">
				<div
					class="flex h-20 w-20 shrink-0 items-center justify-center overflow-hidden rounded-full bg-[var(--app-accent)] text-2xl font-medium text-[var(--app-accent-foreground)]"
				>
					{#if previewSrc && !previewBroken}
						<img
							src={previewSrc}
							alt={m['avatar.preview_alt']()}
							class="h-full w-full object-cover"
							onerror={() => (previewBroken = true)}
						/>
					{:else}
						{name.charAt(0).toUpperCase()}
					{/if}
				</div>

				<div class="min-w-0 flex-1 space-y-2">
					<Tabs.Content value="upload" class="space-y-2">
						<input
							bind:this={fileInput}
							type="file"
							accept={AVATAR_TYPES.join(',')}
							class="hidden"
							onchange={pick}
						/>
						<Button variant="outline" size="sm" disabled={saving} onclick={() => fileInput?.click()}>
							<Upload size={13} />
							{file ? file.name : m['avatar.choose_file']()}
						</Button>
						<p class="text-xs text-[var(--color-text-tertiary)]">{m['avatar.file_hint']()}</p>
					</Tabs.Content>
					<Tabs.Content value="url" class="space-y-2">
						<label for="avatar-url" class="block text-xs text-[var(--color-text-secondary)]">{m['avatar.url_label']()}</label>
						<Input id="avatar-url" type="url" bind:value={url} placeholder="https://" disabled={saving} />
						{#if urlIssue}
							<p class="text-xs text-red-500">{issueText(urlIssue)}</p>
						{:else if previewBroken && url.trim()}
							<p class="text-xs text-red-500">{m['avatar.preview_failed']()}</p>
						{/if}
					</Tabs.Content>
				</div>
			</div>
		</Tabs.Root>

		{#if saving && tab === 'upload'}
			<Progress value={progress} aria-label={m['avatar.uploading']()} />
		{/if}
		{#if error}
			<p role="alert" class="text-sm text-red-500">{error}</p>
		{/if}

		<Dialog.Footer>
			<Button variant="outline" disabled={saving} onclick={() => (open = false)}>{m['avatar.cancel']()}</Button>
			<Button disabled={!canSave} onclick={save}>{saving ? m['avatar.uploading']() : m['avatar.save']()}</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
