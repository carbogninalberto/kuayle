<script lang="ts">
	import { onMount } from 'svelte';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Textarea } from '$lib/components/ui/textarea/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Switch } from '$lib/components/ui/switch/index.js';
	import { telegram, type TelegramSettings } from '$lib/api/telegram';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/paraglide/messages.js';
	let { slug }: { slug: string } = $props();
	let settings = $state<TelegramSettings | null>(null);
	let failed = $state(false);
	let busy = $state(false);
	let token = $state('');
	let ids = $state('');
	let enabled = $state(false);
	let ownID = $state('');
	let command = $state('');
	const values = $derived(ids.trim() ? ids.trim().split(/[\s,]+/) : []);
	const validID = (value: string) => /^[1-9][0-9]*$/.test(value) && Number(value) <= 4503599627370495;
	const valid = $derived(values.length <= 100 && values.every(validID));
	const statusLabel = $derived(
		settings?.status === 'connected'
			? m['telegram.connected']()
			: settings?.status === 'connection_failed'
				? m['telegram.connection_failed']()
				: settings?.status === 'disabled'
					? m['telegram.disabled']()
					: settings?.configured
						? m['telegram.saved']()
						: m['telegram.not_configured']()
	);
	function apply(value: TelegramSettings) {
		settings = value;
		enabled = value.enabled;
		ids = (value.allowed_user_ids ?? []).join('\n');
	}
	async function load() {
		failed = false;
		try {
			apply(await telegram.get(slug));
		} catch (err) {
			failed = true;
			appToast.apiError(err, m['telegram.failed']());
		}
	}
	onMount(() => {
		void load();
	});
	async function save(remove = false) {
		busy = true;
		try {
			apply(
				await telegram.save(slug, {
					enabled: remove ? false : enabled,
					allowed_user_ids: remove ? [] : [...new Set(values.map(Number))],
					...(remove ? { bot_token: null } : token.trim() ? { bot_token: token.trim() } : {})
				})
			);
			token = '';
			command = '';
			appToast.success(m['telegram.updated']());
		} catch (err) {
			appToast.apiError(err, m['telegram.failed']());
		} finally {
			busy = false;
		}
	}
	async function link() {
		busy = true;
		command = '';
		try {
			command = (await telegram.link(slug, Number(ownID))).command;
		} catch (err) {
			appToast.apiError(err, m['telegram.link_failed']());
		} finally {
			busy = false;
		}
	}
	async function unlink() {
		busy = true;
		try {
			await telegram.unlink(slug);
			if (settings) settings = { ...settings, linked_user_id: null };
			command = '';
			await load();
		} catch (err) {
			appToast.apiError(err, m['telegram.failed']());
		} finally {
			busy = false;
		}
	}
</script>

<div class="mx-auto w-full max-w-2xl space-y-6 px-4 py-8 sm:px-8 sm:py-10">
	<div>
		<h1 class="text-2xl font-semibold">{m['telegram.title']()}</h1>
		<p class="mt-2 text-sm text-muted-foreground">{m['telegram.description']()}</p>
	</div>
	{#if settings}
		<p role="status" class="text-sm">
			{statusLabel}{#if settings.bot_username}
				· @{settings.bot_username}{/if}
		</p>
		{#if settings.owner}
			<section class="space-y-5 rounded-lg border bg-card p-5" aria-label={m['telegram.bot_settings']()}>
				<div class="space-y-2">
					<label for="telegram-token" class="text-sm font-medium">{m['telegram.token']()}</label>
					<Input
						id="telegram-token"
						type="password"
						autocomplete="new-password"
						bind:value={token}
						disabled={busy}
						placeholder={settings.configured ? m['telegram.token_saved']() : m['telegram.token_placeholder']()}
					/>
					<p class="text-xs text-muted-foreground">{m['telegram.token_help']()}</p>
				</div>
				<div class="space-y-2">
					<label for="telegram-ids" class="text-sm font-medium">{m['telegram.allowed_ids']()}</label>
					<Textarea
						id="telegram-ids"
						bind:value={ids}
						rows={4}
						disabled={busy}
						aria-invalid={!valid}
						aria-describedby="telegram-ids-help"
					/>
					<p id="telegram-ids-help" class="text-xs text-muted-foreground">{m['telegram.ids_help']()}</p>
					{#if !valid}<p role="alert" class="text-sm text-destructive">{m['telegram.invalid_ids']()}</p>{/if}
				</div>
				<div class="flex items-center justify-between gap-4">
					<label for="telegram-enabled" class="text-sm font-medium">{m['telegram.enable']()}</label>
					<Switch id="telegram-enabled" bind:checked={enabled} disabled={busy} />
				</div>
				<p class="text-xs text-muted-foreground">{m['telegram.https_help']()}</p>
				<div class="flex flex-wrap gap-2">
					<Button onclick={() => save()} disabled={busy || !valid || (!settings.configured && !token.trim())}
						>{m['telegram.save']()}</Button
					>
					{#if settings.configured}<Button variant="outline" onclick={() => save(true)} disabled={busy}
							>{m['telegram.remove']()}</Button
						>{/if}
				</div>
			</section>
		{/if}
		<section class="space-y-4 rounded-lg border bg-card p-5" aria-labelledby="telegram-link-title">
			<h2 id="telegram-link-title" class="font-medium">{m['telegram.link_title']()}</h2>
			<p class="text-sm text-muted-foreground">{m['telegram.link_help']()}</p>
			{#if settings.linked_user_id}
				<p class="text-sm">{m['telegram.linked']({ id: String(settings.linked_user_id) })}</p>
				<Button variant="outline" onclick={unlink} disabled={busy}>{m['telegram.unlink']()}</Button>
			{:else}
				<div class="space-y-2">
					<label for="telegram-own-id" class="text-sm font-medium">{m['telegram.own_id']()}</label>
					<Input id="telegram-own-id" inputmode="numeric" bind:value={ownID} disabled={busy || !settings.enabled} />
				</div>
				<Button onclick={link} disabled={busy || !settings.enabled || !validID(ownID)}
					>{m['telegram.generate_link']()}</Button
				>
				{#if command}
					<div class="space-y-2" aria-live="polite">
						<p class="text-sm">{m['telegram.command_help']()}</p>
						<code class="block break-all rounded-md bg-muted p-3 text-xs select-all">{command}</code>
						{#if settings.bot_username}<a
								class="text-sm underline"
								href="https://t.me/{encodeURIComponent(settings.bot_username)}"
								target="_blank"
								rel="noopener noreferrer">{m['telegram.open_bot']()}</a
							>{/if}
					</div>
				{/if}
			{/if}
		</section>
	{:else if failed}
		<Button variant="outline" onclick={load}>{m['telegram.reload']()}</Button>
	{:else}
		<p role="status" class="text-sm text-muted-foreground">{m['telegram.loading']()}</p>
	{/if}
</div>
