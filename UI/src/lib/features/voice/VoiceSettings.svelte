<script lang="ts">
	import { onMount } from 'svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import * as Select from '$lib/components/ui/select/index.js';
	import { Progress } from '$lib/components/ui/progress/index.js';
	import { voice, type VoiceModel, type VoiceStatus } from '$lib/api/voice';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/paraglide/messages.js';
	import { downloadPercent } from './voice-rules';

	let { slug }: { slug: string } = $props();
	let status = $state<VoiceStatus | null>(null);
	let failed = $state(false);
	let busy = $state(false);

	function label(model: VoiceModel) {
		const size = `${model.size_mb} MB`;
		if (model.engine === 'vosk')
			return m['voice.option_vosk']({
				language: model.language === 'it' ? m['voice.language_it']() : m['voice.language_en'](),
				size
			});
		return m['voice.option_whisper']({ name: model.id.replace('faster-whisper-', ''), size });
	}
	const options = $derived([
		{ value: 'off', label: m['voice.model_off']() },
		...(status?.models ?? []).map((model) => ({ value: model.id, label: label(model) }))
	]);
	const selected = $derived(options.find((option) => option.value === status?.model)?.label ?? m['voice.model_off']());
	const downloading = $derived(status?.state === 'queued' || status?.state === 'downloading');
	const percent = $derived(downloadPercent(status?.downloaded_bytes, status?.total_bytes));
	const stateText = $derived.by(() => {
		if (!status) return '';
		switch (status.state) {
			case 'off':
				return status.configured ? m['voice.state_off']() : m['voice.state_not_configured']();
			case 'unavailable':
				return m['voice.state_unavailable']();
			case 'queued':
			case 'absent':
				return m['voice.state_queued']();
			case 'downloading':
				return m['voice.state_downloading']({ percent: String(percent) });
			case 'ready':
				return m['voice.state_ready']();
			case 'failed':
				return m['voice.state_failed']({ error: status.error ?? '' });
		}
	});

	async function load() {
		failed = false;
		try {
			status = await voice.status(slug);
		} catch (err) {
			failed = true;
			appToast.apiError(err, m['voice.failed_load']());
		}
	}

	async function choose(model: string) {
		if (!status || busy) return;
		busy = true;
		try {
			status = await voice.select(slug, model);
			appToast.success(m['voice.updated']());
		} catch (err) {
			appToast.apiError(err, m['voice.failed_update']());
		} finally {
			busy = false;
		}
	}

	onMount(() => {
		void load();
	});

	// Follow the download until the model is ready or fails.
	$effect(() => {
		if (!downloading) return;
		const timer = setInterval(async () => {
			try {
				status = await voice.status(slug);
			} catch {
				/* The next tick retries. */
			}
		}, 2000);
		return () => clearInterval(timer);
	});
</script>

<div class="mx-auto w-full max-w-2xl space-y-6 px-4 py-8 sm:px-8 sm:py-10">
	<div>
		<h1 class="text-2xl font-semibold">{m['voice.settings_title']()}</h1>
		<p class="mt-2 text-sm text-muted-foreground">{m['voice.settings_desc']()}</p>
	</div>
	{#if status}
		<section class="space-y-4 rounded-lg border bg-card p-5" aria-labelledby="voice-model-label">
			<div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
				<div class="min-w-0">
					<p id="voice-model-label" class="text-sm font-medium">{m['voice.model_label']()}</p>
					<p class="text-xs text-muted-foreground">{m['voice.model_desc']()}</p>
				</div>
				{#if status.owner}
					<Select.Root type="single" value={status.model} onValueChange={(value) => value && void choose(value)} items={options}>
						<Select.Trigger class="w-full sm:w-[280px]" aria-labelledby="voice-model-label" disabled={busy}
							>{selected}</Select.Trigger
						>
						<Select.Content>
							{#each options as option (option.value)}
								<Select.Item
									value={option.value}
									label={option.label}
									disabled={option.value !== 'off' && !status.configured}>{option.label}</Select.Item
								>
							{/each}
						</Select.Content>
					</Select.Root>
				{:else}
					<p class="text-sm">{selected}</p>
				{/if}
			</div>
			<div class="space-y-2" aria-live="polite">
				<p role="status" class="text-sm">{stateText}</p>
				{#if downloading}
					<Progress value={percent} max={100} class="h-1.5" aria-label={m['voice.download_progress']()} />
				{/if}
				{#if status.owner && status.state === 'failed'}
					<Button variant="outline" size="sm" disabled={busy} onclick={() => choose(status!.model)}
						>{m['voice.retry']()}</Button
					>
				{/if}
			</div>
			{#if status.owner}
				<p class="text-xs text-muted-foreground">{m['voice.model_hint']()}</p>
			{:else}
				<p class="text-xs text-muted-foreground">{m['voice.owner_only']()}</p>
			{/if}
		</section>
	{:else if failed}
		<Button variant="outline" onclick={load}>{m['voice.reload']()}</Button>
	{:else}
		<p role="status" class="text-sm text-muted-foreground">{m['voice.loading']()}</p>
	{/if}
</div>
