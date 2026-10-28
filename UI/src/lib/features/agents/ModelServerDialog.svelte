<script lang="ts">
	import { untrack } from 'svelte';
	import { LoaderCircle, RefreshCw } from '@lucide/svelte';
	import * as Dialog from '$lib/components/ui/dialog/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import { Switch } from '$lib/components/ui/switch/index.js';
	import SelectField from '$lib/components/shared/SelectField.svelte';
	import { modelServers } from '$lib/api/dev-machines';
	import type { ModelServer, ModelServerInput, ThinkingLevel } from '$lib/types/dev-machine';
	import { appToast, getErrorMessage } from '$lib/features/toast/toast';
	import { m } from '$lib/paraglide/messages.js';
	import AgentLogo from './AgentLogo.svelte';

	let {
		slug,
		server,
		onsaved,
		onclose
	}: {
		slug: string;
		server?: ModelServer;
		onsaved: (server: ModelServer) => void;
		onclose: () => void;
	} = $props();

	// Presets only fill in public endpoints; the key is always the admin's own.
	const presets: { name: string; openai?: string; anthropic?: string }[] = [
		{ name: 'DeepSeek', openai: 'https://api.deepseek.com', anthropic: 'https://api.deepseek.com/anthropic' },
		{ name: 'OpenAI', openai: 'https://api.openai.com/v1' },
		{ name: 'Anthropic', anthropic: 'https://api.anthropic.com' },
		{ name: 'OpenRouter', openai: 'https://openrouter.ai/api/v1' }
	];
	const levels: ThinkingLevel[] = ['off', 'low', 'high', 'max'];

	// The form starts from the server being edited; later prop changes do not reset it.
	const initial = untrack(() => server);
	let name = $state(initial?.name ?? ''),
		openai = $state(initial?.openai_base_url ?? ''),
		anthropic = $state(initial?.anthropic_base_url ?? ''),
		apiKey = $state(''),
		models = $state<string[]>(initial?.models ?? []),
		defaultModel = $state(initial?.default_model ?? ''),
		fastModel = $state(initial?.fast_model ?? ''),
		thinking = $state<ThinkingLevel>(initial?.thinking ?? 'high'),
		isDefault = $state(initial?.is_default ?? false),
		fetching = $state(false),
		fetchError = $state(''),
		saving = $state(false);

	const hasURL = $derived(openai.trim() !== '' || anthropic.trim() !== '');
	// A stored key is reused only for the URLs it was saved with.
	const canFetch = $derived(
		hasURL &&
			(apiKey !== '' ||
				(!!server &&
					openai.trim() === (server.openai_base_url ?? '') &&
					anthropic.trim() === (server.anthropic_base_url ?? '')))
	);
	const canSave = $derived(name.trim() !== '' && hasURL && defaultModel.trim() !== '' && (!!server || apiKey !== ''));
	const modelOptions = $derived(models.map((model) => ({ value: model, label: model })));

	function preset(choice: (typeof presets)[number]) {
		if (!name.trim()) name = choice.name;
		openai = choice.openai ?? '';
		anthropic = choice.anthropic ?? '';
		models = [];
		fetchError = '';
	}

	async function fetchModels() {
		if (!canFetch || fetching) return;
		fetching = true;
		fetchError = '';
		try {
			const result = await modelServers.discover(slug, {
				server_id: apiKey ? undefined : server?.id,
				openai_base_url: openai.trim() || undefined,
				anthropic_base_url: anthropic.trim() || undefined,
				api_key: apiKey || undefined
			});
			models = result.models;
			if (models.length === 0) fetchError = m['settings.agents.no_models']();
			if (!models.includes(defaultModel)) defaultModel = models[0] ?? defaultModel;
			if (fastModel && !models.includes(fastModel)) fastModel = '';
		} catch (error) {
			fetchError = getErrorMessage(error, m['settings.agents.fetch_failed']());
		} finally {
			fetching = false;
		}
	}

	async function save(event: SubmitEvent) {
		event.preventDefault();
		if (!canSave || saving) return;
		saving = true;
		const input: ModelServerInput = {
			name: name.trim(),
			openai_base_url: openai.trim() || undefined,
			anthropic_base_url: anthropic.trim() || undefined,
			...(apiKey ? { api_key: apiKey } : {}),
			models,
			default_model: defaultModel.trim(),
			fast_model: fastModel.trim(),
			thinking,
			is_default: isDefault
		};
		try {
			const saved = server ? await modelServers.update(slug, server.id, input) : await modelServers.create(slug, input);
			appToast.success(m['settings.agents.server_saved']());
			onsaved(saved);
		} catch (error) {
			appToast.apiError(error, m['settings.agents.server_save_failed']());
		} finally {
			saving = false;
		}
	}
</script>

<Dialog.Root
	open
	onOpenChange={(open) => {
		if (!open) onclose();
	}}
