<script lang="ts">
	import { onMount } from 'svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import { Password } from '$lib/components/ui/password/index.js';
	import SelectField from '$lib/components/shared/SelectField.svelte';
	import {
		assistantResearch,
		type AssistantResearchSettings,
		type AssistantResearchUpdate
	} from '$lib/api/assistant-research';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/paraglide/messages.js';

	let { slug }: { slug: string } = $props();
	let settings = $state<AssistantResearchSettings | null>(null);
	let failed = $state(false);
	let saving = $state(false);
	let provider = $state('brave');
	let apiKey = $state('');
	let model = $state('');
	let parallel = $state(3);

	const providerLabels: Record<string, () => string> = {
		brave: () => 'Brave Search',
		tavily: () => 'Tavily',
		searxng: () => m['assistant_research.provider_searxng']()
	};
	const providerNames: Record<string, string> = { brave: 'Brave Search', tavily: 'Tavily', searxng: 'SearXNG' };
	// Providers that need no workspace key: the operator configures them.
	const keylessProviders = new Set(['searxng']);
	const providerLabel = (value: string) => providerLabels[value]?.() ?? value;
	const providerOptions = $derived(
		(settings?.search_providers ?? ['brave', 'tavily']).map((value) => ({ value, label: providerLabel(value) }))
	);
	const keyless = $derived(keylessProviders.has(provider));
	// The saved provider is no longer offered (e.g. SearXNG was turned off).
	const unavailable = $derived(
		settings && !settings.search_providers.includes(settings.search_provider) ? settings.search_provider : null
	);
	const parallelOptions = Array.from({ length: 8 }, (_, i) => ({ value: i + 1, label: String(i + 1) }));
	const readOnly = $derived(!settings?.can_manage || saving);

	function apply(next: AssistantResearchSettings) {
		settings = next;
		provider = next.search_providers.includes(next.search_provider)
			? next.search_provider
			: (next.search_providers[0] ?? next.search_provider);
		model = next.subagent_model;
		parallel = next.subagent_max_parallel;
		apiKey = '';
	}

	async function load() {
		failed = false;
		try {
			apply(await assistantResearch.get(slug));
		} catch (err) {
			failed = true;
			appToast.apiError(err, m['assistant_research.failed_load']());
		}
	}

	async function save(update: AssistantResearchUpdate, message: string) {
		if (!settings?.can_manage || saving) return;
		saving = true;
		try {
			apply(await assistantResearch.update(slug, update));
			appToast.success(message);
		} catch (err) {
			appToast.apiError(err, m['assistant_research.failed_save']());
		} finally {
			saving = false;
		}
	}

	function submit(event: SubmitEvent) {
		event.preventDefault();
		const update: AssistantResearchUpdate = {
			search_provider: provider,
			subagent_model: model.trim(),
			subagent_max_parallel: parallel
		};
		if (apiKey.trim() && !keylessProviders.has(provider)) update.search_api_key = apiKey.trim();
		void save(update, m['assistant_research.saved']());
	}

	onMount(() => {
		void load();
	});
</script>

{#if settings}
	<form class="space-y-6" onsubmit={submit} aria-labelledby="assistant-research-heading">
		<div class="space-y-1">
			<h2 id="assistant-research-heading" class="text-sm font-medium">{m['assistant_research.heading']()}</h2>
			<p class="text-xs text-muted-foreground">
				{settings.can_manage ? m['assistant_research.hint']() : m['assistant_research.admin_only']()}
			</p>
		</div>

		<section class="space-y-4 rounded-xl border bg-card p-4" aria-labelledby="assistant-research-search">
			<h3 id="assistant-research-search" class="text-sm font-medium">{m['assistant_research.search_heading']()}</h3>
			<div class="grid gap-2">
				<Label for="research-provider">{m['assistant_research.provider']()}</Label>
				<SelectField id="research-provider" bind:value={provider} options={providerOptions} disabled={readOnly} />
				{#if unavailable}
					<p class="text-xs text-destructive" role="status">
						{m['assistant_research.provider_unavailable']({ provider: providerNames[unavailable] ?? unavailable })}
					</p>
				{/if}
			</div>
			{#if keyless}
				<p class="text-xs text-muted-foreground">{m['assistant_research.searxng_hint']()}</p>
			{:else}
				<div class="grid gap-2">
					<Label for="research-key">{m['assistant_research.api_key']()}</Label>
					<Password
						id="research-key"
						bind:value={apiKey}
						autocomplete="off"
						disabled={readOnly}
						placeholder={settings.has_search_api_key
							? m['assistant_research.key_configured_placeholder']()
							: m['assistant_research.key_placeholder']()}
					/>
					<div class="flex flex-wrap items-center justify-between gap-2">
						<p class="text-xs text-muted-foreground" role="status">
							{settings.has_search_api_key
								? m['assistant_research.key_configured']()
								: m['assistant_research.key_not_configured']()}
						</p>
						{#if settings.can_manage && settings.has_search_api_key}
							<Button
								type="button"
								variant="outline"
								size="sm"
								disabled={saving}
								onclick={() => save({ search_api_key: null }, m['assistant_research.key_removed']())}
								>{m['assistant_research.remove_key']()}</Button
							>
						{/if}
					</div>
				</div>
			{/if}
		</section>

		<section class="space-y-4 rounded-xl border bg-card p-4" aria-labelledby="assistant-research-subagents">
			<div class="space-y-1">
				<h3 id="assistant-research-subagents" class="text-sm font-medium">{m['assistant_research.subagents_heading']()}</h3>
				<p class="text-xs text-muted-foreground">{m['assistant_research.subagents_hint']()}</p>
			</div>
			<div class="grid gap-2">
				<Label for="research-model">{m['assistant_research.model']()}</Label>
				<Input
					id="research-model"
					bind:value={model}
					maxlength={200}
					disabled={readOnly}
					placeholder={m['assistant_research.model_placeholder']()}
				/>
			</div>
			<div class="grid gap-2">
				<Label for="research-parallel">{m['assistant_research.parallel']()}</Label>
				<SelectField id="research-parallel" bind:value={parallel} options={parallelOptions} disabled={readOnly} />
			</div>
		</section>

		{#if settings.can_manage}
			<Button type="submit" disabled={saving}>{m['assistant_research.save']()}</Button>
		{/if}
	</form>
{:else if failed}
	<Button variant="outline" onclick={load}>{m['assistant_settings.reload']()}</Button>
{:else}
	<p role="status" class="text-sm text-muted-foreground">{m['assistant_research.loading']()}</p>
{/if}
