<script lang="ts">
	import { Input } from '$lib/components/ui/input/index.js';
	import { Textarea } from '$lib/components/ui/textarea/index.js';
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/paraglide/messages.js';
	import { getLocale, setLocale } from '$lib/paraglide/runtime.js';
	import { Button } from '$lib/components/ui/button';
	import * as Select from '$lib/components/ui/select';
	import { getAISettings, updateAISettings } from '$lib/api/ai-settings';
	import type { AISettings } from '$lib/types/ai-settings';

	const slug = $derived(page.params.workspaceSlug ?? '');

	let settings = $state<AISettings | null>(null);
	let provider = $state('openai_compatible');
	let baseUrl = $state('');
	let model = $state('');
	let apiKey = $state('');
	let prompt = $state('');
	let issueCopyPrompt = $state('');
	let saving = $state(false);

	onMount(async () => {
		try {
			settings = await getAISettings(slug);
			provider = settings.provider;
			baseUrl = settings.base_url;
			model = settings.model;
			prompt = settings.description_expand_prompt;
			issueCopyPrompt = settings.issue_copy_prompt;
		} catch (err: any) {
			appToast.apiError(err, m['settings.ai.failed_load']());
		}
	});

	async function saveSettings() {
		saving = true;
		try {
			const payload: any = {
				provider,
				base_url: baseUrl.trim(),
				model: model.trim(),
				description_expand_prompt: prompt.trim(),
				issue_copy_prompt: issueCopyPrompt.trim()
			};
			if (apiKey.trim()) payload.api_key = apiKey.trim();
			settings = await updateAISettings(slug, payload);
			provider = settings.provider;
			baseUrl = settings.base_url;
			model = settings.model;
			prompt = settings.description_expand_prompt;
			issueCopyPrompt = settings.issue_copy_prompt;
			apiKey = '';
			appToast.success(m['settings.ai.updated']());
		} catch (err: any) {
			appToast.apiError(err, m['settings.ai.failed_update']());
		} finally {
			saving = false;
		}
	}

	function resetPrompt() {
		if (settings) prompt = settings.default_prompt;
	}

	function resetIssueCopyPrompt() {
		if (settings) issueCopyPrompt = settings.default_issue_copy_prompt;
	}
</script>

<div class="mx-auto max-w-2xl px-8 py-10">
	<h1 class="text-2xl font-semibold text-[var(--color-text-primary)]">{m['settings.ai.title']()}</h1>
	<p class="mt-1 text-sm text-[var(--color-text-tertiary)]">{m['settings.ai.desc']()}</p>

	{#if settings}
		<div class="mt-8 rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
			<div class="flex items-center justify-between gap-4 px-5 py-4">
				<div>
					<p class="text-sm font-medium text-[var(--color-text-primary)]">{m['settings.ai.provider']()}</p>
					<p class="text-xs text-[var(--color-text-tertiary)]">{m['settings.ai.provider_desc']()}</p>
				</div>
				<Select.Root type="single" value={provider} onValueChange={(v) => v && (provider = v)}>
					<Select.Trigger size="sm" class="w-[190px]">{m['settings.ai.provider_label']()}</Select.Trigger>
					<Select.Content>
						<Select.Item value="openai_compatible">{m['settings.ai.provider_label']()}</Select.Item>
					</Select.Content>
				</Select.Root>
			</div>

			<div class="border-t border-[var(--app-border)]"></div>

			<div class="flex items-center justify-between gap-4 px-5 py-4">
				<div>
					<p class="text-sm font-medium text-[var(--color-text-primary)]">{m['settings.ai.base_url']()}</p>
					<p class="text-xs text-[var(--color-text-tertiary)]">{m['settings.ai.base_url_example']()}</p>
				</div>
				<Input
					type="url"
					bind:value={baseUrl}
					placeholder={m['settings.ai.base_url_placeholder']()}
					class="w-[300px] py-1.5"
				/>
			</div>

			<div class="border-t border-[var(--app-border)]"></div>

			<div class="flex items-center justify-between gap-4 px-5 py-4">
				<div>
					<p class="text-sm font-medium text-[var(--color-text-primary)]">{m['settings.ai.model']()}</p>
					<p class="text-xs text-[var(--color-text-tertiary)]">{m['settings.ai.model_desc']()}</p>
				</div>
				<Input
					type="text"
					bind:value={model}
					placeholder={m['settings.ai.model_placeholder']()}
					class="w-[240px] py-1.5"
				/>
			</div>

			<div class="border-t border-[var(--app-border)]"></div>

			<div class="flex items-center justify-between gap-4 px-5 py-4">
				<div>
					<p class="text-sm font-medium text-[var(--color-text-primary)]">{m['settings.ai.api_key']()}</p>
					<p class="text-xs text-[var(--color-text-tertiary)]">
						{settings.has_api_key ? m['settings.ai.key_configured']() : m['settings.ai.key_not_configured']()}
					</p>
				</div>
				<Input
					type="password"
					bind:value={apiKey}
					placeholder={settings.has_api_key ? m['settings.ai.key_configured_placeholder']() : m['settings.ai.key_placeholder']()}
					class="w-[240px] py-1.5"
				/>
			</div>
		</div>

		<div class="mt-8">
			<div class="flex items-center justify-between gap-4">
				<div>
					<h2 class="text-sm font-medium text-[var(--color-text-primary)]">{m['settings.ai.desc_prompt']()}</h2>
					<p class="text-xs text-[var(--color-text-tertiary)]">{m['settings.ai.desc_prompt_desc']()}</p>
				</div>
				<Button variant="outline" size="sm" onclick={resetPrompt}>{m['settings.ai.reset']()}</Button>
			</div>
			<Textarea
				bind:value={prompt}
				rows={8}
				class="mt-3 w-full"
			></Textarea>
		</div>

		<div class="mt-8">
			<div class="flex items-center justify-between gap-4">
				<div>
					<h2 class="text-sm font-medium text-[var(--color-text-primary)]">{m['settings.ai.copy_prompt']()}</h2>
					<p class="text-xs text-[var(--color-text-tertiary)]">
						{m['settings.ai.copy_prompt_desc']({ title: '{title}', description: '{description}', identifier: '{identifier}' })}
					</p>
				</div>
				<Button variant="outline" size="sm" onclick={resetIssueCopyPrompt}>{m['settings.ai.reset']()}</Button>
			</div>
			<Textarea
				bind:value={issueCopyPrompt}
				rows={8}
				class="mt-3 w-full font-mono"
			></Textarea>
		</div>

		<div class="mt-4 flex justify-end">
			<Button onclick={saveSettings} disabled={saving}>{saving ? m['settings.saving']() : m['settings.ai.save_settings']()}</Button>
		</div>
	{:else}
		<div class="mt-8 flex justify-center py-8">
			<div class="h-5 w-5 animate-spin rounded-full border-2 border-[var(--color-text-tertiary)] border-t-transparent"></div>
		</div>
	{/if}
</div>
