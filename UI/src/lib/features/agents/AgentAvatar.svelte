<script lang="ts">
	import Bot from '@lucide/svelte/icons/bot';
	import { taskTone, toneDot } from '$lib/features/assistant/task-tone';

	// The avatar of an AI agent: a bot glyph on the agent's colour, never an
	// initial, so agents are told apart from people at a glance. With a task
	// state it carries a status dot.
	let {
		name,
		color = '',
		size = 20,
		state: taskState = null,
		class: className = ''
	}: { name: string; color?: string; size?: number; state?: string | null; class?: string } = $props();
</script>

<span
	class="relative inline-flex shrink-0 items-center justify-center rounded-full bg-[var(--color-bg-tertiary)] text-[var(--color-text-primary)] {className}"
	style="width: {size}px; height: {size}px;{color ? ` background-color: ${color}; color: #fff;` : ''}"
	title={name}
	data-agent-avatar
>
	<Bot size={Math.round(size * 0.62)} aria-hidden="true" />
	{#if taskState}
		<span
			class="absolute -right-0.5 -bottom-0.5 rounded-full ring-1 ring-[var(--color-bg)] {toneDot[taskTone(taskState)]}"
			style="width: {Math.max(5, Math.round(size * 0.3))}px; height: {Math.max(5, Math.round(size * 0.3))}px;"
			aria-hidden="true"
		></span>
	{/if}
</span>
