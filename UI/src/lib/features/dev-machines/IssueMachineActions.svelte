<script lang="ts">
	import { goto } from '$app/navigation';
	import { Activity, Box, Code2, GitBranch, SquareTerminal } from '@lucide/svelte';
	import { listDevMachines } from '$lib/api/dev-machines';
	import type { Issue } from '$lib/types/issue';
	import type { DevMachine } from '$lib/types/dev-machine';
	import { appToast } from '$lib/features/toast/toast';
	import { Button } from '$lib/components/ui/button';
	import type { IssueMachineIntent } from './IssueMachinePickerDialog.svelte';
	import { m } from '$lib/paraglide/messages.js';
	import { getLocale, setLocale } from '$lib/paraglide/runtime.js';
	import { authState } from '$lib/features/auth/auth.state.svelte';
	import { demoMode } from '$lib/demo';

	let {
		slug,
		issue,
		onaction,
		repositoryOpen = $bindable(false),
		pickerOpen = $bindable(false),
		pickerIntent = $bindable<IssueMachineIntent>('ide')
	}: {
		slug: string; issue: Issue; onaction?: () => void;
		repositoryOpen?: boolean; pickerOpen?: boolean; pickerIntent?: IssueMachineIntent;
	} = $props();

	let machines = $state<DevMachine[]>([]);
	const canUseDevMachines = $derived(!demoMode || authState.user?.is_sysadmin === true);

	async function refresh() {
		try {
			machines = (await listDevMachines(slug, issue.id)).data ?? [];
			return true;
		} catch (error) {
			appToast.apiError(error, 'Unable to load Dev Machines');
			return false;
		}
	}

	function chooseMachine(intent: IssueMachineIntent) {
		pickerIntent = intent;
		pickerOpen = true;
		onaction?.();
	}

	async function viewMachine(anchor = '') {
		if (!(await refresh())) return;
		const target = machines[0];
		onaction?.();
		if (target) await goto(`/${slug}/machines/${target.id}${anchor}`);
		else await goto(`/${slug}/settings/dev-machines?issue_id=${encodeURIComponent(issue.id)}`);
	}
</script>

{#if canUseDevMachines}<div class="min-w-0 space-y-0.5">
	<Button variant="ghost" onclick={() => chooseMachine('ide')} class="w-full min-w-0 justify-start"><Code2 size={14} />{m['machines.open_code_editor_action']()}</Button>
	<Button variant="ghost" onclick={() => chooseMachine('terminal')} class="w-full min-w-0 justify-start"><SquareTerminal size={14} />{m['machines.open_terminal_action']()}</Button>
	<Button variant="ghost" onclick={() => viewMachine('#tasks')} class="w-full min-w-0 justify-start"><Box size={14} />{m['machines.view_machine_tasks']()}</Button>
	<Button variant="ghost" onclick={() => viewMachine('#activity')} class="w-full min-w-0 justify-start"><Activity size={14} />{m['machines.view_machine_activity']()}</Button>
	<Button variant="ghost" onclick={() => { repositoryOpen = true; onaction?.(); }} class="w-full min-w-0 justify-start"><GitBranch size={14} />{m['machines.set_development_defaults']()}</Button>
</div>{/if}
