<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import * as AlertDialog from '$lib/components/ui/alert-dialog';
	import * as Select from '$lib/components/ui/select';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Switch } from '$lib/components/ui/switch';
	import { Textarea } from '$lib/components/ui/textarea';
	import LoadingState from '$lib/components/shared/LoadingState.svelte';
	import ErrorState from '$lib/components/shared/ErrorState.svelte';
	import { getGitHubStatus } from '$lib/api/github';
	import { createDevMachine, deleteDevMachineEnvironment, deleteDevMachineScopeSetting, getDevMachinePolicy, getDevMachineScopeSetting, listDevMachineEnvironments, updateDevMachinePolicy, updateDevMachineScopeSetting } from '$lib/api/dev-machines';
	import { getWorkspace } from '$lib/api/workspaces';
	import type { DevMachineEnvironment, DevMachinePolicy } from '$lib/types/dev-machine';
	import type { GitHubRepo } from '$lib/types/github';
	import { appToast } from '$lib/features/toast/toast';

	const slug = $derived(page.params.workspaceSlug ?? '');
	let policy = $state<DevMachinePolicy | null>(null);
	let repositories = $state('');
	let saving = $state(false);
	let loading = $state(true);
	let failed = $state(false);
	let canAdmin = $state(false);
	let linkedRepositories = $state<GitHubRepo[]>([]);
	let environments = $state<DevMachineEnvironment[]>([]);
	let defaultRepositoryId = $state('none');
	let defaultEnvironmentId = $state('standard');
	let builderBusy = $state(false);
	let environmentToDelete = $state<DevMachineEnvironment | null>(null);
	let environmentDeleteOpen = $state(false);
	let environmentDeleteBusy = $state(false);
	const builderSizes = [
		{ id: 'medium', diskGb: 50 },
		{ id: 'small', diskGb: 20 }
	] as const;

	const readyEnvironments = $derived(environments.filter((item) => item.status === 'ready'));

	onMount(() => void load());

	async function load() {
		loading = true;
		failed = false;
		try {
			const [loaded, workspace, github, scope, availableEnvironments] = await Promise.all([
				getDevMachinePolicy(slug), getWorkspace(slug), getGitHubStatus(slug),
				getDevMachineScopeSetting(slug, 'workspace'), listDevMachineEnvironments(slug)
			]);
			policy = { ...loaded, allowed_providers: loaded.allowed_providers ?? [], allowed_repositories: loaded.allowed_repositories ?? [] };
			repositories = policy.allowed_repositories.join('\n');
			canAdmin = workspace.current_user_role === 'owner' || workspace.current_user_role === 'admin';
			linkedRepositories = github.repos ?? [];
			environments = availableEnvironments ?? [];
			defaultRepositoryId = scope.github_repo_id ?? 'none';
			defaultEnvironmentId = scope.environment_id ?? 'standard';
		} catch (error) {
			failed = true;
			appToast.apiError(error, 'Failed to load Dev Machine policy');
		} finally {
			loading = false;
		}
	}

	async function save(event: SubmitEvent) {
		event.preventDefault();
		if (!policy) return;
		if (!canAdmin) return;
		saving = true;
		try {
			policy = await updateDevMachinePolicy(slug, {
				enabled: policy.enabled,
				max_concurrent_machines: policy.max_concurrent_machines,
				max_machines_per_user: policy.max_machines_per_user,
				max_daily_agent_runs: policy.max_daily_agent_runs,
				max_runtime_minutes: policy.max_runtime_minutes,
				max_disk_gb: policy.max_disk_gb,
				allowed_providers: policy.allowed_providers,
				allowed_repositories: repositories.split('\n').map((line) => line.trim()).filter(Boolean),
				allow_custom_providers: policy.allow_custom_providers,
				idle_pause_minutes: policy.idle_pause_minutes
			});
			const repository = linkedRepositories.find((item) => item.id === defaultRepositoryId);
			if (defaultRepositoryId === 'none' && defaultEnvironmentId === 'standard') {
				await deleteDevMachineScopeSetting(slug, 'workspace');
			} else {
				await updateDevMachineScopeSetting(slug, {
					scope_type: 'workspace',
					github_repo_id: repository?.id,
					base_branch: repository?.default_branch,
					environment_id: defaultEnvironmentId === 'standard' ? undefined : defaultEnvironmentId
				});
			}
			appToast.success('Dev Machine policy saved');
		} catch (error) {
			appToast.apiError(error, 'Failed to save Dev Machine policy');
		} finally {
			saving = false;
		}
	}

	function toggleProvider(provider: string) {
		if (!policy) return;
		policy.allowed_providers = policy.allowed_providers.includes(provider)
			? policy.allowed_providers.filter((item) => item !== provider)
			: [...policy.allowed_providers, provider];
	}

	async function createEnvironmentBuilder() {
		const builderSize = builderSizes.find((size) => size.diskGb <= (policy?.max_disk_gb ?? 0))?.id;
		if (!builderSize) {
			appToast.error('Increase the workspace maximum disk policy to at least 20 GB before creating an Environment Builder');
			return;
		}
		builderBusy = true;
		try {
			const machine = await createDevMachine(slug, {
				size: builderSize, services: { ide: true, browser: false },
				agents: [], env_vars: [], keep_running: true, environment_builder: true
			});
			appToast.success('Environment Builder queued');
			await goto(`/${slug}/machines/${machine.id}`);
		} catch (error) {
			appToast.apiError(error, 'Failed to create Environment Builder');
		} finally {
			builderBusy = false;
		}
	}

	async function deleteEnvironment() {
		if (!environmentToDelete) return;
		environmentDeleteBusy = true;
		try {
			await deleteDevMachineEnvironment(slug, environmentToDelete.id);
			appToast.success('Development environment deletion requested');
			environmentToDelete = null;
			environmentDeleteOpen = false;
			await load();
		} catch (error) {
			appToast.apiError(error, 'Failed to delete development environment');
		} finally {
			environmentDeleteBusy = false;
		}
	}
