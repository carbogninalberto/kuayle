<script lang="ts">
	import { onMount } from 'svelte';
	import ConfirmDialog from '$lib/components/shared/ConfirmDialog.svelte';
	import PageTitle from '$lib/components/shared/PageTitle.svelte';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Switch } from '$lib/components/ui/switch';
	import { generatePushKeys, getPushSettings, savePushSettings, sendTestPush, type PushSettings } from '$lib/api/push-settings';
	import { authState } from '$lib/features/auth/auth.state.svelte';
	import { m } from '$lib/features/i18n/messages';
	import { getErrorMessage } from '$lib/features/toast/toast';

	const admin = $derived(authState.user?.is_sysadmin === true);
	let settings = $state<PushSettings>();
	let subject = $state('');
	let enabled = $state(false);
	let busy = $state(false);
	let confirmRotation = $state(false);
	let notice = $state('');
	let error = $state('');
	const readOnly = $derived(busy || settings?.read_only);
	const dirty = $derived(subject !== settings?.subject || enabled !== settings?.enabled);

	async function perform(action: () => Promise<void>) {
		if (busy) return;
		busy = true;
		error = '';
		notice = '';
		try {
			await action();
		} catch (err) {
			error = getErrorMessage(err, m['settings.push.failed']());
		} finally {
			busy = false;
		}
	}

	function apply(value: PushSettings) {
		settings = value;
		subject = value.subject;
		enabled = value.enabled;
	}

	function load() {
		return perform(async () => apply(await getPushSettings()));
	}

	onMount(() => { if (admin) void load(); });

	function generate(confirm: boolean) {
		confirmRotation = false;
		return perform(async () => {
			settings = await generatePushKeys(confirm);
			if (settings.requires_reenrollment) notice = m['settings.push.rotation_warning']();
		});
	}

	function save() {
		return perform(async () => {
			apply(await savePushSettings(subject.trim(), enabled));
			notice = m['settings.push.saved']();
		});
	}

	function testPush() {
		return perform(async () => {
			const result = await sendTestPush();
			notice = m['settings.push.result'](result);
			settings = await getPushSettings();
		});
	}
</script>

<PageTitle parts={[m['settings.push.title'](), m['settings.title']()]} />

<div class="mx-auto max-w-3xl space-y-6 px-5 py-10 sm:px-8">
	<h1 class="text-2xl font-semibold">{m['settings.push.title']()}</h1>
	{#if !admin}
		<p>{m['settings.push.denied']()}</p>
	{:else}
		{#if error}
			<p role="alert" class="text-sm text-destructive">{error}</p>
			{#if !settings}<Button variant="outline" disabled={busy} onclick={load}>{m['settings.tokens.retry']()}</Button>{/if}
		{/if}
		{#if settings}
			<section class="space-y-4 rounded-lg border bg-card p-5">
				<p class="text-sm">
					<strong>{settings.enabled ? m['common.enabled']() : m['common.disabled']()}</strong>
					{#if settings.source === 'environment'}{m['settings.push.environment']()}
					{:else if settings.source === 'app'}{m['settings.push.app']()}
					{:else}{m['settings.push.off']()}{/if}
				</p>
				{#if settings.public_key}
					<div>
						<h2 class="text-sm font-medium">{m['settings.push.public_key']()}</h2>
						<code class="select-all break-all text-xs text-muted-foreground">{settings.public_key}</code>
					</div>
				{/if}
				<Button variant="outline" disabled={readOnly} onclick={() => settings?.public_key ? confirmRotation = true : generate(false)}>
					{settings.public_key ? m['settings.push.regenerate']() : m['settings.push.generate']()}
				</Button>
			</section>

			<form class="space-y-4 rounded-lg border bg-card p-5" onsubmit={(event) => { event.preventDefault(); void save(); }}>
				<div class="grid gap-2">
					<Label for="push-contact">{m['settings.push.contact']()}</Label>
					<Input id="push-contact" bind:value={subject} disabled={readOnly} required={enabled} maxlength={2048}
						aria-describedby="push-contact-help" />
					<p id="push-contact-help" class="text-xs text-muted-foreground">{m['settings.push.contact_hint']()}</p>
				</div>
				<div class="flex items-center gap-3">
					<Switch id="push-enabled" bind:checked={enabled} disabled={readOnly || !settings.public_key} />
					<Label for="push-enabled">{m['settings.push.enable']()}</Label>
				</div>
				<Button type="submit" disabled={readOnly || !dirty}>{m['common.save']()}</Button>
			</form>

			<section class="space-y-3 rounded-lg border bg-card p-5">
				<p class="text-sm">{m['settings.push.devices']({ count: settings.subscribed_devices })}</p>
				<Button variant="outline" disabled={busy || !settings.enabled} onclick={testPush}>{m['settings.push.test']()}</Button>
			</section>
			<p role="status" class="text-sm">{notice}</p>
		{:else if !error}
			<p role="status">{m['common.loading']()}</p>
		{/if}
	{/if}
</div>

{#if confirmRotation}
	<ConfirmDialog title={m['settings.push.regenerate']()} message={m['settings.push.rotation_warning']()}
		confirmLabel={m['settings.push.regenerate']()} onconfirm={() => generate(true)} oncancel={() => confirmRotation = false} />
{/if}
