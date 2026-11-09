<script lang="ts" module>
	/** One recorded change of a title or a rich-text body, oldest to newest. */
	export interface VersionEntry {
		id: string;
		field: 'title' | 'body';
		old_value: string | null;
		new_value: string | null;
		author: string;
		created_at: string;
	}
</script>

<script lang="ts">
	import { untrack, type Snippet } from 'svelte';
	import SelectField from '$lib/components/shared/SelectField.svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Checkbox } from '$lib/components/ui/checkbox/index.js';
	import * as Dialog from '$lib/components/ui/dialog';
	import { m } from '$lib/paraglide/messages.js';
	import { getLocale } from '$lib/paraglide/runtime.js';
	import { sanitizeHtml } from '$lib/security/sanitize';
	import TextDiff from './TextDiff.svelte';

	// Shared by issue and wiki history: a stepper through recorded changes
	// shown as a git-style diff or as before/after panes, with optional restore.
	let {
		dialogTitle,
		intro,
		empty,
		fieldName,
		load,
		onrestore,
		triggerClass = 'rounded-md border border-[var(--app-border)] px-2.5 py-1.5 text-xs text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]',
		triggerContent,
		triggerLabel,
		open = $bindable(false)
	}: {
		dialogTitle: string;
		intro: string;
		empty: string;
		fieldName: (field: VersionEntry['field']) => string;
		/** Entries oldest first. */
		load: () => Promise<VersionEntry[]>;
		/** Offered on each pane when set; resolves once the value is saved. */
		onrestore?: (entry: VersionEntry, value: string) => Promise<void>;
		triggerClass?: string;
		triggerContent?: Snippet;
		/** Accessible name when the trigger shows only an icon. */
		triggerLabel?: string;
		open?: boolean;
	} = $props();

	let loading = $state(false);
	let error = $state(false);
	let versions = $state<VersionEntry[]>([]);
	let selectedId = $state('');
	let source = $state(false);
	// The git-style diff is the default view; before/after panes are one click away.
	let changes = $state(true);
	let restoring = $state(false);
	let selected = $derived(versions.find((v) => v.id === selectedId));
	let selectedIndex = $derived(versions.findIndex((v) => v.id === selectedId));
	let request = 0;

	async function refresh() {
		const currentRequest = ++request;
		loading = true;
		error = false;
		versions = [];
		selectedId = '';
		try {
			const entries = await load();
			if (currentRequest !== request) return;
			versions = entries;
			selectedId = versions.at(-1)?.id ?? '';
		} catch {
			if (currentRequest === request) error = true;
		} finally {
			if (currentRequest === request) loading = false;
		}
	}

	async function restore(entry: VersionEntry, value: string) {
		if (!onrestore || restoring) return;
		restoring = true;
		try {
			await onrestore(entry, value);
			open = false;
		} finally {
			restoring = false;
		}
	}

	function timestamp(value: string): string {
		return new Date(value).toLocaleString(getLocale());
	}

	$effect(() => {
		if (open)
			untrack(() => {
				source = false;
				changes = true;
				void refresh();
			});
		else request++;
	});
</script>

<Dialog.Root bind:open>
	<Dialog.Trigger class={triggerClass} aria-label={triggerLabel} title={triggerLabel}>
		{#if triggerContent}{@render triggerContent()}{:else}{m['issue.versions.button']()}{/if}
	</Dialog.Trigger>
	<Dialog.Content
		class="top-[5dvh] max-h-[90dvh] overflow-y-auto bg-[var(--color-bg)] text-[var(--color-text-primary)] sm:max-w-4xl"
	>
		<Dialog.Header>
			<Dialog.Title>{dialogTitle}</Dialog.Title>
			<Dialog.Description>{intro}</Dialog.Description>
		</Dialog.Header>
		{#if loading}
			<p role="status">{m['issue.versions.loading']()}</p>
		{:else if error}
			<div role="alert">
				{m['issue.versions.error']()}
				<Button variant="plain" class="underline" onclick={refresh}>{m['issue.versions.retry']()}</Button>
			</div>
		{:else if versions.length === 0}
			<p>{empty}</p>
		{:else}
			<label class="grid gap-2 text-sm">
				{m['issue.versions.change']()}
				<SelectField
					class="w-full min-w-0 rounded-md border border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-2"
					bind:value={selectedId}
					options={versions.map((entry, index) => ({
						value: entry.id,
						label: `${index + 1}. ${fieldName(entry.field)} · ${timestamp(entry.created_at)} · ${entry.author}`
					}))}
				/>
			</label>
			<div class="flex flex-wrap items-center justify-between gap-2">
				<Button
					variant="plain"
					class="rounded border border-[var(--app-border)] px-3 py-1 disabled:opacity-40"
					disabled={selectedIndex <= 0}
					onclick={() => (selectedId = versions[selectedIndex - 1].id)}>{m['issue.versions.previous']()}</Button
				>
				<span>{m['issue.versions.position']({ current: selectedIndex + 1, total: versions.length })}</span>
				<Button
					variant="plain"
					class="rounded border border-[var(--app-border)] px-3 py-1 disabled:opacity-40"
					disabled={selectedIndex >= versions.length - 1}
					onclick={() => (selectedId = versions[selectedIndex + 1].id)}>{m['issue.versions.next']()}</Button
				>
			</div>
			{#if selected}
				<p class="text-sm break-words">
					{m['issue.versions.edited']({ author: selected.author, field: fieldName(selected.field) })} ·
					<time datetime={selected.created_at}>{timestamp(selected.created_at)}</time>
				</p>
				<div class="flex flex-wrap items-center gap-4">
					<label class="flex items-center gap-2"
						><Checkbox bind:checked={changes} /> {m['versions.show_changes']()}</label
					>
					{#if selected.field === 'body' && !changes}
						<label class="flex items-center gap-2"
							><Checkbox bind:checked={source} /> {m['issue.versions.source']()}</label
						>
					{/if}
				</div>
				{#if changes}
					<TextDiff before={selected.old_value} after={selected.new_value} html={selected.field === 'body'} />
				{:else}
					<div class="grid min-w-0 gap-4 md:grid-cols-2">
						{#each [{ label: m['issue.versions.before'](), value: selected.old_value }, { label: m['issue.versions.after'](), value: selected.new_value }] as side (side.label)}
							<section aria-label={side.label} class="min-w-0 rounded-lg border border-[var(--app-border)] p-3">
								<div class="mb-3 flex items-center justify-between gap-2">
									<h3 class="font-medium">{side.label}</h3>
									{#if onrestore && (side.value || selected.field === 'body')}
										<Button
											variant="outline"
											size="sm"
											disabled={restoring}
											onclick={() => selected && restore(selected, side.value ?? '')}>{m['versions.restore']()}</Button
										>
									{/if}
								</div>
								<div class="max-h-[45dvh] overflow-auto break-words [overflow-wrap:anywhere]">
									{#if !side.value}
										<p class="text-[var(--color-text-tertiary)]">{m['issue.versions.blank']()}</p>
									{:else if selected.field === 'body' && !source}
										<div class="prose prose-sm max-w-none text-inherit [&_img]:max-w-full [&_pre]:whitespace-pre-wrap">
											{@html sanitizeHtml(side.value).replace(/<input\b/g, '<input disabled')}
										</div>
									{:else}
										<pre class="font-mono text-sm whitespace-pre-wrap">{side.value}</pre>
									{/if}
								</div>
							</section>
						{/each}
					</div>
				{/if}
			{/if}
		{/if}
	</Dialog.Content>
</Dialog.Root>
