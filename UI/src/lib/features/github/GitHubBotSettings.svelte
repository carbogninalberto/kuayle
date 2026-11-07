<script lang="ts">
	import { onMount } from 'svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Switch } from '$lib/components/ui/switch/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import SelectField from '$lib/components/shared/SelectField.svelte';
	import { Loader2, Upload, Trash2 } from '@lucide/svelte';
	import { getGitHubBot, updateGitHubBot, uploadGitHubBotAvatar } from '$lib/api/github';
	import type { GitHubBotAbilityKey, GitHubBotSettings, GitHubBotSettingsUpdate } from '$lib/types/github';
	import { AVATAR_TYPES, validateAvatarFile } from '$lib/features/profile/avatar-rules';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/paraglide/messages.js';
	import {
		BOT_NAME_MAX,
		DEFAULT_MENTION_HARNESS,
		mentionHarnessChoice,
		mentionHarnessFromChoice,
		validateBotAvatarUrl,
		validateBotName
	} from './bot';

	let { slug, canManage }: { slug: string; canManage: boolean } = $props();

	let settings = $state<GitHubBotSettings | null>(null);
	let failed = $state(false);
	let busy = $state(false);
	let name = $state('');
	let avatarUrl = $state('');
	let harness = $state(DEFAULT_MENTION_HARNESS);
	let model = $state('');
	let previewBroken = $state(false);
	let fileInput = $state<HTMLInputElement | null>(null);

	const nameIssue = $derived(validateBotName(name));
	const urlIssue = $derived(avatarUrl.trim() ? validateBotAvatarUrl(avatarUrl) : null);
	const nameChanged = $derived(!!settings && name.trim() !== settings.display_name && !(name.trim() === '' && settings.display_name === settings.default_name));
	const mentionChanged = $derived(
		!!settings && (mentionHarnessFromChoice(harness) !== settings.mention_harness || model.trim() !== settings.mention_model)
	);
	const mentionEnabled = $derived(settings?.abilities.find((a) => a.key === 'mention_reviews')?.enabled ?? false);

	const ABILITY_TEXT: Record<GitHubBotAbilityKey, { label: () => string; description: () => string }> = {
		link_issues: { label: () => m['github_bot.ability_link_issues'](), description: () => m['github_bot.ability_link_issues_desc']() },
		task_feedback: { label: () => m['github_bot.ability_task_feedback'](), description: () => m['github_bot.ability_task_feedback_desc']() },
		review_comments: { label: () => m['github_bot.ability_review_comments'](), description: () => m['github_bot.ability_review_comments_desc']() },
		pull_request_actions: { label: () => m['github_bot.ability_pull_request_actions'](), description: () => m['github_bot.ability_pull_request_actions_desc']() },
		merge_pull_requests: { label: () => m['github_bot.ability_merge_pull_requests'](), description: () => m['github_bot.ability_merge_pull_requests_desc']() },
		releases: { label: () => m['github_bot.ability_releases'](), description: () => m['github_bot.ability_releases_desc']() },
		mention_reviews: { label: () => m['github_bot.ability_mention_reviews'](), description: () => m['github_bot.ability_mention_reviews_desc']() }
	};

	const harnessOptions = $derived([
		{ value: DEFAULT_MENTION_HARNESS, label: m['github_bot.harness_default']() },
		...(settings?.harnesses ?? []).map((id) => ({ value: id, label: id }))
	]);

	function apply(value: GitHubBotSettings) {
		settings = value;
		name = value.display_name === value.default_name ? '' : value.display_name;
		avatarUrl = value.avatar_source === 'url' ? value.avatar_url : '';
		harness = mentionHarnessChoice(value.mention_harness);
		model = value.mention_model;
		previewBroken = false;
	}

	async function load() {
		failed = false;
		try {
			const value = await getGitHubBot(slug);
			if (!Array.isArray(value?.abilities)) throw new Error('Unexpected GitHub bot settings');
			apply(value);
		} catch (err) {
			failed = true;
			appToast.apiError(err, m['github_bot.load_failed']());
		}
	}

	onMount(() => {
		void load();
	});

	async function save(update: GitHubBotSettingsUpdate) {
		busy = true;
		try {
			apply(await updateGitHubBot(slug, update));
			appToast.success(m['github_bot.saved']());
		} catch (err) {
			appToast.apiError(err, m['github_bot.save_failed']());
		} finally {
			busy = false;
		}
	}

	async function toggle(key: GitHubBotAbilityKey, enabled: boolean) {
		await save({ abilities: { [key]: enabled } });
	}

	async function pick(e: Event) {
		const input = e.currentTarget as HTMLInputElement;
		const file = input.files?.[0];
		input.value = '';
		if (!file) return;
		const issue = validateAvatarFile(file);
		if (issue) {
			appToast.error(m[`avatar.error_${issue}`]());
			return;
		}
		busy = true;
		try {
			apply(await uploadGitHubBotAvatar(slug, file));
			appToast.success(m['github_bot.saved']());
		} catch (err: any) {
			appToast.error(err?.error?.message && err.status && err.status < 500 ? err.error.message : m['avatar.error_upload']());
		} finally {
			busy = false;
		}
	}
