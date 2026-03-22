<script lang="ts">
	let {
		title,
		message,
		confirmLabel = 'Confirm',
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
</script>

<div class="fixed inset-0 z-50 flex items-center justify-center">
	<!-- svelte-ignore a11y_no_static_element_interactions a11y_click_events_have_key_events -->
	<div class="fixed inset-0 bg-black/50" onclick={oncancel}></div>
	<div
		class="relative z-10 w-full max-w-sm rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-6 shadow-2xl"
	>
		<h3 class="text-sm font-medium text-[var(--color-text-primary)]">{title}</h3>
		<p class="mt-2 text-sm text-[var(--color-text-secondary)]">{message}</p>
		<div class="mt-4 flex justify-end gap-2">
			<button
				onclick={oncancel}
				class="rounded-md border border-[var(--app-border)] px-3 py-1.5 text-sm text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]"
			>
				Cancel
			</button>
			<button
				onclick={onconfirm}
				class="rounded-md px-3 py-1.5 text-sm text-white {variant === 'danger'
					? 'bg-[var(--color-error)] hover:bg-red-600'
					: 'bg-[var(--app-accent)] hover:bg-[var(--app-accent-hover)]'}"
			>
				{confirmLabel}
			</button>
		</div>
	</div>
</div>
