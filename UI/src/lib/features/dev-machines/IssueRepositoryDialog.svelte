<script lang="ts">
	import * as Dialog from '$lib/components/ui/dialog';
	import * as Select from '$lib/components/ui/select';
	import * as Command from '$lib/components/ui/command';
	import { Button } from '$lib/components/ui/button';
	import { Label } from '$lib/components/ui/label';
	import ComboboxPopover from '$lib/components/shared/ComboboxPopover.svelte';
	import { ChevronsUpDown, GitBranch } from 'lucide-svelte';
	import { getGitHubStatus } from '$lib/api/github';
	import { deleteDevMachineScopeSetting, getDevMachineScopeSetting, listDevMachineEnvironments, updateDevMachineScopeSetting } from '$lib/api/dev-machines';
	import type { GitHubRepo } from '$lib/types/github';
	import type { DevMachineEnvironment } from '$lib/types/dev-machine';
	import type { Issue } from '$lib/types/issue';
	import { appToast } from '$lib/features/toast/toast';

	let { open = $bindable(false), slug, issue }: { open: boolean; slug: string; issue: Issue } = $props();
	let repositories = $state<GitHubRepo[]>([]);
	let environments = $state<DevMachineEnvironment[]>([]);
	let repositoryId = $state('inherit');
	let environmentId = $state('inherit');
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
		} catch (error) {
			appToast.apiError(error, 'Failed to load issue development defaults');
		} finally {
			loading = false;
		}
	}

	async function save() {
		saving = true;
		try {
			if (repositoryId === 'inherit' && environmentId === 'inherit') {
				await deleteDevMachineScopeSetting(slug, 'issue', issue.id);
			} else {
				await updateDevMachineScopeSetting(slug, {
					scope_type: 'issue', scope_id: issue.id,
					github_repo_id: selectedRepository?.id,
					base_branch: selectedRepository?.default_branch,
					environment_id: selectedEnvironment?.id
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
		<Dialog.Header><Dialog.Title>Issue development defaults</Dialog.Title><Dialog.Description>Override the project, team, or workspace repository and environment for {issue.identifier}.</Dialog.Description></Dialog.Header>
		<div class="space-y-4">
			<div class="space-y-1.5">
				<Label>Repository</Label>
				<ComboboxPopover bind:open={repositoryOpen} placeholder="Search repositories..." emptyMessage="No repositories found." width="w-[min(28rem,calc(100vw-2rem))]">
					{#snippet trigger()}
						<Button type="button" variant="outline" class="w-full justify-between gap-2 font-normal" disabled={loading} aria-label="Repository">
							<span class="flex min-w-0 items-center gap-2"><GitBranch class="size-3.5 shrink-0 text-[var(--color-text-tertiary)]" /><span class="truncate">{selectedRepository?.full_name ?? 'Use project, team, or workspace default'}</span></span>
							<ChevronsUpDown class="size-3.5 shrink-0 text-[var(--color-text-tertiary)]" />
						</Button>
					{/snippet}
					<Command.Item value="Use project team or workspace default" data-checked={repositoryId === 'inherit'} onSelect={() => { repositoryId = 'inherit'; repositoryOpen = false; }} class="text-[var(--color-text-tertiary)]">Use project, team, or workspace default</Command.Item>
					{#each repositories as repository (repository.id)}
						<Command.Item value={repository.full_name} data-checked={repositoryId === repository.id} onSelect={() => { repositoryId = repository.id; repositoryOpen = false; }} class="flex items-center gap-2"><GitBranch class="size-3.5 text-[var(--color-text-tertiary)]" /><span class="truncate">{repository.full_name}</span></Command.Item>
					{/each}
				</ComboboxPopover>
			</div>
			<div class="space-y-1.5"><Label>Environment</Label><Select.Root type="single" value={environmentId} disabled={loading} onValueChange={(value) => value && (environmentId = value)}><Select.Trigger class="w-full">{selectedEnvironment?.name ?? 'Use project, team, or workspace default'}</Select.Trigger><Select.Content><Select.Item value="inherit" label="Use inherited default">Use inherited default</Select.Item>{#each environments as environment}<Select.Item value={environment.id} label={environment.name}>{environment.name}</Select.Item>{/each}</Select.Content></Select.Root></div>
		</div>
		<Dialog.Footer><Button variant="outline" onclick={() => (open = false)}>Cancel</Button><Button onclick={save} disabled={loading || saving}>{saving ? 'Saving...' : 'Save defaults'}</Button></Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
