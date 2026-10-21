<script lang="ts">
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
		return members.find(m => m.user_id === entry.user_id)?.name ?? (entry.user_id ? 'Former or unavailable member' : 'Unknown author');
	}
</script>

<Dialog.Root bind:open onOpenChange={(value) => { if (value) { source = false; load(); } else request++; }}>
	<Dialog.Trigger class="rounded-md border border-[var(--app-border)] px-2.5 py-1.5 text-xs text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]">Version history</Dialog.Trigger>
	<Dialog.Content class="top-[5dvh] max-h-[90dvh] sm:max-w-4xl overflow-y-auto bg-[var(--color-bg)] text-[var(--color-text-primary)]">
		<Dialog.Header>
			<Dialog.Title>{identifier} version history</Dialog.Title>
			<Dialog.Description>Review recorded title and description changes, oldest to newest.</Dialog.Description>
		</Dialog.Header>
		{#if loading}
			<p role="status">Loading version history…</p>
		{:else if error}
			<div role="alert">Could not load version history. <button class="underline" onclick={load}>Try again</button></div>
		{:else if versions.length === 0}
			<p>No title or description changes recorded yet. Earlier versions are available only when changes were recorded.</p>
		{:else}
			<label class="grid gap-2 text-sm">
				Recorded change
				<select class="w-full min-w-0 rounded-md border border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-2" bind:value={selectedId}>
					{#each versions as entry, index (entry.id)}
						<option value={entry.id}>{index + 1}. {entry.field} · {new Date(entry.created_at).toLocaleString()} · {author(entry)}</option>
					{/each}
				</select>
			</label>
			<div class="flex flex-wrap items-center justify-between gap-2">
				<button class="rounded border border-[var(--app-border)] px-3 py-1 disabled:opacity-40" disabled={selectedIndex <= 0} onclick={() => selectedId = versions[selectedIndex - 1].id}>Previous change</button>
				<span>{selectedIndex + 1} of {versions.length}</span>
				<button class="rounded border border-[var(--app-border)] px-3 py-1 disabled:opacity-40" disabled={selectedIndex >= versions.length - 1} onclick={() => selectedId = versions[selectedIndex + 1].id}>Next change</button>
			</div>
			{#if selected}
				<p class="text-sm break-words">{author(selected)} changed the {selected.field} · <time datetime={selected.created_at}>{new Date(selected.created_at).toLocaleString()}</time></p>
				{#if selected.field === 'description'}
					<label class="flex items-center gap-2"><input type="checkbox" bind:checked={source} /> Show HTML source</label>
				{/if}
				<div class="grid min-w-0 gap-4 md:grid-cols-2">
					{#each [{ label: 'Before', value: selected.old_value }, { label: 'After', value: selected.new_value }] as side}
						<section aria-label={side.label} class="min-w-0 rounded-lg border border-[var(--app-border)] p-3">
							<h3 class="mb-3 font-medium">{side.label}</h3>
							<div class="max-h-[45dvh] overflow-auto break-words [overflow-wrap:anywhere]">
								{#if !side.value}
									<p class="text-[var(--color-text-tertiary)]">Empty</p>
								{:else if selected.field === 'description' && !source}
									<div class="prose prose-sm max-w-none text-inherit [&_img]:max-w-full [&_pre]:whitespace-pre-wrap">{@html sanitizeHtml(side.value)}</div>
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
