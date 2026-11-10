<script lang="ts">
	import ConfirmDialog from '$lib/components/shared/ConfirmDialog.svelte';
	import { m } from '$lib/features/i18n/messages';
	import { unassignConfirm } from './issue-agents.svelte';
</script>

{#if unassignConfirm.pending}
	<ConfirmDialog
		title={m['agents.assignee.unassign_title']({ name: unassignConfirm.pending.name })}
		message={m['agents.assignee.unassign_message']({ name: unassignConfirm.pending.name })}
		confirmLabel={m['agents.assignee.unassign_confirm']()}
		onconfirm={() => unassignConfirm.answer(true)}
		oncancel={() => unassignConfirm.answer(false)}
	/>
{/if}
