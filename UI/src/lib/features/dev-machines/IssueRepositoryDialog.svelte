<script lang="ts">
	import TestCommandField from '$lib/features/dev-machines/TestCommandField.svelte';
	import AgentDefaultsField from '$lib/features/dev-machines/AgentDefaultsField.svelte';
	import { parseTestCommand, formatTestCommand } from '$lib/features/dev-machines/test-command';
	import * as Dialog from '$lib/components/ui/dialog';
	import * as Select from '$lib/components/ui/select';
	import * as Command from '$lib/components/ui/command';
	import { Button } from '$lib/components/ui/button';
	import { Label } from '$lib/components/ui/label';
	import ComboboxPopover from '$lib/components/shared/ComboboxPopover.svelte';
	import { ChevronsUpDown, GitBranch } from '@lucide/svelte';
	import { getGitHubStatus } from '$lib/api/github';
	import { deleteDevMachineScopeSetting, getDevMachineScopeSetting, listDevMachineEnvironments, updateDevMachineScopeSetting } from '$lib/api/dev-machines';
	import type { GitHubRepo } from '$lib/types/github';
	import type { DevMachineEnvironment } from '$lib/types/dev-machine';
	import type { Issue } from '$lib/types/issue';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/paraglide/messages.js';
	import { getLocale, setLocale } from '$lib/paraglide/runtime.js';

	let { open = $bindable(false), slug, issue }: { open: boolean; slug: string; issue: Issue } = $props();
	let repositories = $state<GitHubRepo[]>([]);
	let environments = $state<DevMachineEnvironment[]>([]);
	let repositoryId = $state('inherit');
	let environmentId = $state('inherit');
	let testCommand = $state('');
	let agentProvider = $state('inherit');
	let agentInstructions = $state('');
	let loading = $state(false);
	let saving = $state(false);
	let repositoryOpen = $state(false);

	const selectedRepository = $derived(repositories.find((item) => item.id === repositoryId));
	const selectedEnvironment = $derived(environments.find((item) => item.id === environmentId));

	$effect(() => {
		if (!open) return;
		void load();
	});

	async function load() {
		loading = true;
		try {
			const [github, setting, availableEnvironments] = await Promise.all([
				getGitHubStatus(slug), getDevMachineScopeSetting(slug, 'issue', issue.id), listDevMachineEnvironments(slug)
			]);
			repositories = github.repos ?? [];
			environments = (availableEnvironments ?? []).filter((item) => item.status === 'ready');
			repositoryId = setting.github_repo_id ?? 'inherit';
			environmentId = setting.environment_id ?? 'inherit';
			testCommand = formatTestCommand(setting.test_command); agentProvider = setting.agent_provider ?? 'inherit'; agentInstructions = setting.agent_instructions ?? '';
		} catch (error) {
			appToast.apiError(error, 'Failed to load issue development defaults');
		} finally {
			loading = false;
		}
	}

	async function save() {
		saving = true;
		try {
			const command = parseTestCommand(testCommand);
			if (repositoryId === 'inherit' && environmentId === 'inherit' && command === undefined && agentProvider === 'inherit' && !agentInstructions.trim()) {
				await deleteDevMachineScopeSetting(slug, 'issue', issue.id);
			} else {
				await updateDevMachineScopeSetting(slug, {
					scope_type: 'issue', scope_id: issue.id,
					github_repo_id: selectedRepository?.id,
					base_branch: selectedRepository?.default_branch,
					environment_id: selectedEnvironment?.id,
					test_command: command, agent_provider: agentProvider === 'inherit' ? undefined : agentProvider, agent_instructions: agentInstructions.trim() || undefined
				});
			}
			appToast.success('Issue development defaults saved');
			open = false;
		} catch (error) {
			appToast.apiError(error, 'Failed to save issue development defaults');
		} finally {
			saving = false;
		}
	}
</script>

<Dialog.Root bind:open>
	<Dialog.Content class="sm:max-w-md">
		<Dialog.Header><Dialog.Title>{m['machines.issue_defaults_title']()}</Dialog.Title><Dialog.Description>{m['machines.issue_defaults_desc']({ identifier: issue.identifier })}</Dialog.Description></Dialog.Header>
		<div class="space-y-4">
			<div class="space-y-1.5">
				<Label>{m['machines.repository']()}</Label>
				<ComboboxPopover bind:open={repositoryOpen} placeholder={m['machines.search_repositories']()} emptyMessage={m['machines.no_repositories']()} width="w-[min(28rem,calc(100vw-2rem))]">
					{#snippet trigger(props)}
						<Button {...props} type="button" variant="outline" class="w-full justify-between gap-2 font-normal" disabled={loading} aria-label={m['machines.repository']()}>
							<span class="flex min-w-0 items-center gap-2"><GitBranch class="size-3.5 shrink-0 text-[var(--color-text-tertiary)]" /><span class="truncate">{selectedRepository?.full_name ?? m['machines.use_workspace_default']()}</span></span>
							<ChevronsUpDown class="size-3.5 shrink-0 text-[var(--color-text-tertiary)]" />
						</Button>
					{/snippet}
					<Command.Item value="Use project team or workspace default" data-checked={repositoryId === 'inherit'} onSelect={() => { repositoryId = 'inherit'; repositoryOpen = false; }} class="text-[var(--color-text-tertiary)]">Use project, team, or workspace default</Command.Item>
					{#each repositories as repository (repository.id)}
						<Command.Item value={repository.full_name} data-checked={repositoryId === repository.id} onSelect={() => { repositoryId = repository.id; repositoryOpen = false; }} class="flex items-center gap-2"><GitBranch class="size-3.5 text-[var(--color-text-tertiary)]" /><span class="truncate">{repository.full_name}</span></Command.Item>
					{/each}
				</ComboboxPopover>
			</div>
			<div class="space-y-1.5"><Label>{m['machines.environment']()}</Label><Select.Root type="single" value={environmentId} disabled={loading} onValueChange={(value) => value && (environmentId = value)}><Select.Trigger class="w-full">{selectedEnvironment?.name ?? m['machines.use_workspace_default']()}</Select.Trigger><Select.Content><Select.Item value="inherit" label={m['machines.use_inherited_default']()}>{m['machines.use_inherited_default']()}</Select.Item>{#each environments as environment}<Select.Item value={environment.id} label={environment.name}>{environment.name}</Select.Item>{/each}</Select.Content></Select.Root></div>
			<TestCommandField bind:value={testCommand} disabled={loading || saving} /><AgentDefaultsField bind:provider={agentProvider} bind:instructions={agentInstructions} disabled={loading || saving} />
		</div>
		<Dialog.Footer><Button variant="outline" onclick={() => (open = false)}>{m['common.cancel']()}</Button><Button onclick={save} disabled={loading || saving}>{saving ? m['common.saving']() : m['machines.save_defaults']()}</Button></Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
