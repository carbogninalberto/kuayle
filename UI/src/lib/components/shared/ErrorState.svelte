<script lang="ts">
	let {
		message = 'Something went wrong',
		onretry
	}: { message?: string; onretry?: () => void } = $props();
</script>

<div class="flex h-64 flex-col items-center justify-center gap-3">
	<p class="text-sm text-[var(--color-error)]">{message}</p>
	{#if onretry}
		<button
			onclick={onretry}
			class="rounded-md border border-[var(--app-border)] px-3 py-1.5 text-sm text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]"
		>
			Try again
		</button>
	{/if}
</div>
