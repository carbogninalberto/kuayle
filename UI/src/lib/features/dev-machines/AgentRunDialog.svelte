<script lang="ts">
	import * as Dialog from '$lib/components/ui/dialog';
	import * as Select from '$lib/components/ui/select';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Switch } from '$lib/components/ui/switch';
	import { Textarea } from '$lib/components/ui/textarea';
	import { createAgentRun, listMachineAgentProviders } from '$lib/api/dev-machines';
	import type { AgentProvider, AgentRun, DevMachine } from '$lib/types/dev-machine';
	import { appToast } from '$lib/features/toast/toast';

	let { open = $bindable(false), slug, machine, checkoutId, initialPrompt = '', oncreated }: {
		open: boolean; slug: string; machine: DevMachine; checkoutId?: string; initialPrompt?: string; oncreated?: (run: AgentRun) => void;
	} = $props();

	let providers = $state<AgentProvider[]>([]);
	let providersLoaded = $state(false);
	let providersError = $state(false);
	let loadId = 0;
	let provider = $state<AgentProvider['id']>('opencode');
	const mode = 'autonomous' as const;
	let prompt = $state('');
	let criteria = $state('');
	let testArgv = $state('[]');
	let forbiddenPaths = $state('.env\nsecrets/');
	let allowedSecrets = $state('');
	let maxRuntime = $state(0);
	let pushBranch = $state(true);
	let openPullRequest = $state(false);
	let loading = $state(false);
	let machineRemainingSeconds = $state(0);
	let maxRuntimeCap = $state(0);

	const selectedProvider = $derived(providers.find((item) => item.id === provider));
	const selectedProviderLabel = $derived(selectedProvider?.display_name ?? (providers.length === 0 ? 'Loading providers...' : 'Select provider'));
	const submitEnabled = $derived(providersLoaded && !providersError && !loading && !!prompt.trim() && !!selectedProvider && maxRuntime >= 30 && maxRuntime <= maxRuntimeCap);

	$effect(() => {
		if (!open) return;
		prompt = initialPrompt;
		machineRemainingSeconds = Math.max(0, Math.floor((new Date(machine.expires_at).getTime() - Date.now()) / 1000));
		maxRuntimeCap = Math.min(machine.max_runtime_minutes * 60, machineRemainingSeconds, 86400);
		maxRuntime = maxRuntimeCap >= 30 ? Math.min(3600, maxRuntimeCap) : 0;
		pushBranch = !!checkoutId || !!machine.repository_affinity_id;
		openPullRequest = false;
		void loadProviders();
	});

	async function loadProviders() {
		const currentLoadId = ++loadId;
		providersLoaded = false;
		providersError = false;
		try {
			const items = await listMachineAgentProviders(slug, machine.id);
			if (currentLoadId !== loadId) return;
			providers = (items ?? [])
				.map((item) => ({ ...item, required_secrets: item.required_secrets ?? [], supported_modes: item.supported_modes ?? [] }))
				.filter((item) => item.supported_modes.includes('autonomous'));
			const configured = providers.find((item) => item.id === provider) ?? providers[0];
			if (configured) {
				provider = configured.id;
				allowedSecrets = configured.required_secrets.join('\n');
			} else {
				providersError = true;
			}
		} catch {
			if (currentLoadId !== loadId) return;
			providersError = true;
		} finally {
			if (currentLoadId === loadId) providersLoaded = true;
		}
	}

	function selectProvider(value: AgentProvider['id']) {
		provider = value;
		const p = providers.find((item) => item.id === value);
		allowedSecrets = (p?.required_secrets ?? []).join('\n');
	}

	function setPushBranch(value: boolean) {
		pushBranch = value;
		if (!value) openPullRequest = false;
	}

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		let parsedTest: string[];
		try {
			parsedTest = JSON.parse(testArgv);
			if (!Array.isArray(parsedTest) || parsedTest.some((part) => typeof part !== 'string')) throw new Error();
		} catch {
			appToast.error('Test command must be a JSON argv array');
			return;
		}
		loading = true;
		try {
			const run = await createAgentRun(slug, machine.id, {
				checkout_id: checkoutId, use_root_workspace: !checkoutId && !machine.repository_affinity_id,
				provider, mode, prompt, acceptance_criteria: criteria.split('\n').map((line) => line.trim()).filter(Boolean),
				allowed_commands: [], forbidden_paths: forbiddenPaths.split('\n').map((line) => line.trim()).filter(Boolean),
				test_command: parsedTest, max_runtime_seconds: maxRuntime,
				allowed_secrets: allowedSecrets.split('\n').map((line) => line.trim()).filter(Boolean), push_branch: pushBranch, open_pull_request: openPullRequest
			});
			appToast.success('Agent run queued');
			open = false;
			oncreated?.(run);
		} catch (error) {
			appToast.apiError(error, 'Failed to queue agent run');
		} finally {
			loading = false;
		}
	}
