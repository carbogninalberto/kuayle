<script lang="ts">
	let { message = 'Loading...' }: { message?: string } = $props();
</script>

<div class="flex h-64 items-center justify-center">
	<p class="text-sm text-[var(--color-text-secondary)]">{message}</p>
</div>
