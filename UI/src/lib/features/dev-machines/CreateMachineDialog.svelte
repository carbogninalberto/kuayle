<script lang="ts">
	import { onDestroy } from 'svelte';
	import { Check, LoaderCircle, RefreshCw, X } from '@lucide/svelte';
	import * as Dialog from '$lib/components/ui/dialog';
	import * as Select from '$lib/components/ui/select';
	import { Alert, AlertDescription } from '$lib/components/ui/alert';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import * as RadioGroup from '$lib/components/ui/radio-group';
	import { Switch } from '$lib/components/ui/switch';
	import MachineResourceSliders from '$lib/features/dev-machines/MachineResourceSliders.svelte';
	import {
		checkMachineName,
		createDevMachine,
		getDevMachinePolicy,
		getDevMachineRepositoryDefaults,
		getMachineNameSuggestion,
		listAgentProviders,
		listDevMachineEnvironments
	} from '$lib/api/dev-machines';
	import type {
		AgentProvider,
		CreateDevMachineInput,
		DevMachine,
		DevMachineEnvironment,
		DevMachinePolicy,
		DevMachineRepositoryDefaults,
		DevelopmentRepository
	} from '$lib/types/dev-machine';
	import { DEFAULT_REPOSITORY_CHOICE, repositoryChoiceBlocked, repositoryCreateInput } from '$lib/features/dev-machines/repository-choice';
	import { repositorySourceLabel } from '$lib/features/dev-machines/repository-source';
	import {
		DEFAULT_MACHINE_RESOURCES,
		MACHINE_DISK_GB,
		normalizeMachineResources,
		resolveMachineSizeOptions
	} from '$lib/features/dev-machines/machine-size';
	import type { Issue } from '$lib/types/issue';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/paraglide/messages.js';
	import { getLocale, setLocale } from '$lib/paraglide/runtime.js';

	let {
		open = $bindable(false),
		slug,
		issue,
		oncreated
	}: { open: boolean; slug: string; issue?: Issue; oncreated?: (machine: DevMachine) => void } = $props();

	let providers = $state<AgentProvider[]>([]);
	let environments = $state<DevMachineEnvironment[]>([]);
	let policy = $state<DevMachinePolicy | null>(null);
	let name = $state('');
	let nameStatus = $state<'idle' | 'checking' | 'available' | 'unavailable'>('idle');
	let environmentId = $state('standard');
	let repositoryDefaults = $state<DevMachineRepositoryDefaults | null>(null);
	let repositoryChoice = $state(DEFAULT_REPOSITORY_CHOICE);
	let cpuMillis = $state(DEFAULT_MACHINE_RESOURCES.cpu_millis);
	let memoryMb = $state(DEFAULT_MACHINE_RESOURCES.memory_mb);
	let diskGb = $state(DEFAULT_MACHINE_RESOURCES.disk_gb);
	let useAgent = $state(true);
	let provider = $state<AgentProvider['id']>('opencode');
	let profile = $state<'standard' | 'headless'>('standard');
	let browser = $state(true);
	let secretValues = $state<Record<string, string>>({});
	let extraSecretName = $state('');
	let extraSecretValue = $state('');
	let customImage = $state('');
	let customEntrypoint = $state('');
	let customArgs = $state('[]');
	let loading = $state(false);
	let loadingOptions = $state(false);
	let checkTimer: ReturnType<typeof setTimeout> | undefined;
	let checkSequence = 0;
	let dialogGeneration = 0;

	const selectedProvider = $derived(providers.find((item) => item.id === provider));
	const selectedEnvironment = $derived(environments.find((item) => item.id === environmentId));
	const headless = $derived(profile === 'headless');
	const defaultRepository = $derived(repositoryDefaults?.repository ?? null);
	const repositoryBlocked = $derived(repositoryChoiceBlocked(repositoryChoice, repositoryDefaults));
	const sizeOptions = $derived(
		resolveMachineSizeOptions(policy?.size_options, policy?.max_disk_gb ?? MACHINE_DISK_GB[MACHINE_DISK_GB.length - 1])
	);
	const resources = $derived(normalizeMachineResources({ cpu_millis: cpuMillis, memory_mb: memoryMb, disk_gb: diskGb }, sizeOptions));
	const allRequiredSecretsFilled = $derived(
		(selectedProvider?.required_secrets ?? []).every((secret) => !!secretValues[secret]?.trim())
	);
	const canSubmit = $derived(
		!loading &&
		!loadingOptions &&
		policy?.enabled !== false &&
		!repositoryBlocked &&
		nameStatus === 'available' &&
		sizeOptions.disk_gb.length > 0 &&
		(!useAgent || (!!selectedProvider && allRequiredSecretsFilled)) &&
		(!useAgent || !selectedProvider?.custom || (!!customImage.trim() && !!customEntrypoint.trim()))
	);

	$effect(() => {
		const currentOpen = open;
		const currentSlug = slug;
		const generation = ++dialogGeneration;
		cancelNameCheck();
		name = '';
		nameStatus = 'idle';
		loadingOptions = currentOpen;
		if (!currentOpen) return;
		void resetAndLoad(currentSlug, generation);
		return () => {
			if (dialogGeneration === generation) dialogGeneration++;
			cancelNameCheck();
		};
	});

	$effect(() => {
		const currentOpen = open;
		const currentSlug = slug;
		const generation = dialogGeneration;
		const candidate = name.trim();
		cancelNameCheck();
		const sequence = ++checkSequence;
		if (!currentOpen) {
			nameStatus = 'idle';
			return;
		}
		if (!/^[a-z][a-z0-9-]{1,253}[a-z0-9]$/.test(candidate)) {
			nameStatus = candidate ? 'unavailable' : 'idle';
			return;
		}
		nameStatus = 'checking';
		const timer = setTimeout(async () => {
			try {
				const result = await checkMachineName(currentSlug, candidate);
				if (isCurrentDialog(currentSlug, generation) && sequence === checkSequence && candidate === name.trim()) {
					nameStatus = result.available ? 'available' : 'unavailable';
				}
			} catch {
				if (isCurrentDialog(currentSlug, generation) && sequence === checkSequence) nameStatus = 'unavailable';
			}
		}, 300);
		checkTimer = timer;
		return () => {
			if (checkTimer === timer) {
				clearTimeout(timer);
				checkTimer = undefined;
			}
			if (checkSequence === sequence) checkSequence++;
		};
	});

	onDestroy(() => {
		dialogGeneration++;
		cancelNameCheck();
	});

	function isCurrentDialog(currentSlug: string, generation: number) {
		return open && slug === currentSlug && dialogGeneration === generation;
	}

	function cancelNameCheck() {
		checkSequence++;
		if (checkTimer) clearTimeout(checkTimer);
		checkTimer = undefined;
	}

	async function resetAndLoad(currentSlug: string, generation: number) {
		loadingOptions = true;
		providers = [];
		environments = [];
		policy = null;
		secretValues = {};
		extraSecretName = '';
		extraSecretValue = '';
		profile = 'standard';
		browser = true;
		repositoryDefaults = null;
		repositoryChoice = DEFAULT_REPOSITORY_CHOICE;
		cpuMillis = DEFAULT_MACHINE_RESOURCES.cpu_millis;
		memoryMb = DEFAULT_MACHINE_RESOURCES.memory_mb;
		diskGb = DEFAULT_MACHINE_RESOURCES.disk_gb;
		try {
			const [suggestion, availableProviders, machinePolicy, availableEnvironments, defaults] = await Promise.all([
				getMachineNameSuggestion(currentSlug),
				listAgentProviders(currentSlug),
				getDevMachinePolicy(currentSlug),
				listDevMachineEnvironments(currentSlug),
				// Optional: without it the server still applies the configured repository.
				getDevMachineRepositoryDefaults(currentSlug, { issueId: issue?.id }).catch(() => null)
			]);
			if (!isCurrentDialog(currentSlug, generation)) return;
			name = suggestion.name;
			nameStatus = 'available';
			providers = (availableProviders ?? []).map((item) => ({
				...item,
				required_secrets: item.required_secrets ?? [],
				optional_secrets: item.optional_secrets ?? [],
				supported_modes: item.supported_modes ?? []
			}));
			policy = machinePolicy;
			environments = (availableEnvironments ?? []).filter((item) => item.status === 'ready');
			environmentId = 'standard';
			repositoryDefaults = defaults;
			if (providers.length > 0 && !providers.some((item) => item.id === provider)) provider = providers[0].id;
			if (providers.length === 0) useAgent = false;
		} catch (error) {
			if (!isCurrentDialog(currentSlug, generation)) return;
			appToast.apiError(error, 'Failed to load Dev Machine options');
		} finally {
			if (isCurrentDialog(currentSlug, generation)) loadingOptions = false;
		}
	}

	async function regenerateName() {
		const currentSlug = slug;
		const generation = dialogGeneration;
		cancelNameCheck();
		try {
			nameStatus = 'checking';
			const suggestion = await getMachineNameSuggestion(currentSlug);
			if (!isCurrentDialog(currentSlug, generation)) return;
			name = suggestion.name;
			nameStatus = 'available';
		} catch (error) {
			if (!isCurrentDialog(currentSlug, generation)) return;
			nameStatus = 'unavailable';
			appToast.apiError(error, 'Failed to generate a machine name');
		}
	}

	function defaultRepositoryLabel(repository: DevelopmentRepository | null) {
		if (!repository) return m['machines.no_default_repository']();
		return m['machines.repository_from_settings']({ repository: repository.full_name, source: repositorySourceLabel(repository.source) });
	}

	function selectProvider(value: AgentProvider['id']) {
		provider = value;
		const selected = providers.find((item) => item.id === value);
		if (selected?.custom && !customImage) customImage = selected.default_image;
	}

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		if (!canSubmit) return;
		let providerConfig: Record<string, unknown> | undefined;
		if (useAgent && selectedProvider?.custom) {
			let args: string[];
			try {
				args = JSON.parse(customArgs);
				if (!Array.isArray(args) || args.some((part) => typeof part !== 'string')) throw new Error();
			} catch {
				appToast.error('Custom provider arguments must be a JSON argv array');
				return;
			}
			providerConfig = {
				image: customImage.trim(),
				entrypoint: customEntrypoint.trim(),
				args,
				required_secrets: extraSecretName.trim() ? [extraSecretName.trim()] : []
			};
		}

		const envVars: CreateDevMachineInput['env_vars'] = [];
		const developerSecretTarget = headless ? 'terminal' : 'ide';
		if (useAgent) {
			for (const requiredSecret of [...(selectedProvider?.required_secrets ?? []), ...(selectedProvider?.optional_secrets ?? [])]) {
				const value = secretValues[requiredSecret]?.trim();
				if (!value) continue;
				envVars.push({ name: requiredSecret, value, target_service: 'agent', provider, secret: true });
				envVars.push({ name: requiredSecret, value, target_service: developerSecretTarget, secret: true });
			}
		}
		if (useAgent && extraSecretName.trim() && extraSecretValue) {
			envVars.push({ name: extraSecretName.trim(), value: extraSecretValue, target_service: 'agent', provider, secret: true });
			envVars.push({ name: extraSecretName.trim(), value: extraSecretValue, target_service: developerSecretTarget, secret: true });
		}

		loading = true;
		try {
			const machine = await createDevMachine(slug, {
				name: name.trim(),
				issue_id: issue?.id,
				project_id: issue?.project_id ?? undefined,
				size: 'custom',
				cpu_millis: resources.cpu_millis,
				memory_mb: resources.memory_mb,
				disk_gb: resources.disk_gb,
				profile,
				services: { ide: !headless, browser: headless ? false : browser },
				agents: useAgent ? [{ provider, mode: 'autonomous', config: providerConfig }] : [],
				env_vars: envVars,
				environment_id: selectedEnvironment?.id,
				repo: repositoryCreateInput(repositoryChoice, repositoryDefaults)
			});
			appToast.success('Dev Machine queued');
			open = false;
			oncreated?.(machine);
		} catch (error) {
			appToast.apiError(error, 'Failed to create Dev Machine');
		} finally {
			loading = false;
		}
	}
