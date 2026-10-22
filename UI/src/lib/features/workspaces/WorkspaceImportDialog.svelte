<script lang="ts">
	import { Input } from '$lib/components/ui/input/index.js';
	import { goto } from '$app/navigation';
	import { Upload, Loader2, AlertTriangle } from '@lucide/svelte';
	import { Button } from '$lib/components/ui/button';
	import * as Dialog from '$lib/components/ui/dialog';
	import { appToast } from '$lib/features/toast/toast';
	import {
		importWorkspace,
		previewWorkspaceImport,
		type WorkspaceImportPreview
	} from '$lib/api/workspaces';
	import { m } from '$lib/paraglide/messages.js';

	let { compact = false }: { compact?: boolean } = $props();
	let input = $state<HTMLInputElement | null>(null);
	let file = $state<File | null>(null);
	let preview = $state<WorkspaceImportPreview | null>(null);
	let name = $state('');
	let slug = $state('');
	let confirmation = $state('');
	let open = $state(false);
	let loadingPreview = $state(false);
	let importing = $state(false);

	const importantCounts = $derived(
		preview
			? ['teams', 'projects', 'issues', 'comments', 'assets'].map((key) => ({
					key,
					value: preview?.manifest.counts[key] ?? 0
				}))
			: []
	);
	const canImport = $derived(
		!!file &&
		!!preview &&
		preview.missing_users.length === 0 &&
		name.trim().length > 0 &&
		slug.trim().length > 0 &&
		confirmation === slug.trim() &&
		!importing
	);

	function chooseFile() {
		input?.click();
	}

	async function selected(event: Event) {
		const selectedFile = (event.currentTarget as HTMLInputElement).files?.[0] ?? null;
		(event.currentTarget as HTMLInputElement).value = '';
		if (!selectedFile) return;
		file = selectedFile;
		preview = null;
		loadingPreview = true;
		try {
			preview = await previewWorkspaceImport(selectedFile);
			name = preview.name;
			slug = preview.slug;
			confirmation = '';
			open = true;
		} catch (error: any) {
			file = null;
			appToast.apiError(error, m['workspace_transfer.invalid_archive']());
		} finally {
			loadingPreview = false;
		}
	}

	async function runImport() {
		if (!file || !canImport) return;
		importing = true;
		try {
			const result = await importWorkspace(file, name.trim(), slug.trim());
			appToast.success(m['workspace_transfer.imported']({ name: result.name }));
			open = false;
			await goto(`/${result.slug}/inbox`);
		} catch (error: any) {
			if (error?.error?.code === 'WORKSPACE_IMPORT_MISSING_USERS') {
				preview = preview
					? {
							...preview,
							missing_users: (error.error.details ?? []).map((item: { message: string }) => item.message)
						}
					: preview;
			}
			appToast.apiError(error, m['workspace_transfer.import_failed']());
		} finally {
			importing = false;
		}
	}
</script>

<Input bind:ref={input} type="file" accept=".zip,.kuayle.zip,application/zip" class="hidden" onchange={selected} />
<Button variant={compact ? 'outline' : 'default'} onclick={chooseFile} disabled={loadingPreview}>
	{#if loadingPreview}<Loader2 size={14} class="animate-spin" />{:else}<Upload size={14} />{/if}
	{m['workspace_transfer.import_button']()}
</Button>

<Dialog.Root bind:open>
	<Dialog.Content class="sm:max-w-lg border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
		<Dialog.Header>
			<Dialog.Title>{m['workspace_transfer.import_title']()}</Dialog.Title>
			<Dialog.Description>{m['workspace_transfer.import_description']()}</Dialog.Description>
		</Dialog.Header>
		{#if preview}
			<div class="space-y-4 py-2">
				<div class="grid grid-cols-5 gap-2">
					{#each importantCounts as count}
						<div class="rounded-md border border-[var(--app-border)] bg-[var(--color-bg)] p-2 text-center">
							<p class="text-base font-semibold text-[var(--color-text-primary)]">{count.value}</p>
							<p class="truncate text-[10px] capitalize text-[var(--color-text-tertiary)]">{count.key}</p>
						</div>
					{/each}
				</div>

				{#if preview.missing_users.length > 0}
					<div class="rounded-md border border-red-500/30 bg-red-500/5 p-3">
						<div class="flex gap-2 text-sm text-[var(--color-error)]">
							<AlertTriangle size={16} class="mt-0.5 shrink-0" />
							<div>
								<p class="font-medium">{m['workspace_transfer.missing_users']()}</p>
								<p class="mt-1 text-xs">{preview.missing_users.join(', ')}</p>
							</div>
						</div>
					</div>
				{/if}

				{#if preview.manifest.warnings.length > 0}
					<div class="rounded-md border border-amber-500/30 bg-amber-500/5 p-3 text-xs text-[var(--color-text-secondary)]">
						<p class="font-medium text-[var(--color-text-primary)]">{m['workspace_transfer.reconfigure']()}</p>
						<ul class="mt-1 list-disc space-y-1 pl-4">
							{#each preview.manifest.warnings as warning}<li>{warning}</li>{/each}
						</ul>
					</div>
				{/if}

				<div class="grid grid-cols-2 gap-3">
					<label class="text-sm text-[var(--color-text-secondary)]">
						{m['workspace_transfer.workspace_name']()}
						<Input bind:value={name} maxlength={100} class="mt-1 w-full" />
					</label>
					<label class="text-sm text-[var(--color-text-secondary)]">
						{m['workspace_transfer.workspace_slug']()}
						<Input bind:value={slug} maxlength={50} class="mt-1 w-full font-mono" />
					</label>
				</div>
				<label class="block text-sm text-[var(--color-text-secondary)]">
					{m['workspace_transfer.confirm_slug']({ slug })}
					<Input bind:value={confirmation} class="mt-1 w-full font-mono" />
				</label>
			</div>
		{/if}
		<Dialog.Footer>
			<Button variant="outline" onclick={() => (open = false)}>{m['workspace_transfer.cancel']()}</Button>
			<Button onclick={runImport} disabled={!canImport}>
				{#if importing}<Loader2 size={14} class="animate-spin" />{/if}
				{importing ? m['workspace_transfer.importing']() : m['workspace_transfer.import_confirm']()}
			</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