</script>

{#if loading}<LoadingState />
{:else if failed || !policy}<ErrorState message="Unable to load Dev Machine policy" onretry={load} />
{:else}
	<form onsubmit={save} class="mx-auto max-w-3xl space-y-6 p-6">
		<div><h1 class="text-lg font-semibold">Dev Machines</h1><p class="mt-1 text-sm text-[var(--color-text-tertiary)]">Control execution capacity, repositories, and provider access for this workspace.</p></div>
		{#if !canAdmin}<p class="rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-3 text-xs text-[var(--color-text-tertiary)]">Only workspace owners and admins can change this policy.</p>{/if}
		<fieldset disabled={!canAdmin} class="space-y-6">
		<section class="space-y-4 rounded-xl border border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-5">
			<label class="flex items-center justify-between gap-4"><div><p class="text-sm font-medium">Enable Dev Machines</p><p class="text-xs text-[var(--color-text-tertiary)]">The server must also have the manager, gateway, images, wildcard DNS, and TLS configured.</p></div><Switch bind:checked={policy.enabled} /></label>
			<div class="grid gap-4 sm:grid-cols-2">
				<label class="space-y-1"><Label>Concurrent machines</Label><Input type="number" min="0" max="100" bind:value={policy.max_concurrent_machines} /></label>
				<label class="space-y-1"><Label>Machines per user</Label><Input type="number" min="0" max="50" bind:value={policy.max_machines_per_user} /></label>
				<label class="space-y-1"><Label>Daily agent runs</Label><Input type="number" min="0" bind:value={policy.max_daily_agent_runs} /></label>
				<label class="space-y-1"><Label>Maximum runtime, minutes</Label><Input type="number" min="5" max="1440" bind:value={policy.max_runtime_minutes} /></label>
				<label class="space-y-1"><Label>Maximum disk, GB</Label><Input type="number" min="20" max="2048" bind:value={policy.max_disk_gb} /></label>
				<label class="space-y-1"><Label>Pause after inactivity, minutes</Label><Input type="number" min="5" max="10080" bind:value={policy.idle_pause_minutes} /></label>
			</div>
		</section>
		<section class="space-y-4 rounded-xl border border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-5">
			<div class="flex items-start justify-between gap-4"><div><h2 class="text-sm font-semibold">Development environments</h2><p class="text-xs text-[var(--color-text-tertiary)]">Configure a writable builder through code-server or terminal, pause it, then save an immutable local snapshot.</p></div><Button type="button" variant="outline" onclick={createEnvironmentBuilder} disabled={builderBusy}>{builderBusy ? 'Creating...' : 'New Environment Builder'}</Button></div>
			{#if environments.length === 0}<p class="text-xs text-[var(--color-text-tertiary)]">No saved environments yet.</p>{:else}<div class="space-y-2">{#each environments as environment}<div class="flex items-center justify-between gap-3 rounded-lg border border-[var(--app-border)] p-3"><div class="min-w-0"><p class="truncate text-sm font-medium">{environment.name}</p><p class="truncate text-xs text-[var(--color-text-tertiary)]">{environment.image_ref}</p><p class="mt-1 text-[10px] capitalize text-[var(--color-text-tertiary)]">{environment.status.replaceAll('_', ' ')}</p></div><Button type="button" size="sm" variant="outline" disabled={environment.status === 'delete_requested'} onclick={() => { environmentToDelete = environment; environmentDeleteOpen = true; }}>Delete</Button></div>{/each}</div>{/if}
		</section>
		<section class="space-y-4 rounded-xl border border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-5">
			<div><h2 class="text-sm font-semibold">Workspace defaults</h2><p class="text-xs text-[var(--color-text-tertiary)]">Teams, projects, and issues can override these values.</p></div>
			<div class="grid gap-4 sm:grid-cols-2">
				<div class="space-y-1"><Label>Development repository</Label><Select.Root type="single" value={defaultRepositoryId} onValueChange={(value) => value && (defaultRepositoryId = value)}><Select.Trigger class="w-full">{linkedRepositories.find((item) => item.id === defaultRepositoryId)?.full_name ?? 'No default repository'}</Select.Trigger><Select.Content><Select.Item value="none" label="No default repository">No default repository</Select.Item>{#each linkedRepositories as repository}<Select.Item value={repository.id} label={repository.full_name}>{repository.full_name}</Select.Item>{/each}</Select.Content></Select.Root></div>
				<div class="space-y-1"><Label>Development environment</Label><Select.Root type="single" value={defaultEnvironmentId} onValueChange={(value) => value && (defaultEnvironmentId = value)}><Select.Trigger class="w-full">{readyEnvironments.find((item) => item.id === defaultEnvironmentId)?.name ?? 'Standard Kuayle environment'}</Select.Trigger><Select.Content><Select.Item value="standard" label="Standard Kuayle environment">Standard Kuayle environment</Select.Item>{#each readyEnvironments as environment}<Select.Item value={environment.id} label={environment.name}>{environment.name}</Select.Item>{/each}</Select.Content></Select.Root></div>
			</div>
		</section>
		<section class="space-y-4 rounded-xl border border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-5">
			<div><h2 class="text-sm font-semibold">Providers</h2><p class="text-xs text-[var(--color-text-tertiary)]">Only selected providers can be attached to new machines.</p></div>
			<div class="grid gap-3 sm:grid-cols-3">{#each ['claude-code','opencode','codex'] as provider}<label class="flex items-center justify-between gap-3 rounded-lg border border-[var(--app-border)] p-3"><span class="text-sm">{provider}</span><Switch aria-label={provider} checked={policy.allowed_providers.includes(provider)} onCheckedChange={() => toggleProvider(provider)} /></label>{/each}</div>
			<label class="flex items-center gap-2 text-sm"><Switch bind:checked={policy.allow_custom_providers} />Allow admin-configured custom CLI providers</label>
		</section>
		<section class="space-y-3 rounded-xl border border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-5"><div><h2 class="text-sm font-semibold">Repository allowlist</h2><p class="text-xs text-[var(--color-text-tertiary)]">One <code>owner/repository</code> per line. Empty allows any linked repository.</p></div><Textarea bind:value={repositories} rows={6} class="font-mono text-xs" /></section>
		<div class="flex justify-end"><Button type="submit" disabled={saving}>{saving ? 'Saving...' : 'Save policy'}</Button></div>
		</fieldset>
	</form>
	<AlertDialog.Root bind:open={environmentDeleteOpen}><AlertDialog.Content><AlertDialog.Header><AlertDialog.Title>Delete development environment?</AlertDialog.Title><AlertDialog.Description>{environmentToDelete?.name} will be marked for deletion. New machines will no longer be able to use the image after cleanup; existing machine history is retained.</AlertDialog.Description></AlertDialog.Header><AlertDialog.Footer><AlertDialog.Cancel onclick={() => (environmentToDelete = null)}>Cancel</AlertDialog.Cancel><AlertDialog.Action variant="destructive" onclick={deleteEnvironment} disabled={environmentDeleteBusy}>{environmentDeleteBusy ? 'Deleting...' : 'Delete environment'}</AlertDialog.Action></AlertDialog.Footer></AlertDialog.Content></AlertDialog.Root>
{/if}
