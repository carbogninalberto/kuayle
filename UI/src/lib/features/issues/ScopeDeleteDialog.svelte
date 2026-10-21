<script lang="ts">
	import * as AlertDialog from '$lib/components/ui/alert-dialog';
	import { Button } from '$lib/components/ui/button';
	import { m } from '$lib/paraglide/messages.js';

	let {
		title,
		message,
		onconfirm,
		oncancel
	}: {
		title: string;
		message: string;
		onconfirm: () => Promise<void>;
		oncancel: () => void;
	} = $props();
	let pending = $state(false);
	async function confirm() {
		if (pending) return;
		pending = true;
		try {
			await onconfirm();
		} finally {
			pending = false;
		}
	}
</script>

<AlertDialog.Root
	open
	onOpenChange={(open) => {
		if (!open && !pending) oncancel();
	}}
>
	<AlertDialog.Content
		onEscapeKeydown={(event) => {
			if (pending) event.preventDefault();
		}}
	>
		<AlertDialog.Header>
			<AlertDialog.Title>{title}</AlertDialog.Title>
			<AlertDialog.Description>{message}</AlertDialog.Description>
		</AlertDialog.Header>
		<AlertDialog.Footer>
			<AlertDialog.Cancel disabled={pending}>{m['common.cancel']()}</AlertDialog.Cancel>
			<Button variant="destructive" disabled={pending} onclick={confirm}
				>{pending ? m['common.saving']() : m['common.delete']()}</Button
			>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>
