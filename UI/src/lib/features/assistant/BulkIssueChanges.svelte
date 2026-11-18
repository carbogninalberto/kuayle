<script lang="ts">
	import type { BulkIssuePayload } from '$lib/api/conversations';
	import { getStatusLabels, getPriorityLabels, type IssueStatus, type IssuePriority } from '$lib/types/issue';
	import { m } from '$lib/features/i18n/messages';

	let { payload }: { payload: BulkIssuePayload } = $props();
	const fields: Record<string, () => string> = {
		status: m['issue.status'], priority: m['issue.priority'], assignee_id: m['issue.assignee'],
		label_ids: m['issue.labels'], cycle_id: m['common.context_menu.cycle'], project_id: m['issue.project']
	};
	function display(key: string, value: unknown): string {
		if (key === 'status' && typeof value === 'string') {
			const labels = getStatusLabels();
			if (Object.hasOwn(labels, value)) return labels[value as IssueStatus];
		}
		if (key === 'priority' && typeof value === 'number') return getPriorityLabels()[value as IssuePriority] ?? String(value);
		if (value === '' || (Array.isArray(value) && !value.length)) return m['issue.history.none']();
		if (Array.isArray(value) && value.every((entry) => typeof entry === 'string')) return value.join(', ');
		return typeof value === 'string' ? value : JSON.stringify(value);
	}
</script>

<ul class="min-w-0 space-y-1 text-xs">
	{#each payload.changes as change, i (i)}
		{@const result = payload.results?.[i]}
		<li class="break-words [overflow-wrap:anywhere]">
			<strong>{typeof change?.identifier === 'string' ? change.identifier : '#' + (i + 1)}</strong>
			{Object.entries(change ?? {}).filter(([key]) => key !== 'identifier').map(([key, value]) =>
				(Object.hasOwn(fields, key) ? fields[key]() : key) + ': ' + display(key, value)).join(' · ')}
			{#if result}
				<span class:text-destructive={!result.success}> · {result.success ? m['wiki.updated_label']() : result.error}</span>
			{/if}
		</li>
	{/each}
</ul>
