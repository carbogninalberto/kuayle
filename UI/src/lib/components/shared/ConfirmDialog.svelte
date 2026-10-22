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
	<AlertDialog.Content>
		<AlertDialog.Header>
			<AlertDialog.Title>{title}</AlertDialog.Title>
			<AlertDialog.Description>{message}</AlertDialog.Description>
		</AlertDialog.Header>
		<AlertDialog.Footer>
			<AlertDialog.Cancel>{m['sharedComponents.confirm_dialog.cancel']()}</AlertDialog.Cancel>
			<Button variant={variant === 'danger' ? 'destructive' : 'default'} onclick={onconfirm}>
				{effectiveConfirmLabel}
			</Button>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>
