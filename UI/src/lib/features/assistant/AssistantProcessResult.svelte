<script lang="ts">
	import { ExternalLink, Square } from '@lucide/svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import { conversations, type AssistantProcess } from '$lib/api/conversations';
	import { openAppPort } from '$lib/features/dev-machines/open-app-port';
	import { getLocale, m } from '$lib/features/i18n/messages';
	import { appToast } from '$lib/features/toast/toast';

	let { slug, conversationId, processId }: { slug: string; conversationId: string; processId: string } = $props();
	let process = $state<AssistantProcess>();
	let unavailable = $state(false);
	let action = $state<'open' | 'stop' | null>(null);
	let refresh = $state(0);
	const status = $derived(process?.status ?? 'queued');
	const active = $derived(status === 'queued' || status === 'running');
	const deadline = $derived(
		process && new Intl.DateTimeFormat(getLocale(), { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(process.expires_at))
	);

	$effect(() => {
		const current = { slug, conversationId, processId, refresh };
		let disposed = false;
		let timer: ReturnType<typeof setTimeout> | undefined;

		async function poll() {
			try {
				const result = await conversations.assistantProcess(current.slug, current.conversationId, current.processId);
				if (disposed) return;
				process = result;
				unavailable = false;
				if (result.status === 'stopped' || result.status === 'failed') return;
			} catch {
				if (disposed) return;
				unavailable = true;
			}
			timer = setTimeout(() => void poll(), 3000);
		}
		void poll();
		return () => {
			disposed = true;
			clearTimeout(timer);
		};
	});

	async function open() {
		if (!process?.machine_id || action) return;
		action = 'open';
		try {
			await openAppPort(slug, process.machine_id, process.id);
		} catch (error) {
			appToast.apiError(error, m['machines.port_not_ready']());
		} finally {
			action = null;
		}
	}

	async function stop() {
		if (action) return;
		action = 'stop';
		try {
			process = await conversations.stopAssistantProcess(slug, conversationId, processId);
			refresh++;
		} catch (error) {
			appToast.apiError(error, m['assistant.process_stop_failed']());
		} finally {
			action = null;
		}
	}
</script>

<div class="min-w-0 space-y-1.5 pl-6 text-xs" data-testid="assistant-process-result">
	<p role="status" class="text-muted-foreground">
		{unavailable ? m['assistant.process_unavailable']() : m[`assistant.process_${status}`]()}
		{#if !unavailable && active && deadline} · {m['assistant.process_expires']({ date: deadline })}{/if}
	</p>
	{#if process && active && !unavailable}
		<div class="flex flex-wrap items-center gap-1.5">
			{#if status === 'running' && process.preview_url}
				<Button size="sm" variant="outline" disabled={action !== null} onclick={() => void open()}>
					<ExternalLink />{m['assistant.process_open']()}
				</Button>
			{/if}
			<Button size="sm" variant="outline" disabled={action !== null} onclick={() => void stop()}>
				<Square />{m['assistant.tool_stop_process']()}
			</Button>
		</div>
		{#if status === 'running' && process.preview_url}
			<p class="break-all text-muted-foreground">{process.preview_url}</p>
		{/if}
	{/if}
</div>
