<script lang="ts">
	import { m } from '$lib/paraglide/messages.js';
	import { getLocale } from '$lib/paraglide/runtime.js';
	import * as Dialog from '$lib/components/ui/dialog';
	import { getIssueHistory } from '$lib/api/issues';
	import type { IssueHistory } from '$lib/types/issue';
	import type { WorkspaceMember } from '$lib/types/workspace';
	import { sanitizeHtml } from '$lib/security/sanitize';

	let { slug, identifier, members }: { slug: string; identifier: string; members: WorkspaceMember[] } = $props();
	let open = $state(false);
	let loading = $state(false);
	let error = $state(false);
	let versions = $state<IssueHistory[]>([]);
	let selectedId = $state('');
	let source = $state(false);
	let selected = $derived(versions.find(v => v.id === selectedId));
	let selectedIndex = $derived(versions.findIndex(v => v.id === selectedId));
	let request = 0;

	async function load() {
		const currentRequest = ++request;
		loading = true;
		error = false;
		versions = [];
		selectedId = '';
		try {
			const history = await getIssueHistory(slug, identifier);
			if (currentRequest !== request) return;
			// The API orders newest first with PostgreSQL timestamp precision.
			// Reverse it without truncating microseconds through JavaScript Date.
			versions = (history ?? []).filter(v => v.field === 'title' || v.field === 'description').reverse();
			selectedId = versions.at(-1)?.id ?? '';
		} catch {
			if (currentRequest === request) error = true;
		} finally {
			if (currentRequest === request) loading = false;
		}
	}

	function author(entry: IssueHistory): string {
		return members.find(m => m.user_id === entry.user_id)?.name ?? (entry.user_id ? m['issue.versions.member']({ id: entry.user_id.slice(0, 8) }) : m['issue.versions.unknown']());
	}

	function fieldName(field: string): string {
		return field === 'title' ? m['issue.versions.title_field']() : m['issue.versions.description_field']();
	}

	function timestamp(value: string): string {
		return new Date(value).toLocaleString(getLocale());
	}
</script>

<Dialog.Root bind:open onOpenChange={(value) => { if (value) { source = false; load(); } else request++; }}>
	<Dialog.Trigger class="rounded-md border border-[var(--app-border)] px-2.5 py-1.5 text-xs text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]">{m['issue.versions.button']()}</Dialog.Trigger>
	<Dialog.Content class="top-[5dvh] max-h-[90dvh] sm:max-w-4xl overflow-y-auto bg-[var(--color-bg)] text-[var(--color-text-primary)]">
		<Dialog.Header>
			<Dialog.Title>{m['issue.versions.dialog_title']({ id: identifier })}</Dialog.Title>
			<Dialog.Description>{m['issue.versions.intro']()}</Dialog.Description>
		</Dialog.Header>
		{#if loading}
			<p role="status">{m['issue.versions.loading']()}</p>
		{:else if error}
			<div role="alert">{m['issue.versions.error']()} <button class="underline" onclick={load}>{m['issue.versions.retry']()}</button></div>
		{:else if versions.length === 0}
			<p>{m['issue.versions.empty']()}</p>
		{:else}
			<label class="grid gap-2 text-sm">
				{m['issue.versions.change']()}
				<select class="w-full min-w-0 rounded-md border border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-2" bind:value={selectedId}>
					{#each versions as entry, index (entry.id)}
						<option value={entry.id}>{index + 1}. {fieldName(entry.field)} · {timestamp(entry.created_at)} · {author(entry)}</option>
					{/each}
				</select>
			</label>
			<div class="flex flex-wrap items-center justify-between gap-2">
				<button class="rounded border border-[var(--app-border)] px-3 py-1 disabled:opacity-40" disabled={selectedIndex <= 0} onclick={() => selectedId = versions[selectedIndex - 1].id}>{m['issue.versions.previous']()}</button>
				<span>{m['issue.versions.position']({ current: selectedIndex + 1, total: versions.length })}</span>
				<button class="rounded border border-[var(--app-border)] px-3 py-1 disabled:opacity-40" disabled={selectedIndex >= versions.length - 1} onclick={() => selectedId = versions[selectedIndex + 1].id}>{m['issue.versions.next']()}</button>
			</div>
			{#if selected}
				<p class="text-sm break-words">{m['issue.versions.edited']({ author: author(selected), field: fieldName(selected.field) })} · <time datetime={selected.created_at}>{timestamp(selected.created_at)}</time></p>
				{#if selected.field === 'description'}
					<label class="flex items-center gap-2"><input type="checkbox" bind:checked={source} /> {m['issue.versions.source']()}</label>
				{/if}
				<div class="grid min-w-0 gap-4 md:grid-cols-2">
					{#each [{ label: m['issue.versions.before'](), value: selected.old_value }, { label: m['issue.versions.after'](), value: selected.new_value }] as side}
						<section aria-label={side.label} class="min-w-0 rounded-lg border border-[var(--app-border)] p-3">
							<h3 class="mb-3 font-medium">{side.label}</h3>
							<div class="max-h-[45dvh] overflow-auto break-words [overflow-wrap:anywhere]">
								{#if !side.value}
									<p class="text-[var(--color-text-tertiary)]">{m['issue.versions.blank']()}</p>
								{:else if selected.field === 'description' && !source}
									<div class="prose prose-sm max-w-none text-inherit [&_img]:max-w-full [&_pre]:whitespace-pre-wrap">{@html sanitizeHtml(side.value).replace(/<input\b/g, '<input disabled')}</div>
								{:else}
									<pre class="whitespace-pre-wrap font-mono text-sm">{side.value}</pre>
								{/if}
							</div>
						</section>
					{/each}
				</div>
			{/if}
		{/if}
	</Dialog.Content>
</Dialog.Root>
