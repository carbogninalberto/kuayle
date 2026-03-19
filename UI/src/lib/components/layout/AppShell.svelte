<script lang="ts">
	let { children } = $props();
</script>

<div class="flex h-full flex-col">
	{@render children()}
</div>
