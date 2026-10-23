<script lang="ts">
	import { Button } from '$lib/components/ui/button/index.js';
	import { m } from '$lib/paraglide/messages.js';
	import * as AlertDialog from '$lib/components/ui/alert-dialog/index.js';

	let {
		title,
		message,
		confirmLabel,
		variant = 'danger',
		onconfirm,
		oncancel
	}: {
		title: string;
		message: string;
		confirmLabel?: string;
		variant?: 'danger' | 'default';
		onconfirm: () => void;
		oncancel: () => void;
	} = $props();

	const effectiveConfirmLabel = $derived(confirmLabel ?? m['sharedComponents.confirm_dialog.confirm']());
</script>

<AlertDialog.Root open onOpenChange={(open) => { if (!open) oncancel(); }}>
	<AlertDialog.Content
		class="block max-w-sm gap-0 rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-6 shadow-2xl ring-0 data-[size=default]:max-w-sm data-[size=default]:sm:max-w-sm"
	>
		<AlertDialog.Title class="text-sm font-medium text-[var(--color-text-primary)]">{title}</AlertDialog.Title>
		<AlertDialog.Description class="mt-2 text-sm text-[var(--color-text-secondary)]">{message}</AlertDialog.Description>
		<div class="mt-4 flex justify-end gap-2">
			<AlertDialog.Cancel
				variant="plain"
				class="rounded-md border border-[var(--app-border)] px-3 py-1.5 text-sm text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]"
			>
				{m['sharedComponents.confirm_dialog.cancel']()}
			</AlertDialog.Cancel>
			<Button
				variant="plain"
				onclick={onconfirm}
				class="rounded-md px-3 py-1.5 text-sm text-white {variant === 'danger'
					? 'bg-[var(--color-error)] hover:bg-red-600'
					: 'bg-[var(--app-accent)] hover:bg-[var(--app-accent-hover)]'}"
			>
				{effectiveConfirmLabel}
			</Button>
		</div>
	</AlertDialog.Content>
</AlertDialog.Root>