</script>

<section class="space-y-4" aria-labelledby="github-bot-title">
	<div>
		<h3 id="github-bot-title" class="text-sm font-medium text-[var(--color-text-primary)]">{m['github_bot.title']()}</h3>
		<p class="mt-1 text-xs text-[var(--color-text-tertiary)]">{m['github_bot.description']()}</p>
	</div>

	{#if failed}
		<div class="flex items-center gap-3 text-xs text-[var(--color-text-tertiary)]">
			{m['github_bot.load_failed']()}
			<Button variant="outline" size="sm" onclick={load}>{m['github_bot.retry']()}</Button>
		</div>
	{:else if !settings}
		<div class="flex items-center justify-center py-6">
			<Loader2 size={18} class="animate-spin text-[var(--color-text-tertiary)]" />
		</div>
	{:else}
		<!-- Identity -->
		<div class="rounded-lg border border-[var(--app-border)] p-4">
			<div class="flex items-start gap-4">
				<div
					class="flex h-14 w-14 shrink-0 items-center justify-center overflow-hidden rounded-full bg-[var(--app-accent)] text-xl font-medium text-[var(--app-accent-foreground)]"
				>
					{#if settings.avatar_url && !previewBroken}
						<img
							src={settings.avatar_url}
							alt={m['github_bot.avatar_alt']({ name: settings.display_name })}
							class="h-full w-full object-cover"
							onerror={() => (previewBroken = true)}
						/>
					{:else}
						{settings.display_name.charAt(0).toUpperCase()}
					{/if}
				</div>
				<div class="min-w-0 flex-1 space-y-3">
					<div class="space-y-1.5">
						<Label for="github-bot-name">{m['github_bot.name_label']()}</Label>
						<div class="flex gap-2">
							<Input
								id="github-bot-name"
								bind:value={name}
								placeholder={settings.default_name}
								maxlength={BOT_NAME_MAX}
								disabled={!canManage || busy}
								aria-invalid={!!nameIssue}
								aria-describedby="github-bot-name-hint"
							/>
							<Button
								size="sm"
								disabled={!canManage || busy || !!nameIssue || !nameChanged}
								onclick={() => save({ display_name: name.trim() })}
							>
								{m['github_bot.save']()}
							</Button>
						</div>
						<p id="github-bot-name-hint" class="text-xs {nameIssue ? 'text-red-500' : 'text-[var(--color-text-tertiary)]'}">
							{nameIssue === 'length'
								? m['github_bot.name_too_long']({ max: BOT_NAME_MAX })
								: nameIssue === 'characters'
									? m['github_bot.name_characters']()
									: m['github_bot.name_hint']()}
						</p>
					</div>

					<div class="space-y-1.5">
						<Label for="github-bot-avatar-url">{m['github_bot.picture_label']()}</Label>
						<div class="flex flex-wrap gap-2">
							<Input
								bind:ref={fileInput}
								type="file"
								variant="plain"
								accept={AVATAR_TYPES.join(',')}
								class="hidden"
								tabindex={-1}
								aria-hidden="true"
								onchange={pick}
							/>
							<Button variant="outline" size="sm" disabled={!canManage || busy} onclick={() => fileInput?.click()}>
								<Upload size={13} />
								{m['github_bot.upload']()}
							</Button>
							{#if settings.avatar_source !== 'none'}
								<Button variant="outline" size="sm" disabled={!canManage || busy} onclick={() => save({ avatar_url: '' })}>
									<Trash2 size={13} />
									{m['github_bot.remove_picture']()}
								</Button>
							{/if}
						</div>
						<div class="flex gap-2">
							<Input
								id="github-bot-avatar-url"
								type="url"
								bind:value={avatarUrl}
								placeholder="https://"
								disabled={!canManage || busy}
								aria-invalid={!!urlIssue}
								aria-describedby="github-bot-picture-hint"
							/>
							<Button
								size="sm"
								variant="outline"
								disabled={!canManage || busy || !avatarUrl.trim() || !!urlIssue || (settings.avatar_source === 'url' && avatarUrl.trim() === settings.avatar_url)}
								onclick={() => save({ avatar_url: avatarUrl.trim() })}
							>
								{m['github_bot.use_link']()}
							</Button>
						</div>
						<p id="github-bot-picture-hint" class="text-xs {urlIssue ? 'text-red-500' : 'text-[var(--color-text-tertiary)]'}">
							{urlIssue ? m['github_bot.picture_url_invalid']() : m['github_bot.picture_hint']()}
						</p>
					</div>
				</div>
			</div>
			<p class="mt-3 text-xs text-[var(--color-text-tertiary)]">{m['github_bot.identity_note']()}</p>
		</div>

		<!-- Abilities -->
		<div>
			<h4 class="text-sm font-medium text-[var(--color-text-primary)]">{m['github_bot.abilities']()}</h4>
			<p class="mt-1 text-xs text-[var(--color-text-tertiary)]">{m['github_bot.abilities_desc']()}</p>
			<ul class="mt-3 space-y-2">
				{#each settings.abilities as ability (ability.key)}
					{@const text = ABILITY_TEXT[ability.key]}
					{#if text}
						<li class="rounded-md border border-[var(--app-border)] px-3 py-2.5">
							<div class="flex items-center justify-between gap-3">
								<div>
									<Label for="github-bot-ability-{ability.key}" class="text-sm font-normal text-[var(--color-text-primary)]">{text.label()}</Label>
									<p class="text-xs text-[var(--color-text-tertiary)]">{text.description()}</p>
								</div>
								<Switch
									id="github-bot-ability-{ability.key}"
									checked={ability.enabled}
									disabled={!canManage || busy}
									onCheckedChange={(value) => toggle(ability.key, value)}
								/>
							</div>
							{#if ability.key === 'mention_reviews'}
								<div class="mt-3 space-y-3 border-t border-[var(--app-border)] pt-3">
									<p class="text-xs text-[var(--color-text-secondary)]">
										{settings.mention_handle
											? m['github_bot.mention_how']({ handle: settings.mention_handle })
											: m['github_bot.mention_how_unknown']()}
									</p>
									<p class="rounded-md bg-[var(--color-bg-secondary)] px-3 py-2 text-xs text-[var(--color-text-secondary)]">
										{m['github_bot.mention_no_approval']()}
									</p>
									<div class="grid gap-3 sm:grid-cols-2">
										<div class="space-y-1.5">
											<Label for="github-bot-mention-harness">{m['github_bot.mention_harness']()}</Label>
											<SelectField
												id="github-bot-mention-harness"
												bind:value={harness}
												options={harnessOptions}
												disabled={!canManage || busy}
											/>
										</div>
										<div class="space-y-1.5">
											<Label for="github-bot-mention-model">{m['github_bot.mention_model']()}</Label>
											<Input
												id="github-bot-mention-model"
												bind:value={model}
												maxlength={128}
												placeholder={m['github_bot.mention_model_default']()}
												disabled={!canManage || busy}
											/>
										</div>
									</div>
									<div class="flex items-center justify-between gap-3">
										<p class="text-xs text-[var(--color-text-tertiary)]">{m['github_bot.mention_defaults']()}</p>
										<Button
											size="sm"
											disabled={!canManage || busy || !mentionChanged}
											onclick={() => save({ mention_harness: mentionHarnessFromChoice(harness), mention_model: model.trim() })}
										>
											{m['github_bot.save']()}
										</Button>
									</div>
									{#if mentionEnabled}
										<p class="text-xs text-[var(--color-text-tertiary)]">{m['github_bot.mention_setup']()}</p>
									{/if}
								</div>
							{/if}
						</li>
					{/if}
				{/each}
			</ul>
			<p class="mt-2 text-xs text-[var(--color-text-tertiary)]">{m['github_bot.status_automations_note']()}</p>
			{#if !canManage}
				<p class="mt-2 text-xs text-[var(--color-text-tertiary)]">{m['github_bot.read_only']()}</p>
			{/if}
		</div>
	{/if}
</section>
