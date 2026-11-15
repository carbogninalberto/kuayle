<script lang="ts">
	import PageTitle from '$lib/components/shared/PageTitle.svelte';
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import {
		listWebhooks,
		createWebhook,
		updateWebhook,
		deleteWebhook,
		listWebhookDeliveries,
		type Webhook,
		type WebhookDelivery
	} from '$lib/api/webhooks';
	import { getWorkspace } from '$lib/api/workspaces';
	import { WEBHOOK_EVENTS, canEnableWebhooks, deliveryProblem, deliveryState, type DeliveryState } from '$lib/features/webhooks/deliveries';
	import * as Alert from '$lib/components/ui/alert';
	import * as Dialog from '$lib/components/ui/dialog';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Password } from '$lib/components/ui/password';
	import { Label } from '$lib/components/ui/label';
	import { Badge } from '$lib/components/ui/badge';
	import { Checkbox } from '$lib/components/ui/checkbox';
	import { Switch } from '$lib/components/ui/switch';
	import EmptyState from '$lib/components/shared/EmptyState.svelte';
	import { appToast } from '$lib/features/toast/toast';
	import { m, getLocale, setLocale } from '$lib/features/i18n/messages';
	import { formatRelativeTime } from '$lib/utils/format';
	import { Plus, Trash2, ExternalLink, History, TriangleAlert } from '@lucide/svelte';

	const slug = $derived(page.params.workspaceSlug ?? '');
	let webhooks = $state<Webhook[]>([]);
	let loading = $state(true);
	let showCreate = $state(false);
	// The workspace says whether it has or had a private team: webhooks are
	// off there, and creating or enabling one is refused.
	let workspace = $state<{ privacy_enabled?: boolean } | null>(null);
	const webhooksAllowed = $derived(canEnableWebhooks(workspace));

	let deliveriesFor = $state<Webhook | null>(null);
	let deliveries = $state<WebhookDelivery[]>([]);
	let deliveriesLoading = $state(false);
	let showDeliveries = $state(false);

	let newUrl = $state('');
	let newSecret = $state('');
	let newEvents = $state<string[]>(['issue.created', 'issue.updated']);

	const DELIVERY_STATE_LABEL: Record<DeliveryState, () => string> = {
		pending: () => m['settings.webhooks.delivery_pending'](),
		retrying: () => m['settings.webhooks.delivery_retrying'](),
		delivered: () => m['settings.webhooks.delivery_delivered'](),
		failed: () => m['settings.webhooks.delivery_failed']()
	};
	const DELIVERY_STATE_VARIANT = {
		pending: 'outline',
		retrying: 'outline',
		delivered: 'secondary',
		failed: 'destructive'
	} as const;

	onMount(async () => {
		void getWorkspace(slug)
			.then((ws) => (workspace = ws))
			// Unknown privacy: let the server decide.
			.catch(() => (workspace = {}));
		try {
			webhooks = await listWebhooks(slug);
		} finally {
			loading = false;
		}
	});

	function formatWhen(date: string) {
		return new Intl.DateTimeFormat(getLocale(), { dateStyle: 'short', timeStyle: 'short' }).format(new Date(date));
	}

	async function openDeliveries(webhook: Webhook) {
		deliveriesFor = webhook;
		deliveries = [];
		deliveriesLoading = true;
		showDeliveries = true;
		try {
			const loaded = await listWebhookDeliveries(slug, webhook.id);
			if (deliveriesFor?.id === webhook.id) deliveries = loaded;
		} catch (err: any) {
			appToast.apiError(err, m['settings.webhooks.failed_deliveries']());
		} finally {
			if (deliveriesFor?.id === webhook.id) deliveriesLoading = false;
		}
	}

	function resetForm() {
		newUrl = '';
		newSecret = '';
		newEvents = ['issue.created', 'issue.updated'];
	}

	async function handleCreate(e: Event) {
		e.preventDefault();
		if (!newUrl.trim() || !newSecret.trim() || newEvents.length === 0) return;
		try {
			const w = await createWebhook(slug, {
				url: newUrl.trim(),
				secret: newSecret.trim(),
				events: newEvents
			});
			webhooks = [w, ...webhooks];
			showCreate = false;
			resetForm();
			appToast.success(m['settings.webhooks.created_toast']());
		} catch (err: any) {
			appToast.apiError(err, m['settings.webhooks.failed_create']());
		}
	}

	async function handleToggleActive(webhook: Webhook) {
		try {
			const updated = await updateWebhook(slug, webhook.id, { is_active: !webhook.is_active });
			webhooks = webhooks.map((w) => (w.id === webhook.id ? updated : w));
			appToast.success(updated.is_active ? m['settings.webhooks.enabled']() : m['settings.webhooks.disabled']());
		} catch (err: any) {
			appToast.apiError(err, m['settings.webhooks.failed_update']());
		}
	}

	async function handleDelete(id: string) {
		try {
			await deleteWebhook(slug, id);
			webhooks = webhooks.filter((w) => w.id !== id);
			appToast.success(m['settings.webhooks.deleted']());
		} catch (err: any) {
			appToast.apiError(err, m['settings.webhooks.failed_delete']());
		}
	}

	function toggleEvent(event: string) {
		if (newEvents.includes(event)) {
			newEvents = newEvents.filter((e) => e !== event);
		} else {
			newEvents = [...newEvents, event];
		}
	}
