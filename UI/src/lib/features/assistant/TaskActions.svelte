<script lang="ts">
	import { Button } from '$lib/components/ui/button/index.js';
	import type { TaskAction } from './task-actions';
	// The task's next steps as one row of labelled buttons: the primary step
	// first, then its secondary companions. Everything else lives in TaskMenu.
	let { actions }: { actions: TaskAction[] } = $props();
	const external = (action: TaskAction) => (action.external ? { target: '_blank', rel: 'noopener noreferrer' } : {});
	const main = $derived(
		actions
			.filter((a) => a.group === 'main')
			.sort((a, b) => Number(b.variant === 'primary') - Number(a.variant === 'primary'))
	);
</script>

{#if main.length}
	<div class="flex flex-wrap items-center gap-1.5">
		{#each main as action (action.id)}<Button
				variant={action.variant === 'primary' ? 'default' : 'outline'}
				size="sm"
				{...external(action)}
				href={action.href}
				title={action.title}
				disabled={action.disabled}
				onclick={action.onclick}><action.icon />{action.label}</Button
			>{/each}
	</div>
{/if}
