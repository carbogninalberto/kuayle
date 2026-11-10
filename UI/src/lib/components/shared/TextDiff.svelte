<script lang="ts">
	import { Button } from '$lib/components/ui/button/index.js';
	import * as ToggleGroup from '$lib/components/ui/toggle-group/index.js';
	import { m } from '$lib/features/i18n/messages';
	import { diffRows, foldRows, splitPairs, type DiffRow } from './version-diff';

	// A git-style diff of two versions of a text: removed lines in red with
	// "-", added lines in green with "+", changed words marked within a line,
	// and long unchanged runs folded. Versions are compared as readable text
	// (editor HTML is reduced to one line per block) and rendered as text only.
	let {
		before,
		after,
		html
	}: {
		before: string | null | undefined;
		after: string | null | undefined;
		/** Whether the values are editor HTML. */
		html: boolean;
	} = $props();

	let layout = $state<'unified' | 'split'>('unified');
	let expanded = $state<Set<number>>(new Set());

	const rows = $derived(diffRows(before, after, html));
	const blocks = $derived(foldRows(rows));
	const changed = $derived(rows.some((row) => row.kind !== 'same'));

	$effect(() => {
		void before;
		void after;
		expanded = new Set();
	});

	function expand(index: number) {
		expanded = new Set([...expanded, index]);
	}

	const tone = {
		add: 'bg-[var(--status-success)]/10',
		del: 'bg-destructive/10',
		same: ''
	};
	const word = {
		add: 'rounded-sm bg-[var(--status-success)]/30 text-[var(--color-text-primary)]',
		del: 'rounded-sm bg-destructive/30 text-[var(--color-text-primary)]',
		same: ''
	};
	const marker = { add: '+', del: '-', same: ' ' };
</script>

{#snippet lineText(row: DiffRow)}
	{#if row.segments}
		{#each row.segments as segment, index (index)}
			{#if segment.changed}<mark class={word[row.kind]}>{segment.text}</mark>{:else}{segment.text}{/if}
		{/each}
	{:else}
		{row.text || ' '}
	{/if}
{/snippet}

{#snippet number(value: number | undefined)}
	<span aria-hidden="true" class="w-8 shrink-0 pr-2 text-right text-[var(--color-text-tertiary)] select-none"
		>{value ?? ''}</span
	>
{/snippet}

{#snippet unifiedRow(row: DiffRow)}
	<div
		class="flex min-w-0 {tone[row.kind]}"
		role={row.kind === 'same' ? undefined : 'group'}
		aria-label={row.kind === 'add' ? m['versions.added']() : row.kind === 'del' ? m['versions.removed']() : undefined}
	>
		{@render number(row.oldNo)}
		{@render number(row.newNo)}
		<span
			aria-hidden="true"
			class="w-4 shrink-0 select-none {row.kind === 'add'
				? 'text-[var(--status-success)]'
				: row.kind === 'del'
					? 'text-destructive'
					: ''}">{marker[row.kind]}</span
		>
		<span class="min-w-0 flex-1 pr-3 break-words whitespace-pre-wrap">{@render lineText(row)}</span>
	</div>
{/snippet}

{#snippet splitSide(row: DiffRow | undefined, side: 'left' | 'right')}
	<div
		class="flex min-w-0 {row ? tone[row.kind] : 'bg-muted/30'}"
		role={row && row.kind !== 'same' ? 'group' : undefined}
		aria-label={row?.kind === 'add' ? m['versions.added']() : row?.kind === 'del' ? m['versions.removed']() : undefined}
	>
		{#if row}
			{@render number(side === 'left' ? row.oldNo : row.newNo)}
			<span
				aria-hidden="true"
				class="w-4 shrink-0 select-none {row.kind === 'add'
					? 'text-[var(--status-success)]'
					: row.kind === 'del'
						? 'text-destructive'
						: ''}">{marker[row.kind]}</span
			>
			<span class="min-w-0 flex-1 pr-2 break-words whitespace-pre-wrap">{@render lineText(row)}</span>
		{/if}
	</div>
{/snippet}

<div class="grid gap-2">
	<div class="flex items-center justify-between gap-2">
		<span class="text-xs text-[var(--color-text-tertiary)]">
			{m['versions.summary']({
				added: rows.filter((row) => row.kind === 'add').length,
				removed: rows.filter((row) => row.kind === 'del').length
			})}
		</span>
		<ToggleGroup.Root
			type="single"
			variant="outline"
			size="sm"
			value={layout}
			aria-label={m['versions.layout']()}
			onValueChange={(value) => {
				if (value) layout = value as 'unified' | 'split';
			}}
		>
			<ToggleGroup.Item value="unified" class="px-3 text-xs">{m['versions.unified']()}</ToggleGroup.Item>
			<ToggleGroup.Item value="split" class="px-3 text-xs">{m['versions.split']()}</ToggleGroup.Item>
		</ToggleGroup.Root>
	</div>
	<div
		data-testid="text-diff"
		class="max-h-[50dvh] overflow-auto rounded-lg border border-[var(--app-border)] bg-muted/20 py-1 font-mono text-xs leading-relaxed"
	>
		{#if !changed}
			<p class="px-3 py-1 text-[var(--color-text-tertiary)]">{m['versions.no_text_changes']()}</p>
		{/if}
		{#each blocks as block, index (index)}
			{#if block.kind === 'folded' && !expanded.has(index)}
				<Button
					variant="plain"
					class="my-0.5 w-full bg-[var(--color-bg-secondary)] px-3 py-1 text-left text-[11px] text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)]"
					onclick={() => expand(index)}>{m['versions.show_unchanged']({ count: block.rows.length })}</Button
				>
			{:else if layout === 'unified'}
				{#each block.rows as row, rowIndex (rowIndex)}
					{@render unifiedRow(row)}
				{/each}
			{:else}
				{#each splitPairs(block.rows) as pair, rowIndex (rowIndex)}
					<div class="grid grid-cols-2 gap-px">
						{@render splitSide(pair.left, 'left')}
						{@render splitSide(pair.right, 'right')}
					</div>
				{/each}
			{/if}
		{/each}
	</div>
</div>
