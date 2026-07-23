<script lang="ts">
	import { onDestroy } from 'svelte';
	import { Check, LoaderCircle, RefreshCw, X } from 'lucide-svelte';
	import * as Dialog from '$lib/components/ui/dialog';
	import * as RadioGroup from '$lib/components/ui/radio-group';
	import * as Select from '$lib/components/ui/select';
	import { Alert, AlertDescription } from '$lib/components/ui/alert';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Switch } from '$lib/components/ui/switch';
	import {
		checkMachineName,
		createDevMachine,
		getDevMachinePolicy,
		getMachineNameSuggestion,
		listAgentProviders,
		listDevMachineEnvironments
	} from '$lib/api/dev-machines';
	import type {
		AgentProvider,
		CreateDevMachineInput,
		DevMachine,
		DevMachineEnvironment,
		DevMachinePolicy
	} from '$lib/types/dev-machine';
	import type { Issue } from '$lib/types/issue';
	import { appToast } from '$lib/features/toast/toast';

	const SIZE_DISK_GB: Record<string, number> = { small: 20, medium: 50, large: 100 };
	const sizes = [
		{ id: 'small', resources: '2 CPU / 4 GB', disk: '20 GB workspace' },
		{ id: 'medium', resources: '4 CPU / 8 GB', disk: '50 GB workspace' },
		{ id: 'large', resources: '8 CPU / 16 GB', disk: '100 GB workspace' }
	] as const;

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
	let size = $state<'small' | 'medium' | 'large'>('medium');
	let useAgent = $state(true);
	let provider = $state<AgentProvider['id']>('opencode');
	let browser = $state(true);
	let keepRunning = $state(false);
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
	const allRequiredSecretsFilled = $derived(
		(selectedProvider?.required_secrets ?? []).every((secret) => !!secretValues[secret]?.trim())
	);
	const sizeAllowed = $derived(!policy || SIZE_DISK_GB[size] <= policy.max_disk_gb);
	const canSubmit = $derived(
		!loading &&
		!loadingOptions &&
		policy?.enabled !== false &&
		nameStatus === 'available' &&
		sizeAllowed &&
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
		keepRunning = false;
		size = 'medium';
		try {
			const [suggestion, availableProviders, machinePolicy, availableEnvironments] = await Promise.all([
				getMachineNameSuggestion(currentSlug),
				listAgentProviders(currentSlug),
				getDevMachinePolicy(currentSlug),
				listDevMachineEnvironments(currentSlug)
			]);
			if (!isCurrentDialog(currentSlug, generation)) return;
			name = suggestion.name;
			nameStatus = 'available';
			providers = (availableProviders ?? []).map((item) => ({
				...item,
				required_secrets: item.required_secrets ?? [],
				supported_modes: item.supported_modes ?? []
			}));
			policy = machinePolicy;
			environments = (availableEnvironments ?? []).filter((item) => item.status === 'ready');
			environmentId = 'standard';
			if (policy && SIZE_DISK_GB[size] > policy.max_disk_gb) {
				size = (sizes.find((candidate) => SIZE_DISK_GB[candidate.id] <= policy!.max_disk_gb)?.id ?? 'small');
			}
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
		if (useAgent) {
			for (const requiredSecret of selectedProvider?.required_secrets ?? []) {
				const value = secretValues[requiredSecret];
				if (!value) continue;
				envVars.push({ name: requiredSecret, value, target_service: 'agent', provider, secret: true });
				envVars.push({ name: requiredSecret, value, target_service: 'ide', secret: true });
			}
		}
		if (useAgent && extraSecretName.trim() && extraSecretValue) {
			envVars.push({ name: extraSecretName.trim(), value: extraSecretValue, target_service: 'agent', provider, secret: true });
			envVars.push({ name: extraSecretName.trim(), value: extraSecretValue, target_service: 'ide', secret: true });
		}

		loading = true;
		try {
			const machine = await createDevMachine(slug, {
				name: name.trim(),
				issue_id: issue?.id,
				project_id: issue?.project_id ?? undefined,
				size,
				services: { ide: true, browser },
				agents: useAgent ? [{ provider, mode: 'autonomous', config: providerConfig }] : [],
				env_vars: envVars,
				environment_id: selectedEnvironment?.id,
				keep_running: keepRunning
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
				<Dialog.Title>New Dev Machine</Dialog.Title>
				<Dialog.Description>Create a reusable code-server and terminal environment. Repositories are attached from issues.</Dialog.Description>
			</Dialog.Header>

			{#if policy?.enabled === false}
				<Alert variant="destructive"><AlertDescription>Dev Machines are disabled by workspace policy.</AlertDescription></Alert>
			{/if}

			<div class="space-y-1.5">
				<Label for="machine-name">Machine name</Label>
				<div class="flex gap-2">
					<div class="relative flex-1">
						<Input id="machine-name" bind:value={name} autocomplete="off" required class="pr-8" />
						<span class="absolute right-2 top-1/2 -translate-y-1/2">
							{#if nameStatus === 'checking'}<LoaderCircle class="size-4 animate-spin text-muted-foreground" />
							{:else if nameStatus === 'available'}<Check class="size-4 text-emerald-500" />
							{:else if nameStatus === 'unavailable'}<X class="size-4 text-destructive" />{/if}
						</span>
					</div>
					<Button type="button" variant="outline" size="icon" onclick={regenerateName} aria-label="Generate another name"><RefreshCw /></Button>
				</div>
				<p class="text-xs text-muted-foreground">
					{#if nameStatus === 'available'}Name is available{:else if nameStatus === 'checking'}Checking availability...{:else if nameStatus === 'unavailable'}Use a unique lowercase name with letters, numbers, and hyphens{/if}
				</p>
			</div>

			<div class="space-y-1.5">
				<Label>Development environment</Label>
				<Select.Root type="single" value={environmentId} onValueChange={(value) => value && (environmentId = value)}>
					<Select.Trigger class="w-full">{selectedEnvironment?.name ?? 'Use configured default environment'}</Select.Trigger>
					<Select.Content>
						<Select.Item value="standard" label="Use configured default environment">Use configured default environment</Select.Item>
						{#each environments as environment}<Select.Item value={environment.id} label={environment.name}>{environment.name}</Select.Item>{/each}
					</Select.Content>
				</Select.Root>
			</div>

			<div class="space-y-2">
				<Label>Machine size</Label>
				<RadioGroup.Root bind:value={size} class="grid grid-cols-1 gap-2 sm:grid-cols-3">
					{#each sizes as option}
						<label class="relative cursor-pointer">
							<RadioGroup.Item value={option.id} disabled={!!policy && SIZE_DISK_GB[option.id] > policy.max_disk_gb} class="peer sr-only" />
							<div data-selected={size === option.id} class="rounded-lg border border-border bg-background p-3 transition data-[selected=true]:border-primary data-[selected=true]:bg-primary/10 peer-focus-visible:ring-2 peer-focus-visible:ring-ring peer-disabled:cursor-not-allowed peer-disabled:opacity-50">
								<span class="block text-sm font-semibold capitalize">{option.id}</span>
								<span class="mt-1 block text-xs text-muted-foreground">{option.resources}</span>
								<span class="block text-xs text-muted-foreground">{option.disk}</span>
							</div>
						</label>
					{/each}
				</RadioGroup.Root>
			</div>

			<div class="space-y-3 rounded-lg border border-border p-4">
				<div class="flex items-center justify-between gap-4"><div><Label>Browser</Label><p class="text-xs text-muted-foreground">Include the isolated browser service.</p></div><Switch aria-label="Browser" bind:checked={browser} /></div>
				<div class="flex items-center justify-between gap-4"><div><Label>Keep running</Label><p class="text-xs text-muted-foreground">Skip the {policy?.idle_pause_minutes ?? 240}-minute inactivity pause.</p></div><Switch aria-label="Keep running" bind:checked={keepRunning} /></div>
			</div>

			<div class="space-y-3 rounded-lg border border-border p-4">
				<div class="flex items-center justify-between gap-4"><div><Label>Agent runtime</Label><p class="text-xs text-muted-foreground">Configure an autonomous coding provider.</p></div><Switch aria-label="Agent runtime" bind:checked={useAgent} /></div>
				{#if useAgent}
					<div class="space-y-1.5">
						<Label>Provider</Label>
						<Select.Root type="single" value={provider} onValueChange={(value) => value && selectProvider(value as AgentProvider['id'])}>
							<Select.Trigger class="w-full">{selectedProvider?.display_name ?? 'Select provider'}</Select.Trigger>
							<Select.Content>{#each providers as item}<Select.Item value={item.id} label={item.display_name}>{item.display_name}</Select.Item>{/each}</Select.Content>
						</Select.Root>
					</div>
					{#if selectedProvider?.custom}
						<div class="grid gap-3 sm:grid-cols-2">
							<label class="space-y-1.5 sm:col-span-2"><Label>Custom image</Label><Input bind:value={customImage} required /></label>
							<label class="space-y-1.5"><Label>Entrypoint</Label><Input bind:value={customEntrypoint} placeholder="/usr/local/bin/agent" required /></label>
							<label class="space-y-1.5"><Label>Arguments as JSON argv</Label><Input bind:value={customArgs} /></label>
						</div>
					{/if}
					{#each selectedProvider?.required_secrets ?? [] as secret}
						<label class="block space-y-1.5"><Label>{secret}</Label><Input type="password" value={secretValues[secret] ?? ''} oninput={(event) => (secretValues[secret] = event.currentTarget.value)} autocomplete="off" required /></label>
					{/each}
					<div class="grid gap-3 sm:grid-cols-2">
						<label class="space-y-1.5"><Label>Additional secret name</Label><Input bind:value={extraSecretName} placeholder="Optional" /></label>
						<label class="space-y-1.5"><Label>Additional secret value</Label><Input bind:value={extraSecretValue} type="password" autocomplete="off" /></label>
					</div>
				{/if}
			</div>

			<Dialog.Footer>
				<Button type="button" variant="outline" onclick={() => (open = false)}>Cancel</Button>
				<Button type="submit" disabled={!canSubmit}>{loading ? 'Queuing...' : 'Create machine'}</Button>
			</Dialog.Footer>
		</form>
	</Dialog.Content>
</Dialog.Root>
