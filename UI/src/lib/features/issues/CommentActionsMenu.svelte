<script lang="ts">
	import { Pencil, Trash2, MoreHorizontal } from '@lucide/svelte';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import { m } from '$lib/features/i18n/messages';

	// The server decides who may edit or delete; these only hide what the
	// caller cannot do. Nothing renders when neither is allowed.
	let {
		canEdit,
		canDelete,
		onedit,
		ondelete
	}: { canEdit: boolean; canDelete: boolean; onedit: () => void; ondelete: () => void } = $props();
</script>

{#if canEdit || canDelete}
	<DropdownMenu.Root>
		<DropdownMenu.Trigger
			class="rounded p-1 text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]"
			aria-label={m['issue.comment_actions']()}
			title={m['issue.comment_actions']()}
			data-testid="comment-actions"><MoreHorizontal size={14} /></DropdownMenu.Trigger
		>
		<DropdownMenu.Content align="end" class="w-36">
			{#if canEdit}
				<DropdownMenu.Item onSelect={onedit} class="gap-2"><Pencil size={14} />{m['common.edit']()}</DropdownMenu.Item>
			{/if}
			{#if canDelete}
				<DropdownMenu.Item variant="destructive" onSelect={ondelete} class="gap-2"
					><Trash2 size={14} />{m['common.delete']()}</DropdownMenu.Item
				>
			{/if}
		</DropdownMenu.Content>
	</DropdownMenu.Root>
{/if}