</script>

<div class="mx-auto max-w-2xl px-8 py-10">
	<div class="flex items-center justify-between">
		<h1 class="text-2xl font-semibold text-[var(--color-text-primary)]">{m['settings.webhooks.title']()}</h1>
		<Button variant="plain" onclick={() => { resetForm(); showCreate = true; }} disabled={!webhooksAllowed}
			class="flex items-center gap-1 rounded-md bg-[var(--app-accent)] px-3 py-1.5 text-sm text-[var(--app-accent-foreground)] hover:bg-[var(--app-accent-hover)] disabled:cursor-not-allowed disabled:opacity-50"
		>
			<Plus size={14} />
			{m['settings.webhooks.new']()}
		</Button>
	</div>

	{#if workspace?.privacy_enabled}
		<Alert.Root class="mt-6 border-[var(--color-warning)] bg-[var(--color-bg-secondary)] text-[var(--color-warning)]">
			<TriangleAlert />
			<Alert.Title>{m['settings.webhooks.privacy_off']()}</Alert.Title>
		</Alert.Root>
	{/if}

	<div class="mt-8">
		{#if loading}
			<div class="flex h-64 items-center justify-center">
			</div>
		{:else if webhooks.length === 0}
			<EmptyState
				title={m['settings.webhooks.no_webhooks']()}
				description={m['settings.webhooks.no_webhooks_desc']()}
				action={webhooksAllowed ? { label: m['settings.webhooks.new'](), onclick: () => { resetForm(); showCreate = true; } } : undefined}
			/>
		{:else}
			<div class="rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
				{#each webhooks as webhook, i}
					<div class="flex items-center gap-4 px-5 py-4 {i > 0 ? 'border-t border-[var(--app-border)]' : ''}">
						<div class="flex-1 min-w-0">
							<div class="flex items-center gap-2">
								<ExternalLink size={14} class="shrink-0 text-[var(--color-text-tertiary)]" />
								<span class="truncate text-sm font-medium text-[var(--color-text-primary)]">{webhook.url}</span>
								{#if !webhook.is_active}
									<Badge variant="outline" class="text-[10px]">{m['settings.webhooks.inactive']()}</Badge>
								{/if}
							</div>
							<div class="mt-1 flex flex-wrap items-center gap-1.5">
								{#each webhook.events as event}
									<Badge variant="secondary" class="text-[10px]">{event}</Badge>
								{/each}
							</div>
							<p class="mt-1 text-xs text-[var(--color-text-tertiary)]">
								{m['settings.webhooks.created']({ time: formatRelativeTime(webhook.created_at, getLocale()) })}
							</p>
						</div>
						<div class="flex items-center gap-2">
							<Button aria-label={m['settings.webhooks.deliveries']()} title={m['settings.webhooks.deliveries']()}
								variant="ghost"
								size="icon-sm"
								onclick={() => openDeliveries(webhook)}
								class="text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)]"
							>
								<History size={14} />
							</Button>
							<Switch
								checked={webhook.is_active}
								disabled={!webhook.is_active && !webhooksAllowed}
								onCheckedChange={() => handleToggleActive(webhook)}
							/>
							<Button aria-label={m['common.delete']()}
								variant="ghost"
								size="icon-sm"
								onclick={() => handleDelete(webhook.id)}
								class="text-[var(--color-text-tertiary)] hover:text-[var(--color-error)]"
							>
								<Trash2 size={14} />
							</Button>
						</div>
					</div>
				{/each}
			</div>
		{/if}
	</div>
</div>

<Dialog.Root bind:open={showCreate}>
	<Dialog.Content class="sm:max-w-[480px] border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-0 overflow-hidden rounded-xl">
		<form onsubmit={handleCreate}>
			<div class="px-5 pt-5 pb-4 space-y-4">
				<div>
					<h2 class="text-base font-semibold text-[var(--color-text-primary)]">{m['settings.webhooks.create_title']()}</h2>
					<p class="mt-0.5 text-xs text-[var(--color-text-tertiary)]">{m['settings.webhooks.create_desc']()}</p>
				</div>

				<div class="space-y-1.5">
					<Label class="text-xs text-[var(--color-text-secondary)]">{m['settings.webhooks.payload_url']()}</Label>
					<Input
						bind:value={newUrl}
						placeholder={m['settings.webhooks.payload_placeholder']()}
						required
						class=""
					/>
				</div>

				<div class="space-y-1.5">
					<Label class="text-xs text-[var(--color-text-secondary)]">{m['settings.webhooks.secret']()}</Label>
					<Password
						bind:value={newSecret}
						placeholder={m['settings.webhooks.secret_placeholder']()}
						required
						class="bg-[var(--color-bg)] border-[var(--app-border)] text-[var(--color-text-primary)]"
					/>
					<p class="text-[10px] text-[var(--color-text-tertiary)]">{m['settings.webhooks.secret_desc']()}</p>
				</div>

				<div class="space-y-1.5">
					<Label class="text-xs text-[var(--color-text-secondary)]">{m['settings.webhooks.events']()}</Label>
					<div class="grid grid-cols-2 gap-2">
						{#each WEBHOOK_EVENTS as event}
							<Button variant="plain" type="button"
								onclick={() => toggleEvent(event)}
								class="flex items-center gap-2 rounded-md border border-[var(--app-border)] px-3 py-2 text-left hover:bg-[var(--color-bg-hover)]"
							>
								<Checkbox checked={newEvents.includes(event)} />
								<span class="text-xs text-[var(--color-text-primary)]">{event}</span>
							</Button>
						{/each}
					</div>
				</div>
			</div>

			<div class="flex justify-end gap-2 border-t border-[var(--app-border)] px-5 py-3">
				<Button variant="outline" size="sm" type="button" onclick={() => (showCreate = false)}>{m['settings.webhooks.cancel']()}</Button>
				<Button size="sm" type="submit" disabled={!newUrl.trim() || !newSecret.trim() || newEvents.length === 0}>{m['settings.webhooks.create_button']()}</Button>
			</div>
		</form>
	</Dialog.Content>
</Dialog.Root>

<Dialog.Root bind:open={showDeliveries}>
	<Dialog.Content class="sm:max-w-[560px] border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-0 overflow-hidden rounded-xl">
		<Dialog.Header class="px-5 pt-5">
			<Dialog.Title class="text-base font-semibold text-[var(--color-text-primary)]">{m['settings.webhooks.deliveries']()}</Dialog.Title>
			{#if deliveriesFor}
				<Dialog.Description class="truncate text-xs text-[var(--color-text-tertiary)]">{deliveriesFor.url}</Dialog.Description>
			{/if}
		</Dialog.Header>
		<div class="max-h-[60vh] overflow-y-auto px-5 pb-5">
			{#if deliveriesLoading}
				<div class="h-24"></div>
			{:else if deliveries.length === 0}
				<p class="py-8 text-center text-sm text-[var(--color-text-tertiary)]">{m['settings.webhooks.no_deliveries']()}</p>
			{:else}
				<ul class="divide-y divide-[var(--app-border)] rounded-lg border border-[var(--app-border)]">
					{#each deliveries as delivery (delivery.id)}
						{@const current = deliveryState(delivery)}
						{@const problem = deliveryProblem(delivery)}
						<li class="px-3 py-2.5">
							<div class="flex items-center gap-2">
								<Badge variant={DELIVERY_STATE_VARIANT[current]} class="text-[10px]">{DELIVERY_STATE_LABEL[current]()}</Badge>
								<span class="truncate text-xs font-medium text-[var(--color-text-primary)]">{delivery.event}</span>
								<span class="ml-auto shrink-0 text-[11px] text-[var(--color-text-tertiary)]">{formatWhen(delivery.created_at)}</span>
							</div>
							<p class="mt-1 flex flex-wrap gap-x-3 text-[11px] text-[var(--color-text-tertiary)]">
								<span>{m['settings.webhooks.attempts']({ count: delivery.attempts })}</span>
								{#if problem}
									<span>{m['settings.webhooks.last_error']({ error: problem })}</span>
								{/if}
								{#if delivery.next_attempt_at && current === 'retrying'}
									<span>{m['settings.webhooks.next_attempt']({ time: formatWhen(delivery.next_attempt_at) })}</span>
								{/if}
							</p>
						</li>
					{/each}
				</ul>
			{/if}
		</div>
	</Dialog.Content>
</Dialog.Root>

<PageTitle parts={[m['settings.webhooks.title'](), m['settings.title']()]} />
