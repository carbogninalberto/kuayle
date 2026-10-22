<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { listWebhooks, createWebhook, updateWebhook, deleteWebhook, type Webhook } from '$lib/api/webhooks';
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
	import { m } from '$lib/paraglide/messages.js';
	import { getLocale, setLocale } from '$lib/paraglide/runtime.js';
	import { formatRelativeTime } from '$lib/utils/format';
	import { Plus, Trash2, ExternalLink } from '@lucide/svelte';

	const slug = $derived(page.params.workspaceSlug ?? '');
	let webhooks = $state<Webhook[]>([]);
	let loading = $state(true);
	let showCreate = $state(false);

	let newUrl = $state('');
	let newSecret = $state('');
	let newEvents = $state<string[]>(['issue.created', 'issue.updated']);

	const ALL_EVENTS = [
		'issue.created',
		'issue.updated',
		'issue.deleted',
		'issue.triaged',
		'comment.created',
		'cycle.completed',
		'project.updated'
	];

	onMount(async () => {
		try {
			webhooks = await listWebhooks(slug);
		} finally {
			loading = false;
		}
	});

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
		<Button variant="default" size="sm"
			onclick={() => { resetForm(); showCreate = true; }}
			class="flex items-center gap-1 rounded-md px-3 py-1.5 text-sm"
		>
			<Plus size={14} />
			{m['settings.webhooks.new']()}
		</Button>
	</div>

	<div class="mt-8">
		{#if loading}
			<div class="flex h-64 items-center justify-center">
			</div>
		{:else if webhooks.length === 0}
			<EmptyState
				title={m['settings.webhooks.no_webhooks']()}
				description={m['settings.webhooks.no_webhooks_desc']()}
				action={{ label: m['settings.webhooks.new'](), onclick: () => { resetForm(); showCreate = true; } }}
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
							<Switch
								checked={webhook.is_active}
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
						{#each ALL_EVENTS as event}
							<Button variant="outline" size="sm"
								type="button"
								onclick={() => toggleEvent(event)}
								class="h-auto justify-start whitespace-normal flex items-center gap-2 rounded-md border px-3 py-2 text-left"
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
