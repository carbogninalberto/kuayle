<script lang="ts">
	import { onMount } from 'svelte';
	import { Plus, Pencil, Trash2, Server } from '@lucide/svelte';
	import * as AlertDialog from '$lib/components/ui/alert-dialog/index.js';
	import * as Item from '$lib/components/ui/item/index.js';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { modelServers } from '$lib/api/dev-machines';
	import type { ModelServer } from '$lib/types/dev-machine';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/paraglide/messages.js';
	import AgentLogo from './AgentLogo.svelte';
	import ModelServerDialog from './ModelServerDialog.svelte';

	let { slug, canAdmin }: { slug: string; canAdmin: boolean } = $props();
	let servers = $state<ModelServer[]>([]),
		loading = $state(true),
		failed = $state(false),
		editing = $state<ModelServer | null | undefined>(undefined),
		removing = $state<ModelServer | null>(null),
		removeBusy = $state(false);

	async function load() {
		loading = true;
		failed = false;
		try {
			servers = await modelServers.list(slug);
		} catch {
			failed = true;
		} finally {
			loading = false;
		}
	}
	onMount(load);

	async function remove() {
		if (!removing || removeBusy) return;
		removeBusy = true;
		try {
			await modelServers.remove(slug, removing.id);
			appToast.success(m['settings.agents.server_removed']());
			removing = null;
			await load();
		} catch (error) {
			appToast.apiError(error, m['settings.agents.server_remove_failed']());
		} finally {
			removeBusy = false;
		}
	}
</script>

<section aria-labelledby="model-servers-title" class="space-y-4 rounded-xl border bg-card p-5">
	<div class="flex flex-wrap items-start justify-between gap-3">
		<div>
			<h2 id="model-servers-title" class="text-sm font-semibold">{m['settings.agents.servers_title']()}</h2>
			<p class="text-xs text-muted-foreground">{m['settings.agents.servers_desc']()}</p>
		</div>
		{#if canAdmin}<Button size="sm" variant="outline" onclick={() => (editing = null)}
				><Plus />{m['settings.agents.add_server']()}</Button
			>{/if}
	</div>
	{#if loading}<p role="status" class="text-xs text-muted-foreground">{m['common.loading']()}</p>
	{:else if failed}
		<div class="flex items-center justify-between gap-3">
			<p role="alert" class="text-sm text-destructive">{m['settings.agents.servers_failed']()}</p>
			<Button size="sm" variant="outline" onclick={load}>{m['machines.refresh']()}</Button>
		</div>
	{:else if servers.length === 0}
		<p class="text-xs text-muted-foreground">{m['settings.agents.no_servers']()}</p>
	{:else}
		<div class="space-y-2">
			{#each servers as server (server.id)}
				<Item.Root variant="outline" size="sm">
					<Item.Media variant="icon"><Server /></Item.Media>
					<Item.Content class="min-w-0">
						<Item.Title class="flex flex-wrap items-center gap-2"
							>{server.name}{#if server.is_default}<Badge variant="secondary"
									>{m['settings.agents.default_badge']()}</Badge
								>{/if}</Item.Title
						>
						<Item.Description class="flex flex-wrap items-center gap-2">
							<span class="font-mono">{server.default_model}</span>
							{#if server.models.length}<span
									>· {m['settings.agents.model_count']({ count: server.models.length })}</span
								>{/if}
							{#each server.harnesses as harness (harness)}<AgentLogo agent={harness} size="sm" />{/each}
						</Item.Description>
					</Item.Content>
					{#if canAdmin}
						<Item.Actions>
							<Button
								size="icon-sm"
								variant="ghost"
								aria-label={m['settings.agents.edit_named']({ name: server.name })}
								onclick={() => (editing = server)}><Pencil /></Button
							>
							<Button
								size="icon-sm"
								variant="ghost"
								class="text-destructive hover:text-destructive"
								aria-label={m['settings.agents.remove_named']({ name: server.name })}
								onclick={() => (removing = server)}><Trash2 /></Button
							>
						</Item.Actions>
					{/if}
				</Item.Root>
			{/each}
		</div>
	{/if}
</section>

{#if editing !== undefined}
	<ModelServerDialog
		{slug}
		server={editing ?? undefined}
		onsaved={() => {
			editing = undefined;
			void load();
		}}
		onclose={() => (editing = undefined)}
	/>
{/if}
<AlertDialog.Root open={!!removing} onOpenChange={(open) => !open && (removing = null)}>
	<AlertDialog.Content>
		<AlertDialog.Header>
			<AlertDialog.Title>{m['settings.agents.remove_title']({ name: removing?.name ?? '' })}</AlertDialog.Title>
			<AlertDialog.Description>{m['settings.agents.remove_desc']()}</AlertDialog.Description>
		</AlertDialog.Header>
		<AlertDialog.Footer>
			<AlertDialog.Cancel>{m['common.cancel']()}</AlertDialog.Cancel>
			<AlertDialog.Action variant="destructive" disabled={removeBusy} onclick={remove}
				>{m['common.delete']()}</AlertDialog.Action
			>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>