</script>

<Dialog.Root bind:open>
	<Dialog.Content class="max-h-[90vh] overflow-y-auto border-[var(--app-border)] bg-[var(--color-bg-secondary)] sm:max-w-xl">
		<form onsubmit={submit} class="space-y-4">
			<Dialog.Header><Dialog.Title>Run Agent</Dialog.Title><Dialog.Description>{machine.repo_owner && machine.repo_name ? `${machine.repo_owner}/${machine.repo_name} on ${machine.working_branch}` : 'Select a ready issue checkout from the machine page.'}</Dialog.Description></Dialog.Header>
			{#if providersError}
				<div class="flex items-center justify-between gap-3 text-xs text-red-400"><span>No autonomous provider is available.</span><Button type="button" size="xs" variant="outline" onclick={loadProviders}>Retry</Button></div>
			{/if}
			<div class="grid gap-3 sm:grid-cols-2">
				<div class="space-y-1"><Label>Provider</Label><Select.Root type="single" value={provider} disabled={!providersLoaded || providersError} onValueChange={(value) => value && selectProvider(value as AgentProvider['id'])}><Select.Trigger aria-label="Provider" class="w-full">{selectedProviderLabel}</Select.Trigger><Select.Content>{#each providers as item}<Select.Item value={item.id} label={item.display_name}>{item.display_name}</Select.Item>{/each}</Select.Content></Select.Root></div>
				<div class="space-y-1"><Label>Mode</Label><div class="flex h-9 items-center rounded-md border border-[var(--app-border)] px-3 text-sm">Autonomous</div></div>
			</div>
			<label class="block space-y-1"><Label>Prompt and context</Label><Textarea bind:value={prompt} required rows={6} /></label>
			<label class="block space-y-1"><Label>Acceptance criteria, one per line</Label><Textarea bind:value={criteria} rows={3} /></label>
			<label class="block space-y-1"><Label>Test command argv</Label><Input bind:value={testArgv} placeholder='["go","test","./..."]' /></label>
			<label class="block space-y-1"><Label>Forbidden paths, one per line</Label><Textarea bind:value={forbiddenPaths} rows={2} /></label>
			<label class="block space-y-1"><Label>Allowed secret names, one per line</Label><Textarea bind:value={allowedSecrets} rows={2} /></label>
			<label class="block space-y-1">
				<Label>Maximum runtime in seconds</Label>
				<Input bind:value={maxRuntime} type="number" min="30" max={maxRuntimeCap || 86400} />
				<p class="text-[10px] text-[var(--color-text-tertiary)]">Capped to {maxRuntimeCap} s (machine max {machine.max_runtime_minutes} min, remaining lifetime {Math.round(machineRemainingSeconds / 60)} min)</p>
			</label>
			<div class="space-y-3"><label class="flex items-center justify-between gap-4"><span class="text-sm">Push working branch</span><Switch aria-label="Push working branch" checked={pushBranch} onCheckedChange={setPushBranch} /></label><label class="flex items-center justify-between gap-4"><span class="text-sm">Open pull request</span><Switch aria-label="Open pull request" bind:checked={openPullRequest} disabled={!pushBranch} /></label></div>
			<div class="flex justify-end gap-2 border-t border-[var(--app-border)] pt-4"><Button type="button" variant="outline" onclick={() => (open = false)}>Cancel</Button><Button type="submit" disabled={!submitEnabled}>{loading ? 'Queuing...' : 'Run agent'}</Button></div>
		</form>
	</Dialog.Content>
</Dialog.Root>
