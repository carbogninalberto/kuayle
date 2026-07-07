<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { appToast } from '$lib/features/toast/toast';
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
			appToast.apiError(err, 'Failed to load AI settings');
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
			appToast.success('AI settings updated');
		} catch (err: any) {
			appToast.apiError(err, 'Failed to update AI settings');
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
	<h1 class="text-2xl font-semibold text-[var(--color-text-primary)]">AI</h1>
	<p class="mt-1 text-sm text-[var(--color-text-tertiary)]">Configure a workspace AI provider for issue description expansion.</p>

	{#if settings}
		<div class="mt-8 rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
			<div class="flex items-center justify-between gap-4 px-5 py-4">
				<div>
					<p class="text-sm font-medium text-[var(--color-text-primary)]">Provider</p>
					<p class="text-xs text-[var(--color-text-tertiary)]">Any OpenAI-compatible API, including DeepSeek.</p>
				</div>
				<Select.Root type="single" value={provider} onValueChange={(v) => v && (provider = v)}>
					<Select.Trigger size="sm" class="w-[190px]">OpenAI-compatible</Select.Trigger>
					<Select.Content>
						<Select.Item value="openai_compatible">OpenAI-compatible</Select.Item>
					</Select.Content>
				</Select.Root>
			</div>

			<div class="border-t border-[var(--app-border)]"></div>

			<div class="flex items-center justify-between gap-4 px-5 py-4">
				<div>
					<p class="text-sm font-medium text-[var(--color-text-primary)]">Base URL</p>
					<p class="text-xs text-[var(--color-text-tertiary)]">Example: https://api.deepseek.com/v1</p>
				</div>
				<input
					type="url"
					bind:value={baseUrl}
					placeholder="https://api.deepseek.com/v1"
					class="w-[300px] rounded-md border border-[var(--app-border)] bg-[var(--color-bg)] px-3 py-1.5 text-sm text-[var(--color-text-primary)] outline-none focus:border-[var(--app-accent)]"
				/>
			</div>

			<div class="border-t border-[var(--app-border)]"></div>

			<div class="flex items-center justify-between gap-4 px-5 py-4">
				<div>
					<p class="text-sm font-medium text-[var(--color-text-primary)]">Model</p>
					<p class="text-xs text-[var(--color-text-tertiary)]">The chat model used for description expansion.</p>
				</div>
				<input
					type="text"
					bind:value={model}
					placeholder="deepseek-chat"
					class="w-[240px] rounded-md border border-[var(--app-border)] bg-[var(--color-bg)] px-3 py-1.5 text-sm text-[var(--color-text-primary)] outline-none focus:border-[var(--app-accent)]"
				/>
			</div>

			<div class="border-t border-[var(--app-border)]"></div>

			<div class="flex items-center justify-between gap-4 px-5 py-4">
				<div>
					<p class="text-sm font-medium text-[var(--color-text-primary)]">API key</p>
					<p class="text-xs text-[var(--color-text-tertiary)]">
						{settings.has_api_key ? 'A key is configured. Enter a new one to replace it.' : 'No API key configured yet.'}
					</p>
				</div>
				<input
					type="password"
					bind:value={apiKey}
					placeholder={settings.has_api_key ? 'Configured' : 'sk-...'}
					class="w-[240px] rounded-md border border-[var(--app-border)] bg-[var(--color-bg)] px-3 py-1.5 text-sm text-[var(--color-text-primary)] outline-none focus:border-[var(--app-accent)]"
				/>
			</div>
		</div>

		<div class="mt-8">
			<div class="flex items-center justify-between gap-4">
				<div>
					<h2 class="text-sm font-medium text-[var(--color-text-primary)]">Description expansion prompt</h2>
					<p class="text-xs text-[var(--color-text-tertiary)]">Customize how issue descriptions are generated or rewritten.</p>
				</div>
				<Button variant="outline" size="sm" onclick={resetPrompt}>Reset</Button>
			</div>
			<textarea
				bind:value={prompt}
				rows="8"
				class="mt-3 w-full rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-3 py-2 text-sm text-[var(--color-text-primary)] outline-none focus:border-[var(--app-accent)]"
			></textarea>
		</div>

		<div class="mt-8">
			<div class="flex items-center justify-between gap-4">
				<div>
					<h2 class="text-sm font-medium text-[var(--color-text-primary)]">Issue copy prompt</h2>
					<p class="text-xs text-[var(--color-text-tertiary)]">
						Template used by the issue AI prompt copy button. Use placeholders like {'{{issue_identifier}}'}, {'{{team_key}}'}, and {'{{issue_xml}}'}.
					</p>
				</div>
				<Button variant="outline" size="sm" onclick={resetIssueCopyPrompt}>Reset</Button>
			</div>
			<textarea
				bind:value={issueCopyPrompt}
				rows="8"
				class="mt-3 w-full rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-3 py-2 font-mono text-sm text-[var(--color-text-primary)] outline-none focus:border-[var(--app-accent)]"
			></textarea>
		</div>

		<div class="mt-4 flex justify-end">
			<Button onclick={saveSettings} disabled={saving}>{saving ? 'Saving...' : 'Save AI settings'}</Button>
		</div>
	{:else}
		<div class="mt-8 flex justify-center py-8">
			<div class="h-5 w-5 animate-spin rounded-full border-2 border-[var(--color-text-tertiary)] border-t-transparent"></div>
		</div>
	{/if}
</div>
