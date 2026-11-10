<script lang="ts">
	import { Button } from '$lib/components/ui/button/index.js';
	import { Checkbox } from '$lib/components/ui/checkbox/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import * as Dialog from '$lib/components/ui/dialog';
	import * as RadioGroup from '$lib/components/ui/radio-group/index.js';
	import { getWikiPublication, publishWikiPage } from '$lib/api/wiki';
	import type { WikiPublication, WikiPublishMode, WikiScope } from '$lib/types/wiki';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/features/i18n/messages';
	import { Copy, Globe, RefreshCw } from '@lucide/svelte';

	// Publishing one page read-only: off, internal (members who can see the
	// wiki get a clean reader) or public (anyone with the link). The API
	// enforces wiki:write, the workspace's share role and private teams.
	let {
		slug,
		scope,
		pageId,
		readerHref,
		canWrite,
		triggerClass
	}: {
		slug: string;
		scope: WikiScope;
		pageId: string;
		/** The internal reader's path in this workspace. */
		readerHref: string;
		canWrite: boolean;
		triggerClass: string;
	} = $props();

	let open = $state(false);
	let publication = $state<WikiPublication | null>(null);
	let mode = $state<WikiPublishMode>('off');
	let children = $state(false);
	let saving = $state(false);
	let failed = $state(false);

	async function load() {
		failed = false;
		try {
			publication = await getWikiPublication(slug, scope, pageId);
			mode = publication.mode;
			children = publication.children;
		} catch {
			failed = true;
		}
	}

	async function save(rotate = false) {
		if (saving) return;
		saving = true;
		try {
			publication = await publishWikiPage(slug, scope, pageId, { mode, children, rotate });
			mode = publication.mode;
			children = publication.children;
			appToast.success(rotate ? m['wiki.publish.rotated']() : m['wiki.publish.saved']());
		} catch (error) {
			const code = (error as { error?: { code?: string } }).error?.code;
			if (code === 'WIKI_PUBLISH_PRIVATE') appToast.error(m['wiki.publish.private']());
			else if (code === 'WIKI_PUBLISH_FORBIDDEN') appToast.error(m['wiki.publish.forbidden']());
			else appToast.apiError(error, m['wiki.publish.failed']());
			if (publication) mode = publication.mode;
		} finally {
			saving = false;
		}
	}

	async function copy(text: string) {
		try {
			await navigator.clipboard.writeText(text);
			appToast.success(m['wiki.toast.link_copied']());
		} catch {
			appToast.error(m['wiki.toast.link_copy_failed']());
		}
	}

	const readerUrl = $derived(
		typeof window === 'undefined' ? readerHref : new URL(readerHref, window.location.origin).href
	);
	const changed = $derived(!!publication && (publication.mode !== mode || publication.children !== children));
</script>

<Dialog.Root
	bind:open
	onOpenChange={(value) => {
		if (value) void load();
	}}
>
	<Dialog.Trigger class={triggerClass} aria-label={m['wiki.publish.button']()} title={m['wiki.publish.button']()}>
		<Globe size={14} class={publication && publication.mode !== 'off' ? 'text-[var(--app-accent)]' : ''} />
	</Dialog.Trigger>
	<Dialog.Content class="sm:max-w-md">
		<Dialog.Header>
			<Dialog.Title>{m['wiki.publish.title']()}</Dialog.Title>
			<Dialog.Description>{m['wiki.publish.desc']()}</Dialog.Description>
		</Dialog.Header>
		{#if failed}
			<p role="alert" class="text-sm text-[var(--color-error)]">{m['wiki.publish.load_failed']()}</p>
		{:else if publication}
			<RadioGroup.Root
				bind:value={mode}
				disabled={!canWrite || saving}
				aria-label={m['wiki.publish.button']()}
				class="grid gap-2"
			>
				{#each [{ value: 'off', label: m['wiki.publish.off'](), hint: m['wiki.publish.off_hint']() }, { value: 'internal', label: m['wiki.publish.internal'](), hint: m['wiki.publish.internal_hint']() }, { value: 'public', label: m['wiki.publish.public'](), hint: m['wiki.publish.public_hint']() }] as option (option.value)}
					<Label
						for="publish-{option.value}"
						class="flex cursor-pointer items-start gap-3 rounded-md border border-[var(--app-border)] p-3 font-normal has-[[data-state=checked]]:border-[var(--app-accent)]"
					>
						<RadioGroup.Item value={option.value} id="publish-{option.value}" class="mt-0.5" />
						<span class="grid gap-0.5">
							<span class="text-sm font-medium">{option.label}</span>
							<span class="text-xs text-[var(--color-text-tertiary)]">{option.hint}</span>
						</span>
					</Label>
				{/each}
			</RadioGroup.Root>
			{#if mode !== 'off'}
				<Label class="flex items-center gap-2 font-normal">
					<Checkbox bind:checked={children} disabled={!canWrite || saving} />
					{m['wiki.publish.children']()}
				</Label>
			{/if}
			{#if mode === 'public'}
				<p class="text-xs text-[var(--color-text-tertiary)]">{m['wiki.publish.public_references']()}</p>
			{/if}
			{#if publication.mode === 'public' && publication.url && !changed}
				<div class="grid gap-1.5">
					<Label for="publish-link" class="text-xs">{m['wiki.publish.public_link']()}</Label>
					<div class="flex gap-1.5">
						<Input
							id="publish-link"
							readonly
							value={publication.url}
							class="h-8 text-xs"
							onfocus={(e) => (e.currentTarget as HTMLInputElement).select()}
						/>
						<Button
							variant="outline"
							size="icon-sm"
							aria-label={m['wiki.copy_link']()}
							title={m['wiki.copy_link']()}
							onclick={() => publication?.url && copy(publication.url)}><Copy size={14} /></Button
						>
						{#if canWrite}
							<Button
								variant="outline"
								size="icon-sm"
								aria-label={m['wiki.publish.rotate']()}
								title={m['wiki.publish.rotate']()}
								disabled={saving}
								onclick={() => save(true)}><RefreshCw size={14} /></Button
							>
						{/if}
					</div>
				</div>
			{:else if publication.mode === 'internal' && !changed}
				<div class="flex items-center gap-1.5">
					<a href={readerHref} class="truncate text-xs underline underline-offset-2"
						>{m['wiki.publish.open_reader']()}</a
					>
					<Button
						variant="ghost"
						size="icon-sm"
						aria-label={m['wiki.copy_link']()}
						title={m['wiki.copy_link']()}
						onclick={() => copy(readerUrl)}><Copy size={14} /></Button
					>
				</div>
			{/if}
			<Dialog.Footer>
				<Button variant="outline" onclick={() => (open = false)}>{m['common.cancel']()}</Button>
				{#if canWrite}
					<Button disabled={!changed || saving} onclick={() => save()}>{m['wiki.publish.save']()}</Button>
				{/if}
			</Dialog.Footer>
		{/if}
	</Dialog.Content>
</Dialog.Root>
