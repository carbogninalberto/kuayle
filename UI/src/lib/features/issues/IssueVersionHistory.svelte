<script lang="ts">
	import { m } from '$lib/paraglide/messages.js';
	import { getIssueHistory } from '$lib/api/issues';
	import type { IssueHistory } from '$lib/types/issue';
	import type { WorkspaceMember } from '$lib/types/workspace';
	import VersionHistoryDialog, { type VersionEntry } from '$lib/components/shared/VersionHistoryDialog.svelte';

	let { slug, identifier, members }: { slug: string; identifier: string; members: WorkspaceMember[] } = $props();

	function author(entry: IssueHistory): string {
		return (
			members.find((m) => m.user_id === entry.user_id)?.name ??
			(entry.user_id ? m['issue.versions.member']({ id: entry.user_id.slice(0, 8) }) : m['issue.versions.unknown']())
		);
	}

	async function load(): Promise<VersionEntry[]> {
		const history = await getIssueHistory(slug, identifier);
		// The API orders newest first with PostgreSQL timestamp precision.
		// Reverse it without truncating microseconds through JavaScript Date.
		return (history ?? [])
			.filter((v) => v.field === 'title' || v.field === 'description')
			.reverse()
			.map((v) => ({
				id: v.id,
				field: v.field === 'title' ? 'title' : 'body',
				old_value: v.old_value,
				new_value: v.new_value,
				author: author(v),
				created_at: v.created_at
			}));
	}
</script>

<VersionHistoryDialog
	dialogTitle={m['issue.versions.dialog_title']({ id: identifier })}
	intro={m['issue.versions.intro']()}
	empty={m['issue.versions.empty']()}
	fieldName={(field) =>
		field === 'title' ? m['issue.versions.title_field']() : m['issue.versions.description_field']()}
	{load}
/>
