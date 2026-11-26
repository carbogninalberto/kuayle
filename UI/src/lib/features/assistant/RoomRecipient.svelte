<script lang="ts">
	import { untrack } from 'svelte';
	import SelectField from '$lib/components/shared/SelectField.svelte';
	import type { AgentTask } from '$lib/api/agent-tasks';
	import { m } from '$lib/features/i18n/messages';
	import type { ConversationState } from './conversation-state.svelte';
	import { useTaskRun } from './task-run.svelte';

	let { chat, task, value = $bindable(), disabled }: {
		chat: ConversationState; task: AgentTask; value: string; disabled: boolean;
	} = $props();
	const run = useTaskRun(untrack(() => chat), () => task, { chart: true });
	const options = $derived(['all', ...(run.pipeline?.lead ? ['lead'] : []),
		...(run.pipeline?.roster.map((member) => member.role) ?? [])].map((role) => ({ value: role, label: '@' + role })));
</script>

<SelectField size="sm" aria-label={m['tasks.room_recipient']()} bind:value {options} {disabled} />