</script>

<Dialog.Root bind:open>
	<Dialog.Content class="max-h-[90vh] overflow-y-auto sm:max-w-2xl">
		<form onsubmit={submit} class="space-y-5">
			<Dialog.Header>
				<Dialog.Title>{m['machines.create_title']()}</Dialog.Title>
				<Dialog.Description>{m['machines.create_desc']()}</Dialog.Description>
			</Dialog.Header>

			{#if policy?.enabled === false}
				<Alert variant="destructive"><AlertDescription>{m['machines.disabled_policy']()}</AlertDescription></Alert>
			{/if}

			<div class="space-y-1.5">
				<Label for="machine-name">{m['machines.machine_name']()}</Label>
				<div class="flex gap-2">
					<div class="relative flex-1">
						<Input id="machine-name" bind:value={name} autocomplete="off" required class="pr-8" />
						<span class="absolute right-2 top-1/2 -translate-y-1/2">
							{#if nameStatus === 'checking'}<LoaderCircle class="size-4 animate-spin text-muted-foreground" />
							{:else if nameStatus === 'available'}<Check class="size-4 text-emerald-500" />
							{:else if nameStatus === 'unavailable'}<X class="size-4 text-destructive" />{/if}
						</span>
					</div>
					<Button type="button" variant="outline" size="icon" onclick={regenerateName} aria-label={m['machines.generate_name']()}><RefreshCw /></Button>
				</div>
				<p class="text-xs text-muted-foreground">
					{#if nameStatus === 'available'}{m['machines.name_available']()}{:else if nameStatus === 'checking'}{m['machines.checking_availability']()}{:else if nameStatus === 'unavailable'}{m['machines.name_unavailable']()}{/if}
				</p>
			</div>

			<div class="space-y-2">
				<Label>{m['machines.profile']()}</Label>
				<RadioGroup.Root bind:value={profile} class="grid gap-3 sm:grid-cols-2" aria-label={m['machines.profile']()}>
					<label class="flex cursor-pointer items-start gap-3 rounded-lg border p-3 {profile === 'standard' ? 'border-[var(--app-accent)]' : 'border-[var(--app-border)]'}">
						<RadioGroup.Item value="standard" aria-label={m['machines.profile_standard']()} class="mt-0.5" />
						<span>
							<span class="block text-sm font-medium">{m['machines.profile_standard']()}</span>
							<span class="mt-1 block text-xs text-muted-foreground">{m['machines.profile_standard_desc']()}</span>
						</span>
					</label>
					<label class="flex cursor-pointer items-start gap-3 rounded-lg border p-3 {profile === 'headless' ? 'border-[var(--app-accent)]' : 'border-[var(--app-border)]'}">
						<RadioGroup.Item value="headless" aria-label={m['machines.profile_headless']()} class="mt-0.5" />
						<span>
							<span class="block text-sm font-medium">{m['machines.profile_headless']()}</span>
							<span class="mt-1 block text-xs text-muted-foreground">{m['machines.profile_headless_desc']()}</span>
						</span>
					</label>
				</RadioGroup.Root>
			</div>

			<div class="space-y-1.5">
				<Label>{m['machines.development_environment']()}</Label>
				<Select.Root type="single" value={environmentId} onValueChange={(value) => value && (environmentId = value)}>
					<Select.Trigger class="w-full">{selectedEnvironment?.name ?? m['machines.use_default_environment']()}</Select.Trigger>
					<Select.Content>
						<Select.Item value="standard" label={m['machines.use_default_environment']()}>{m['machines.use_default_environment']()}</Select.Item>
						{#each environments as environment}<Select.Item value={environment.id} label={environment.name}>{environment.name}</Select.Item>{/each}
					</Select.Content>
				</Select.Root>
			</div>

			<div class="space-y-1.5">
				<Label>{m['machines.repository']()}</Label>
				<Select.Root type="single" value={repositoryChoice} disabled={loadingOptions} onValueChange={(value) => value && (repositoryChoice = value)}>
					<Select.Trigger class="w-full min-w-0" aria-label={m['machines.repository']()}><span class="truncate">{repositoryChoice === DEFAULT_REPOSITORY_CHOICE ? defaultRepositoryLabel(defaultRepository) : (repositoryDefaults?.repositories.find((item) => item.id === repositoryChoice)?.full_name ?? defaultRepositoryLabel(defaultRepository))}</span></Select.Trigger>
					<Select.Content>
						<Select.Item value={DEFAULT_REPOSITORY_CHOICE} label={defaultRepositoryLabel(defaultRepository)}>{defaultRepositoryLabel(defaultRepository)}</Select.Item>
						{#each repositoryDefaults?.repositories ?? [] as repository (repository.id)}
							{#if repository.id !== defaultRepository?.id}<Select.Item value={repository.id} label={repository.full_name}>{repository.full_name}</Select.Item>{/if}
						{/each}
					</Select.Content>
				</Select.Root>
				{#if repositoryBlocked && defaultRepository}
					<p class="text-xs text-destructive">{m['machines.repository_not_allowed']({ repository: defaultRepository.full_name })}</p>
				{:else if defaultRepository}
					<p class="text-xs text-muted-foreground">{m['machines.repository_default_help']()}</p>
				{/if}
			</div>

			<MachineResourceSliders bind:cpu_millis={cpuMillis} bind:memory_mb={memoryMb} bind:disk_gb={diskGb} {sizeOptions} />

			{#if !headless}
				<div class="space-y-3 rounded-lg border border-border p-4">
					<div class="flex items-center justify-between gap-4"><div><Label>{m['machines.browser']()}</Label><p class="text-xs text-muted-foreground">{m['machines.browser_desc']()}</p></div><Switch aria-label={m['machines.browser']()} bind:checked={browser} /></div>
				</div>
			{/if}

			<div class="space-y-3 rounded-lg border border-border p-4">
				<div class="flex items-center justify-between gap-4"><div><Label>{m['machines.agent_runtime']()}</Label><p class="text-xs text-muted-foreground">{m['machines.agent_runtime_desc']()}</p></div><Switch aria-label={m['machines.agent_runtime']()} bind:checked={useAgent} /></div>
				{#if useAgent}
					<div class="space-y-1.5">
						<Label>{m['machines.provider']()}</Label>
						<Select.Root type="single" value={provider} onValueChange={(value) => value && selectProvider(value as AgentProvider['id'])}>
							<Select.Trigger class="w-full" aria-label={m['machines.provider']()}>{selectedProvider?.display_name ?? m['machines.select_provider']()}</Select.Trigger>
							<Select.Content>{#each providers as item}<Select.Item value={item.id} label={item.display_name}>{item.display_name}</Select.Item>{/each}</Select.Content>
						</Select.Root>
					</div>
					{#if selectedProvider?.custom}
						<div class="grid gap-3 sm:grid-cols-2">
							<label class="space-y-1.5 sm:col-span-2"><Label>{m['machines.custom_image']()}</Label><Input bind:value={customImage} required /></label>
							<label class="space-y-1.5"><Label>{m['machines.entrypoint']()}</Label><Input bind:value={customEntrypoint} placeholder="/usr/local/bin/agent" required /></label>
							<label class="space-y-1.5"><Label>{m['machines.arguments_json']()}</Label><Input bind:value={customArgs} /></label>
						</div>
					{/if}
					{#each selectedProvider?.required_secrets ?? [] as secret}
						<label class="block space-y-1.5"><Label>{secret}</Label><Input type="password" value={secretValues[secret] ?? ''} oninput={(event) => (secretValues[secret] = event.currentTarget.value)} autocomplete="off" required /></label>
					{/each}
					{#each selectedProvider?.optional_secrets ?? [] as secret}
						<label class="block space-y-1.5">
							<Label>{m['machines.optional_secret']({ name: secret })}</Label>
							<Input type="password" value={secretValues[secret] ?? ''} oninput={(event) => (secretValues[secret] = event.currentTarget.value)} autocomplete="off" />
							<span class="block text-xs text-[var(--color-text-tertiary)]">{m['machines.optional_secret_hint']()}</span>
						</label>
					{/each}
					<div class="grid gap-3 sm:grid-cols-2">
						<label class="space-y-1.5"><Label>{m['machines.additional_secret_name']()}</Label><Input bind:value={extraSecretName} placeholder="Optional" /></label>
						<label class="space-y-1.5"><Label>{m['machines.additional_secret_value']()}</Label><Input bind:value={extraSecretValue} type="password" autocomplete="off" /></label>
					</div>
				{/if}
			</div>

			<Dialog.Footer>
				<Button type="button" variant="outline" onclick={() => (open = false)}>{m['common.cancel']()}</Button>
				<Button type="submit" disabled={!canSubmit}>{loading ? m['machines.queuing']() : m['machines.create_machine']()}</Button>
			</Dialog.Footer>
		</form>
	</Dialog.Content>
</Dialog.Root>
