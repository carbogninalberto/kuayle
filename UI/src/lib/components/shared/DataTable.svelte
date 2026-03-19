<script lang="ts">
	import type { TableOptions } from '@tanstack/table-core';

	let { data = [], columns = [] }: { data: any[]; columns: any[] } = $props();
</script>

<div class="overflow-x-auto">
	<table class="w-full text-sm">
		<thead>
			<tr class="border-b border-[var(--app-border)]">
				{#each columns as col}
					<th
						class="px-4 py-2 text-left text-xs font-medium text-[var(--color-text-tertiary)]"
						>{col.header}</th
					>
				{/each}
			</tr>
		</thead>
		<tbody>
			{#each data as row}
				<tr class="border-b border-[var(--app-border)] hover:bg-[var(--color-bg-hover)]">
					{#each columns as col}
						<td class="px-4 py-2 text-[var(--color-text-secondary)]"
							>{col.accessor ? row[col.accessor] : ''}</td
						>
					{/each}
				</tr>
			{/each}
		</tbody>
	</table>
</div>
