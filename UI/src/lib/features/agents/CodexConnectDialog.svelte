<script lang="ts">
	import { onDestroy } from 'svelte';
	import * as Dialog from '$lib/components/ui/dialog/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { agentSignIns, type CodexDeviceChallenge } from '$lib/api/agent-sign-ins';
	import { m } from '$lib/features/i18n/messages';

	let { slug, onconnected }: {
		slug: string;
		onconnected: () => void;
	} = $props();

	const pollInterval = 2000;
	let open = $state(false);
	let challenge = $state<CodexDeviceChallenge | null>(null);
	let problem = $state<'expired' | 'failed' | null>(null);
	let copied = $state(false);
	let attempt: AbortController | null = null;
	let deviceRequest = Promise.resolve();
	let pollTimer: ReturnType<typeof setTimeout> | undefined;
	let expiryTimer: ReturnType<typeof setTimeout> | undefined;

	function cancel(workspace: string, id: string) {
		return agentSignIns.cancelCodexDevice(workspace, id).catch(() => {});
	}

	function stop(cancelChallenge = true) {
		clearTimeout(pollTimer);
		clearTimeout(expiryTimer);
		attempt?.abort();
		attempt = null;
		if (cancelChallenge && challenge) deviceRequest = cancel(slug, challenge.attempt_id);
		challenge = null;
		copied = false;
	}

	function finish(result: 'connected' | 'expired' | 'failed') {
		stop(result === 'failed');
		if (result === 'connected') {
			open = false;
			onconnected();
		} else {
			problem = result;
		}
	}

	async function poll(device: CodexDeviceChallenge, current: AbortController) {
		try {
			const { status } = await agentSignIns.pollCodexDevice(slug, device.attempt_id, current.signal);
			if (current.signal.aborted) return;
			if (status === 'pending') {
				pollTimer = setTimeout(() => void poll(device, current), pollInterval);
			} else {
				finish(status);
			}
		} catch {
			if (!current.signal.aborted) finish('failed');
		}
	}

	async function requestChallenge(workspace: string, current: AbortController) {
		if (current.signal.aborted) return;
		try {
			const device = await agentSignIns.startCodexDevice(workspace);
			if (current.signal.aborted) {
				await cancel(workspace, device.attempt_id);
				return;
			}
			challenge = device;
			const remaining = Date.parse(device.expires_at) - Date.now();
			expiryTimer = setTimeout(() => finish('expired'), remaining);
			pollTimer = setTimeout(() => void poll(device, current), pollInterval);
		} catch {
			if (!current.signal.aborted) finish('failed');
		}
	}

	function changeOpen(next: boolean) {
		open = next;
		if (!open) {
			stop();
			return;
		}
		const current = new AbortController();
		const workspace = slug;
		attempt = current;
		problem = null;
		// Finish cleaning up an earlier start before asking the server for a new code.
		deviceRequest = deviceRequest.then(() => requestChallenge(workspace, current));
	}

	async function copyCode() {
		const current = attempt;
		if (!challenge || !current) return;
		try {
			await navigator.clipboard.writeText(challenge.user_code);
			if (!current.signal.aborted) copied = true;
		} catch {
			// The code is selectable when clipboard access is unavailable.
		}
	}

	onDestroy(stop);
</script>

<Dialog.Root {open} onOpenChange={changeOpen}>
	<Dialog.Trigger>
		{#snippet child({ props })}
			<Button {...props} size="sm" variant="outline">
				{m['settings.agents.codex_connect']()}
			</Button>
		{/snippet}
	</Dialog.Trigger>
	<Dialog.Content class="max-h-[80dvh] overflow-y-auto" showCloseButton={false}>
		<Dialog.Header>
			<Dialog.Title>{m['settings.agents.codex_connect']()}</Dialog.Title>
			<Dialog.Description>{m['settings.agents.codex_device_description']()}</Dialog.Description>
		</Dialog.Header>
		{#if problem}
			<p role="alert" class="text-sm text-destructive">
				{problem === 'expired' ? m['settings.agents.codex_device_expired']() : m['settings.agents.codex_device_failed']()}
			</p>
			<p class="text-xs text-muted-foreground">{m['settings.agents.codex_sign_in_help']()}</p>
		{:else if challenge}
			<div class="flex flex-wrap items-center gap-2">
				<code dir="ltr" class="rounded-md border bg-muted px-3 py-2 font-mono text-lg font-semibold">{challenge.user_code}</code>
				<Button size="sm" variant="outline" onclick={copyCode}>{m['settings.agents.codex_device_copy']()}</Button>
			</div>
			<p role="status" class="text-xs text-muted-foreground">
				{#if copied}{m['settings.agents.codex_device_copied']()} {/if}
				{m['settings.agents.codex_device_waiting']()}
			</p>
		{:else}
			<p role="status" class="text-xs text-muted-foreground">{m['common.loading']()}</p>
		{/if}
		<Dialog.Footer>
			<Dialog.Close>
				{#snippet child({ props })}
					<Button {...props} variant="outline">{problem ? m['common.close']() : m['common.cancel']()}</Button>
				{/snippet}
			</Dialog.Close>
			{#if challenge}
				<Button href={challenge.verification_url} target="_blank" rel="noopener noreferrer">{m['settings.agents.codex_device_open']()}</Button>
			{/if}
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
