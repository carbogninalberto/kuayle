<script lang="ts">
	import * as Dialog from '$lib/components/ui/dialog';
	import * as Select from '$lib/components/ui/select';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import SelectField from '$lib/components/shared/SelectField.svelte';
	import { Switch } from '$lib/components/ui/switch';
	import { Textarea } from '$lib/components/ui/textarea';
	import { createAgentRun, listMachineAgentProviders, modelServers } from '$lib/api/dev-machines';
	import { preferredServer, serversFor } from '$lib/features/agents/model-choice';
	import type {
		AgentProvider,
		AgentRun,
		CreateAgentRunInput,
		ThinkingLevel,
		DevMachine,
		ModelServer
	} from '$lib/types/dev-machine';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/paraglide/messages.js';
	import { getLocale, setLocale } from '$lib/paraglide/runtime.js';

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
	let servers = $state<ModelServer[]>([]);
	let serverId = $state('');
	let modelOverride = $state('');
	let thinkingOverride = $state<ThinkingLevel | ''>('');

	const selectedProvider = $derived(providers.find((item) => item.id === provider));
	const selectedProviderLabel = $derived(selectedProvider?.display_name ?? (providers.length === 0 ? 'Loading providers...' : 'Select provider'));
	// Codex and Claude Code can use a compatible model server; without one they
	// rely on their own sign-in.
	const compatibleServers = $derived(serversFor(servers, provider));
	const selectedServer = $derived(compatibleServers.find((server) => server.id === serverId));
	const submitEnabled = $derived(providersLoaded && !providersError && !loading && !!prompt.trim() && !!selectedProvider && maxRuntime >= 30 && maxRuntime <= maxRuntimeCap);

	$effect(() => {
		if (!open) return;
		prompt = initialPrompt;
		machineRemainingSeconds = Math.max(0, Math.floor((new Date(machine.expires_at).getTime() - Date.now()) / 1000));
		maxRuntimeCap = Math.min(machine.max_runtime_minutes * 60, machineRemainingSeconds, 86400);
		maxRuntime = maxRuntimeCap >= 30 ? Math.min(3600, maxRuntimeCap) : 0;
		pushBranch = !!checkoutId || !!machine.repository_affinity_id;
		openPullRequest = false;
		modelOverride = '';
		thinkingOverride = '';
		void loadProviders();
		void loadServers();
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
				allowedSecrets = [...configured.required_secrets, ...(configured.optional_secrets ?? [])].join('\n');
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

	async function loadServers() {
		try {
			servers = await modelServers.list(slug);
		} catch {
			servers = [];
		}
		serverId = preferredServer(servers, provider)?.id ?? '';
	}

	function selectProvider(value: AgentProvider['id']) {
		provider = value;
		const p = providers.find((item) => item.id === value);
		allowedSecrets = [...(p?.required_secrets ?? []), ...(p?.optional_secrets ?? [])].join('\n');
		serverId = preferredServer(servers, value)?.id ?? '';
		modelOverride = '';
		thinkingOverride = '';
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
			const input: CreateAgentRunInput = {
				checkout_id: checkoutId, use_root_workspace: !checkoutId && !machine.repository_affinity_id,
				provider, mode, prompt, acceptance_criteria: criteria.split('\n').map((line) => line.trim()).filter(Boolean),
				allowed_commands: [], forbidden_paths: forbiddenPaths.split('\n').map((line) => line.trim()).filter(Boolean),
				test_command: parsedTest, max_runtime_seconds: maxRuntime,
				allowed_secrets: allowedSecrets.split('\n').map((line) => line.trim()).filter(Boolean), push_branch: pushBranch, open_pull_request: openPullRequest
			};
			if (selectedServer) {
				input.model_server_id = selectedServer.id;
				if (modelOverride) input.model = modelOverride;
				if (thinkingOverride) input.thinking = thinkingOverride;
			}
			const run = await createAgentRun(slug, machine.id, input);
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
			<Dialog.Header><Dialog.Title>{m['machines.run_agent_title']()}</Dialog.Title><Dialog.Description>{machine.repo_owner && machine.repo_name ? m['machines.run_agent_desc']({ repo: `${machine.repo_owner}/${machine.repo_name}`, branch: machine.working_branch ?? '' }) : m['machines.run_agent_no_checkout']()}</Dialog.Description></Dialog.Header>
			{#if providersError}
				<div class="flex items-center justify-between gap-3 text-xs text-red-400"><span>{m['machines.no_autonomous_provider']()}</span><Button type="button" size="xs" variant="outline" onclick={loadProviders}>{m['machines.refresh']()}</Button></div>
			{/if}
			<div class="grid gap-3 sm:grid-cols-2">
				<div class="space-y-1"><Label>{m['machines.provider']()}</Label><Select.Root type="single" value={provider} disabled={!providersLoaded || providersError} onValueChange={(value) => value && selectProvider(value as AgentProvider['id'])}><Select.Trigger aria-label={m['machines.provider']()} class="w-full">{selectedProviderLabel}</Select.Trigger><Select.Content>{#each providers as item}<Select.Item value={item.id} label={item.display_name}>{item.display_name}</Select.Item>{/each}</Select.Content></Select.Root></div>
				<div class="space-y-1"><Label>{m['machines.mode']()}</Label><div class="flex h-9 items-center rounded-md border border-[var(--app-border)] px-3 text-sm">{m['machines.autonomous']()}</div></div>
			</div>
			{#if compatibleServers.length}
				<div class="grid gap-3 rounded-lg border border-[var(--app-border)] p-3 sm:grid-cols-3">
					<div class="space-y-1">
						<Label>{m['tasks.model_server']()}</Label>
						<SelectField
							bind:value={serverId}
							options={compatibleServers.map((server) => ({ value: server.id, label: server.name }))}
							aria-label={m['tasks.model_server']()}
							class="w-full"
						/>
					</div>
					<div class="space-y-1">
						<Label>{m['tasks.model']()}</Label>
						<SelectField
							bind:value={modelOverride}
							options={[
								{ value: '', label: m['machines.server_default']({ model: selectedServer?.default_model ?? '' }) },
								...(selectedServer?.models ?? []).map((model) => ({ value: model, label: model }))
							]}
							aria-label={m['tasks.model']()}
							class="w-full"
						/>
					</div>
					<div class="space-y-1">
						<Label>{m['tasks.effort']()}</Label>
						<SelectField
							bind:value={thinkingOverride}
							options={[
								{ value: '', label: m['machines.workspace_default']() },
								...(selectedServer?.thinking_levels ?? []).map((level) => ({
									value: level,
									label: m[`settings.agents.thinking_${level}`]()
								}))
							]}
							aria-label={m['tasks.effort']()}
							class="w-full"
						/>
					</div>
				</div>
			{:else if provider === 'codex' || provider === 'claude-code'}
				<p class="text-xs text-[var(--color-text-tertiary)]">{m['tasks.own_sign_in']()}</p>
			{/if}
			<label class="block space-y-1"><Label>{m['machines.prompt_context']()}</Label><Textarea bind:value={prompt} required rows={6} /></label>
			<label class="block space-y-1"><Label>{m['machines.acceptance_criteria']()}</Label><Textarea bind:value={criteria} rows={3} /></label>
			<label class="block space-y-1"><Label>{m['machines.test_command']()}</Label><Input bind:value={testArgv} placeholder='["go","test","./..."]' /></label>
			<label class="block space-y-1"><Label>{m['machines.forbidden_paths']()}</Label><Textarea bind:value={forbiddenPaths} rows={2} /></label>
			<label class="block space-y-1"><Label>{m['machines.allowed_secrets']()}</Label><Textarea bind:value={allowedSecrets} rows={2} /></label>
			<label class="block space-y-1">
				<Label>{m['machines.max_runtime']()}</Label>
				<Input bind:value={maxRuntime} type="number" min="30" max={maxRuntimeCap || 86400} />
				<p class="text-[10px] text-[var(--color-text-tertiary)]">{m['machines.max_runtime_capped']({ max: maxRuntimeCap, machineMax: machine.max_runtime_minutes, remaining: Math.round(machineRemainingSeconds / 60) })}</p>
			</label>
			<div class="space-y-3"><label class="flex items-center justify-between gap-4"><span class="text-sm">{m['machines.push_branch']()}</span><Switch aria-label={m['machines.push_branch']()} checked={pushBranch} onCheckedChange={setPushBranch} /></label><label class="flex items-center justify-between gap-4"><span class="text-sm">{m['machines.open_pr']()}</span><Switch aria-label={m['machines.open_pr']()} bind:checked={openPullRequest} disabled={!pushBranch} /></label></div>
			<div class="flex justify-end gap-2 border-t border-[var(--app-border)] pt-4"><Button type="button" variant="outline" onclick={() => (open = false)}>{m['common.cancel']()}</Button><Button type="submit" disabled={!submitEnabled}>{loading ? m['machines.queuing']() : m['machines.run_agent_action']()}</Button></div>
		</form>
	</Dialog.Content>
</Dialog.Root>
