<script lang="ts">
	import { lineMatches, type DiffFile } from './diff';

	// One file's lines of a unified diff, coloured by kind; lines matching a
	// search query are highlighted. Shared by the diff panel and a file preview.
	let { lines, query = '' }: { lines: DiffFile['lines']; query?: string } = $props();
	const searching = $derived(query.trim() !== '');
	const lineClass = {
		add: 'bg-[var(--status-success)]/10 text-[var(--status-success)]',
		del: 'bg-[var(--status-error)]/10 text-[var(--status-error)]',
		hunk: 'bg-muted text-muted-foreground',
		meta: 'text-muted-foreground',
		context: 'text-foreground'
	};
</script>

<pre class="overflow-x-auto text-xs leading-5"><code
		>{#each lines as line, index (index)}<span
				class="block px-3 whitespace-pre {lineClass[line.kind]} {searching && lineMatches(line.text, query) ? 'bg-[var(--status-warning)]/25' : ''}">{line.text || ' '}</span
			>{/each}</code
	></pre>
