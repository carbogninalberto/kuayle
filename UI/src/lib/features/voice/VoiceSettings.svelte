<script lang="ts">
	import { onMount } from 'svelte';
	import { AudioLines, Clock, Server, Trash2 } from '@lucide/svelte';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import * as RadioGroup from '$lib/components/ui/radio-group/index.js';
	import { Progress } from '$lib/components/ui/progress/index.js';
	import { Switch } from '$lib/components/ui/switch/index.js';
	import { voice, type VoiceModel, type VoiceStatus } from '$lib/api/voice';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/paraglide/messages.js';
	import { downloadPercent, modelTraits } from './voice-rules';

	let { slug }: { slug: string } = $props();
	let status = $state<VoiceStatus | null>(null);
	let failed = $state(false);
	let busy = $state(false);

	// Turning voice on picks the fast multilingual model.
	const DEFAULT_MODEL = 'faster-whisper-base';

	const languageName = (model: VoiceModel) =>
		!model.language
			? m['voice.multilingual']()
			: model.language === 'it'
				? m['voice.language_it']()
				: m['voice.language_en']();
	const title = (model: VoiceModel) =>
		model.engine === 'vosk' ? `Vosk ${languageName(model)}` : `Whisper ${modelTraits(model).name}`;
	const current = $derived(status?.models.find((model) => model.id === status?.model));
	const enabled = $derived(!!status && status.model !== 'off');
	const tone = $derived.by(() => {
		switch (status?.state) {
			case 'ready':
				return 'bg-[var(--status-success)]';
			case 'queued':
			case 'absent':
			case 'downloading':
				return 'bg-[var(--status-running)] animate-pulse';
			case 'failed':
			case 'unavailable':
				return 'bg-[var(--status-error)]';
			default:
				return 'bg-muted-foreground/40';
		}
	});
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

	function toggle(on: boolean) {
		const models = status?.models ?? [];
		void choose(on ? ((models.find((model) => model.id === DEFAULT_MODEL) ?? models[0])?.id ?? 'off') : 'off');
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

{#snippet meter(label: string, value: number)}
	{#if value}
		<span class="flex items-center gap-1.5" title="{label} {value}/4">
			<span class="text-[11px] text-muted-foreground">{label}</span>
			<span class="flex gap-0.5" aria-hidden="true">
				{#each [1, 2, 3, 4] as step (step)}
					<span class="h-1.5 w-3 rounded-full {step <= value ? 'bg-primary' : 'bg-muted'}"></span>
				{/each}
			</span>
			<span class="sr-only">{value}/4</span>
		</span>
	{/if}
{/snippet}

{#snippet modelCard(model: VoiceModel)}
	{@const traits = modelTraits(model)}
	<span class="flex min-w-0 flex-1 flex-col gap-2">
		<span class="flex min-w-0 items-center gap-2">
			<span class="truncate text-sm font-medium">{title(model)}</span>
			{#if model.engine !== 'vosk'}<Badge variant="secondary" class="shrink-0 font-normal">{languageName(model)}</Badge
				>{/if}
			<span class="ml-auto shrink-0 text-xs text-muted-foreground tabular-nums">{model.size_mb} MB</span>
		</span>
		<span class="flex flex-wrap items-center gap-x-4 gap-y-1">
			{@render meter(m['voice.speed'](), traits.speed)}
			{@render meter(m['voice.accuracy'](), traits.accuracy)}
		</span>
	</span>
{/snippet}

<div class="mx-auto w-full max-w-2xl space-y-6 px-4 py-8 sm:px-8 sm:py-10">
	<div class="space-y-3">
		<h1 class="text-2xl font-semibold">{m['voice.settings_title']()}</h1>
		<p class="text-sm text-muted-foreground">{m['voice.settings_desc']()}</p>
		<ul class="flex flex-wrap gap-2 text-xs text-muted-foreground">
			<li class="flex items-center gap-1.5 rounded-full border px-2.5 py-1">
				<Server class="size-3.5" />{m['voice.fact_local']()}
			</li>
			<li class="flex items-center gap-1.5 rounded-full border px-2.5 py-1">
				<Clock class="size-3.5" />{m['voice.fact_limit']()}
			</li>
			<li class="flex items-center gap-1.5 rounded-full border px-2.5 py-1">
				<Trash2 class="size-3.5" />{m['voice.fact_deleted']()}
			</li>
		</ul>
	</div>
	{#if status}
		<section class="overflow-hidden rounded-xl border bg-card" aria-labelledby="voice-enabled-label">
			<div class="flex items-center gap-3 p-5">
				<span class="flex size-10 shrink-0 items-center justify-center rounded-lg bg-muted"
					><AudioLines class="size-5" /></span
				>
				<div class="min-w-0 flex-1 space-y-1" aria-live="polite">
					<p id="voice-enabled-label" class="text-sm font-medium">{m['voice.enabled']()}</p>
					<p role="status" class="flex items-start gap-2 text-xs text-muted-foreground">
						<span class="mt-1 size-2 shrink-0 rounded-full {tone}" aria-hidden="true"></span><span
							class="min-w-0 break-words">{stateText}</span
						>
					</p>
				</div>
				{#if status.owner}
					<Switch
						checked={enabled}
						disabled={busy || (!enabled && !status.configured)}
						aria-labelledby="voice-enabled-label"
						onCheckedChange={toggle}
					/>
				{/if}
			</div>
			{#if downloading || (status.owner && status.state === 'failed')}
				<div class="space-y-3 px-5 pb-5">
					{#if downloading}
						<Progress value={percent} max={100} class="h-1.5" aria-label={m['voice.download_progress']()} />
					{/if}
					{#if status.owner && status.state === 'failed'}
						<Button variant="outline" size="sm" disabled={busy} onclick={() => choose(status!.model)}
							>{m['voice.retry']()}</Button
						>
					{/if}
				</div>
			{/if}
			<div class="space-y-3 border-t bg-muted/30 p-5">
				<p id="voice-model-label" class="text-xs font-medium tracking-wide text-muted-foreground uppercase">
					{m['voice.model_label']()}
				</p>
				{#if status.owner}
					<RadioGroup.Root
						value={status.model}
						onValueChange={(value) => value && value !== status?.model && void choose(value)}
						aria-labelledby="voice-model-label"
						class="grid gap-2 sm:grid-cols-2"
					>
						{#each status.models as model (model.id)}
							<Label
								for="voice-model-{model.id}"
								class="flex min-w-0 cursor-pointer items-start gap-3 rounded-lg border bg-card p-3 font-normal transition-colors hover:bg-muted/50 has-[[data-state=checked]]:border-primary has-[[data-state=checked]]:ring-1 has-[[data-state=checked]]:ring-primary has-[:disabled]:cursor-not-allowed has-[:disabled]:opacity-50"
							>
								<RadioGroup.Item
									id="voice-model-{model.id}"
									value={model.id}
									disabled={busy || !status.configured}
									class="mt-0.5"
								/>
								{@render modelCard(model)}
							</Label>
						{/each}
					</RadioGroup.Root>
				{:else}
					{#if current}
						<div class="flex rounded-lg border bg-card p-3">{@render modelCard(current)}</div>
					{/if}
					<p class="text-xs text-muted-foreground">{m['voice.owner_only']()}</p>
				{/if}
			</div>
		</section>
	{:else if failed}
		<Button variant="outline" onclick={load}>{m['voice.reload']()}</Button>
	{:else}
		<p role="status" class="text-sm text-muted-foreground">{m['voice.loading']()}</p>
	{/if}
</div>
