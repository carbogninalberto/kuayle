<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { TriangleAlert } from '@lucide/svelte';
	import * as Alert from '$lib/components/ui/alert';
	import * as AlertDialog from '$lib/components/ui/alert-dialog';
	import * as Dialog from '$lib/components/ui/dialog';
	import * as Select from '$lib/components/ui/select';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Switch } from '$lib/components/ui/switch';
	import { Textarea } from '$lib/components/ui/textarea';
	import LoadingState from '$lib/components/shared/LoadingState.svelte';
	import ErrorState from '$lib/components/shared/ErrorState.svelte';
	import MachineResourceSliders from '$lib/features/dev-machines/MachineResourceSliders.svelte';
	import { getGitHubStatus } from '$lib/api/github';
	import { createDevMachine, deleteDevMachineEnvironment, deleteDevMachineScopeSetting, getDevMachinePolicy, getDevMachineScopeSetting, listDevMachineEnvironments, updateDevMachinePolicy, updateDevMachineScopeSetting } from '$lib/api/dev-machines';
	import {
		DEFAULT_MACHINE_RESOURCES,
		MACHINE_DISK_GB,
		normalizeMachineResources,
		resolveMachineSizeOptions
	} from '$lib/features/dev-machines/machine-size';
	import { getWorkspace } from '$lib/api/workspaces';
	import type { DevMachineEnvironment, DevMachinePolicy } from '$lib/types/dev-machine';
	import type { GitHubRepo } from '$lib/types/github';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/paraglide/messages.js';
	import { getLocale, setLocale } from '$lib/paraglide/runtime.js';

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
	let environmentBuilderOpen = $state(false);
	let builderCpuMillis = $state(DEFAULT_MACHINE_RESOURCES.cpu_millis);
	let builderMemoryMb = $state(DEFAULT_MACHINE_RESOURCES.memory_mb);
	let builderDiskGb = $state(DEFAULT_MACHINE_RESOURCES.disk_gb);

	const readyEnvironments = $derived(environments.filter((item) => item.status === 'ready'));
	const builderSizeOptions = $derived(
		resolveMachineSizeOptions(
			policy?.size_options,
			policy?.max_disk_gb ?? MACHINE_DISK_GB[MACHINE_DISK_GB.length - 1]
		)
	);
	const builderResources = $derived(
		normalizeMachineResources(
			{ cpu_millis: builderCpuMillis, memory_mb: builderMemoryMb, disk_gb: builderDiskGb },
			builderSizeOptions
		)
	);
	const canCreateEnvironmentBuilder = $derived(
		canAdmin && !builderBusy && builderSizeOptions.disk_gb.length > 0
	);

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
			appToast.apiError(error, m['settings.dev_machines.failed_load']());
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
			appToast.success(m['settings.dev_machines.saved']());
		} catch (error) {
			appToast.apiError(error, m['settings.dev_machines.failed_save']());
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

	function openEnvironmentBuilder() {
		if (!canAdmin) return;
		builderCpuMillis = DEFAULT_MACHINE_RESOURCES.cpu_millis;
		builderMemoryMb = DEFAULT_MACHINE_RESOURCES.memory_mb;
		builderDiskGb = DEFAULT_MACHINE_RESOURCES.disk_gb;
		environmentBuilderOpen = true;
	}

	async function createEnvironmentBuilder(event: SubmitEvent) {
		event.preventDefault();
		if (!canCreateEnvironmentBuilder) return;
		builderBusy = true;
		try {
			const machine = await createDevMachine(slug, {
				size: 'custom',
				cpu_millis: builderResources.cpu_millis,
				memory_mb: builderResources.memory_mb,
				disk_gb: builderResources.disk_gb,
				services: { ide: true, browser: false },
				agents: [], env_vars: [], keep_running: true, environment_builder: true
			});
			appToast.success(m['settings.dev_machines.env_queued']());
			environmentBuilderOpen = false;
			await goto(`/${slug}/machines/${machine.id}`);
		} catch (error) {
			appToast.apiError(error, m['settings.dev_machines.failed_create_env']());
		} finally {
			builderBusy = false;
		}
	}

	async function deleteEnvironment() {
		if (!environmentToDelete) return;
		environmentDeleteBusy = true;
		try {
			await deleteDevMachineEnvironment(slug, environmentToDelete.id);
			appToast.success(m['settings.dev_machines.env_deletion_requested']());
			environmentToDelete = null;
			environmentDeleteOpen = false;
			await load();
		} catch (error) {
			appToast.apiError(error, m['settings.dev_machines.failed_delete_env']());
		} finally {
			environmentDeleteBusy = false;
		}
	}
</script>

{#if loading}<LoadingState />
{:else if failed || !policy}<ErrorState message={m['settings.dev_machines.unable_load']()} onretry={load} />
{:else}
	<form onsubmit={save} class="mx-auto max-w-3xl space-y-6 p-6">
		<div><h1 class="text-lg font-semibold">{m['settings.dev_machines.title']()}</h1><p class="mt-1 text-sm text-[var(--color-text-tertiary)]">{m['settings.dev_machines.desc']()}</p></div>
		{#if policy.disk_quota_mode === 'monitored'}
			<Alert.Root class="border-[var(--color-warning)] bg-[var(--color-bg-secondary)] text-[var(--color-warning)]">
				<TriangleAlert />
				<Alert.Title>{m['settings.dev_machines.monitored_quota_title']()}</Alert.Title>
				<Alert.Description class="text-[var(--color-text-secondary)]">{m['settings.dev_machines.monitored_quota_description']()}</Alert.Description>
			</Alert.Root>
		{/if}
		{#if !canAdmin}<p class="rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-3 text-xs text-[var(--color-text-tertiary)]">{m['settings.dev_machines.admin_only']()}</p>{/if}
		<fieldset disabled={!canAdmin} class="space-y-6">
		<section class="space-y-4 rounded-xl border border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-5">
			<label class="flex items-center justify-between gap-4"><div><p class="text-sm font-medium">{m['settings.dev_machines.enable']()}</p><p class="text-xs text-[var(--color-text-tertiary)]">{m['settings.dev_machines.enable_desc']()}</p></div><Switch bind:checked={policy.enabled} /></label>
			<div class="grid gap-4 sm:grid-cols-2">
				<label class="space-y-1"><Label>{m['settings.dev_machines.concurrent']()}</Label><Input type="number" min="0" max="100" bind:value={policy.max_concurrent_machines} /></label>
				<label class="space-y-1"><Label>{m['settings.dev_machines.per_user']()}</Label><Input type="number" min="0" max="50" bind:value={policy.max_machines_per_user} /></label>
				<label class="space-y-1"><Label>{m['settings.dev_machines.daily_runs']()}</Label><Input type="number" min="0" bind:value={policy.max_daily_agent_runs} /></label>
				<label class="space-y-1"><Label>{m['settings.dev_machines.max_runtime']()}</Label><Input type="number" min="5" max="1440" bind:value={policy.max_runtime_minutes} /></label>
				<label class="space-y-1"><Label>{m['settings.dev_machines.max_disk']()}</Label><Input type="number" min="20" max="2048" bind:value={policy.max_disk_gb} /></label>
				<label class="space-y-1"><Label>{m['settings.dev_machines.idle_pause']()}</Label><Input type="number" min="5" max="10080" bind:value={policy.idle_pause_minutes} /></label>
			</div>
		</section>
		<section class="space-y-4 rounded-xl border border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-5">
			<div class="flex items-start justify-between gap-4"><div><h2 class="text-sm font-semibold">{m['settings.dev_machines.environments']()}</h2><p class="text-xs text-[var(--color-text-tertiary)]">{m['settings.dev_machines.environments_desc']()}</p></div><Button type="button" variant="outline" onclick={openEnvironmentBuilder} disabled={builderBusy}>{m['settings.dev_machines.new_env']()}</Button></div>
			{#if environments.length === 0}<p class="text-xs text-[var(--color-text-tertiary)]">{m['settings.dev_machines.no_envs']()}</p>{:else}<div class="space-y-2">{#each environments as environment}<div class="flex items-center justify-between gap-3 rounded-lg border border-[var(--app-border)] p-3"><div class="min-w-0"><p class="truncate text-sm font-medium">{environment.name}</p><p class="truncate text-xs text-[var(--color-text-tertiary)]">{environment.image_ref}</p><p class="mt-1 text-[10px] capitalize text-[var(--color-text-tertiary)]">{environment.status.replaceAll('_', ' ')}</p></div><Button type="button" size="sm" variant="outline" disabled={environment.status === 'delete_requested'} onclick={() => { environmentToDelete = environment; environmentDeleteOpen = true; }}>{m['settings.dev_machines.delete']()}</Button></div>{/each}</div>{/if}
		</section>
		<section class="space-y-4 rounded-xl border border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-5">
			<div><h2 class="text-sm font-semibold">{m['settings.dev_machines.workspace_defaults']()}</h2><p class="text-xs text-[var(--color-text-tertiary)]">{m['settings.dev_machines.defaults_desc']()}</p></div>
			<div class="grid gap-4 sm:grid-cols-2">
				<div class="space-y-1"><Label>{m['settings.dev_machines.repo']()}</Label><Select.Root type="single" value={defaultRepositoryId} onValueChange={(value) => value && (defaultRepositoryId = value)}><Select.Trigger class="w-full">{linkedRepositories.find((item) => item.id === defaultRepositoryId)?.full_name ?? m['settings.dev_machines.no_default_repo']()}</Select.Trigger><Select.Content><Select.Item value="none" label={m['settings.dev_machines.no_default_repo']()}>{m['settings.dev_machines.no_default_repo']()}</Select.Item>{#each linkedRepositories as repository}<Select.Item value={repository.id} label={repository.full_name}>{repository.full_name}</Select.Item>{/each}</Select.Content></Select.Root></div>
				<div class="space-y-1"><Label>{m['settings.dev_machines.env']()}</Label><Select.Root type="single" value={defaultEnvironmentId} onValueChange={(value) => value && (defaultEnvironmentId = value)}><Select.Trigger class="w-full">{readyEnvironments.find((item) => item.id === defaultEnvironmentId)?.name ?? m['settings.dev_machines.default_env']()}</Select.Trigger><Select.Content><Select.Item value="standard" label={m['settings.dev_machines.default_env']()}>{m['settings.dev_machines.default_env']()}</Select.Item>{#each readyEnvironments as environment}<Select.Item value={environment.id} label={environment.name}>{environment.name}</Select.Item>{/each}</Select.Content></Select.Root></div>
			</div>
		</section>
		<section class="space-y-4 rounded-xl border border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-5">
			<div><h2 class="text-sm font-semibold">{m['settings.dev_machines.providers']()}</h2><p class="text-xs text-[var(--color-text-tertiary)]">{m['settings.dev_machines.providers_desc']()}</p></div>
			<div class="grid gap-3 sm:grid-cols-4">{#each ['claude-code','opencode','codex','pi','qwen-code','crush','kimi','goose'] as provider}<label class="flex items-center justify-between gap-3 rounded-lg border border-[var(--app-border)] p-3"><span class="text-sm">{provider}</span><Switch aria-label={provider} checked={policy.allowed_providers.includes(provider)} onCheckedChange={() => toggleProvider(provider)} /></label>{/each}</div>
			<label class="flex items-center gap-2 text-sm"><Switch bind:checked={policy.allow_custom_providers} />{m['settings.dev_machines.allow_custom']()}</label>
		</section>
		<section class="space-y-3 rounded-xl border border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-5"><div><h2 class="text-sm font-semibold">{m['settings.dev_machines.repo_allowlist']()}</h2><p class="text-xs text-[var(--color-text-tertiary)]">{@html m['settings.dev_machines.repo_allowlist_desc']()}</p></div><Textarea bind:value={repositories} rows={6} class="font-mono text-xs" /></section>
		<div class="flex justify-end"><Button type="submit" disabled={saving}>{saving ? m['settings.saving']() : m['settings.dev_machines.save_policy']()}</Button></div>
		</fieldset>
	</form>
	<Dialog.Root bind:open={environmentBuilderOpen}>
		<Dialog.Content class="max-h-[90vh] overflow-y-auto sm:max-w-2xl">
			<form onsubmit={createEnvironmentBuilder} class="space-y-5">
				<Dialog.Header>
					<Dialog.Title>{m['settings.dev_machines.new_env']()}</Dialog.Title>
					<Dialog.Description>{m['settings.dev_machines.new_env_desc']()}</Dialog.Description>
				</Dialog.Header>
				<MachineResourceSliders
					bind:cpu_millis={builderCpuMillis}
					bind:memory_mb={builderMemoryMb}
					bind:disk_gb={builderDiskGb}
					sizeOptions={builderSizeOptions}
				/>
				{#if builderSizeOptions.disk_gb.length === 0}
					<Alert.Root variant="destructive">
						<Alert.Description>{m['settings.dev_machines.increase_disk']()}</Alert.Description>
					</Alert.Root>
				{/if}
				<Dialog.Footer>
					<Button type="button" variant="outline" onclick={() => (environmentBuilderOpen = false)}>{m['common.cancel']()}</Button>
					<Button type="submit" disabled={!canCreateEnvironmentBuilder}>{builderBusy ? m['common.creating']() : m['common.create']()}</Button>
				</Dialog.Footer>
			</form>
		</Dialog.Content>
	</Dialog.Root>
	<AlertDialog.Root bind:open={environmentDeleteOpen}><AlertDialog.Content><AlertDialog.Header><AlertDialog.Title>{m['settings.dev_machines.delete_env_title']()}</AlertDialog.Title><AlertDialog.Description>{m['settings.dev_machines.delete_env_desc']({ name: environmentToDelete?.name ?? '' })}</AlertDialog.Description></AlertDialog.Header><AlertDialog.Footer><AlertDialog.Cancel onclick={() => (environmentToDelete = null)}>{m['settings.cancel']()}</AlertDialog.Cancel><AlertDialog.Action variant="destructive" onclick={deleteEnvironment} disabled={environmentDeleteBusy}>{environmentDeleteBusy ? m['settings.deleting']() : m['settings.dev_machines.delete_env_button']()}</AlertDialog.Action></AlertDialog.Footer></AlertDialog.Content></AlertDialog.Root>
{/if}