>
	<Dialog.Content class="max-h-[90vh] overflow-y-auto sm:max-w-xl">
		<form onsubmit={save} class="space-y-5">
			<Dialog.Header>
				<Dialog.Title>{server ? m['settings.agents.edit_server']() : m['settings.agents.add_server']()}</Dialog.Title>
				<Dialog.Description>{m['settings.agents.server_desc']()}</Dialog.Description>
			</Dialog.Header>
			{#if !server}
				<div class="flex flex-wrap items-center gap-2">
					<span class="text-xs text-muted-foreground">{m['settings.agents.presets']()}</span>
					{#each presets as choice (choice.name)}
						<Button type="button" size="sm" variant="outline" onclick={() => preset(choice)}>{choice.name}</Button>
					{/each}
				</div>
			{/if}
			<div class="space-y-1.5">
				<Label for="model-server-name">{m['settings.agents.server_name']()}</Label>
				<Input id="model-server-name" bind:value={name} maxlength={64} required autocomplete="off" />
			</div>
			<div class="space-y-1.5">
				<Label for="model-server-openai" class="flex items-center gap-2"
					><AgentLogo agent="codex" size="sm" />{m['settings.agents.openai_url']()}</Label
				>
				<Input
					id="model-server-openai"
					type="url"
					bind:value={openai}
					placeholder="https://api.example.com/v1"
					autocomplete="off"
				/>
				<p class="text-xs text-muted-foreground">{m['settings.agents.openai_url_hint']()}</p>
			</div>
			<div class="space-y-1.5">
				<Label for="model-server-anthropic" class="flex items-center gap-2"
					><AgentLogo agent="claude-code" size="sm" />{m['settings.agents.anthropic_url']()}</Label
				>
				<Input
					id="model-server-anthropic"
					type="url"
					bind:value={anthropic}
					placeholder="https://api.example.com/anthropic"
					autocomplete="off"
				/>
				<p class="text-xs text-muted-foreground">{m['settings.agents.anthropic_url_hint']()}</p>
			</div>
			<div class="space-y-1.5">
				<Label for="model-server-key">{m['settings.agents.api_key']()}</Label>
				<Input
					id="model-server-key"
					type="password"
					bind:value={apiKey}
					autocomplete="new-password"
					placeholder={server ? m['settings.agents.api_key_stored']() : ''}
					required={!server}
				/>
				<p class="text-xs text-muted-foreground">{m['settings.agents.api_key_hint']()}</p>
			</div>
			<div class="space-y-2 rounded-lg border p-3">
				<div class="flex flex-wrap items-center justify-between gap-2">
					<p class="text-sm font-medium">
						{m['settings.agents.models']()}
						{#if models.length}<span class="font-normal text-muted-foreground"> · {models.length}</span>{/if}
					</p>
					<Button type="button" size="sm" variant="outline" disabled={!canFetch || fetching} onclick={fetchModels}
						>{#if fetching}<LoaderCircle class="animate-spin" />{:else}<RefreshCw />{/if}{m[
							'settings.agents.fetch_models'
						]()}</Button
					>
				</div>
				{#if fetchError}<p role="alert" class="text-xs text-destructive">{fetchError}</p>{/if}
				<div class="grid gap-3 sm:grid-cols-2">
					<div class="space-y-1.5">
						<Label>{m['settings.agents.default_model']()}</Label>
						{#if models.length}
							<SelectField
								bind:value={defaultModel}
								options={modelOptions}
								aria-label={m['settings.agents.default_model']()}
								class="w-full"
							/>
						{:else}
							<Input
								bind:value={defaultModel}
								aria-label={m['settings.agents.default_model']()}
								placeholder="model-id"
								autocomplete="off"
							/>
						{/if}
					</div>
					<div class="space-y-1.5">
						<Label>{m['settings.agents.fast_model']()}</Label>
						{#if models.length}
							<SelectField
								bind:value={fastModel}
								options={[{ value: '', label: m['settings.agents.same_as_default']() }, ...modelOptions]}
								aria-label={m['settings.agents.fast_model']()}
								class="w-full"
							/>
						{:else}
							<Input
								bind:value={fastModel}
								aria-label={m['settings.agents.fast_model']()}
								placeholder={m['settings.agents.same_as_default']()}
								autocomplete="off"
							/>
						{/if}
					</div>
					<div class="space-y-1.5">
						<Label>{m['settings.agents.thinking']()}</Label>
						<SelectField
							bind:value={thinking}
							options={levels.map((level) => ({ value: level, label: m[`settings.agents.thinking_${level}`]() }))}
							aria-label={m['settings.agents.thinking']()}
							class="w-full"
						/>
					</div>
				</div>
			</div>
			<label class="flex items-center justify-between gap-4">
				<span>
					<span class="block text-sm font-medium">{m['settings.agents.make_default']()}</span>
					<span class="block text-xs text-muted-foreground">{m['settings.agents.make_default_hint']()}</span>
				</span>
				<Switch bind:checked={isDefault} />
			</label>
			<Dialog.Footer>
				<Button type="button" variant="outline" onclick={onclose}>{m['common.cancel']()}</Button>
				<Button type="submit" disabled={!canSave || saving}
					>{saving ? m['settings.saving']() : m['common.save']()}</Button
				>
			</Dialog.Footer>
		</form>
	</Dialog.Content>
</Dialog.Root>
