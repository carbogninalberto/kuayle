<script lang="ts">
	import PageTitle from '$lib/components/shared/PageTitle.svelte';
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import {
		listForgeConnections,
		createForgeConnection,
		deleteForgeConnection,
		listForgeRepos,
		linkForgeRepo,
		unlinkForgeRepo
	} from '$lib/api/forges';
	import { getWorkspace } from '$lib/api/workspaces';
	import { hasPermission } from '$lib/security/permissions';
	import type { Role } from '$lib/security/roles';
	import type { ForgeConnection, ForgeProvider, ForgeRepo } from '$lib/types/forge';
	import { Button } from '$lib/components/ui/button';
	import { Badge } from '$lib/components/ui/badge';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import * as Select from '$lib/components/ui/select';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/features/i18n/messages';
	import { safeForgeUrl } from '$lib/security/forge-url';
	import { Check, Copy, ExternalLink, GitBranch, Loader2, Plus, Trash2 } from '@lucide/svelte';

	const slug = $derived(page.params.workspaceSlug ?? '');
	let connections = $state<ForgeConnection[]>([]);
	let repos = $state<Record<string, ForgeRepo[]>>({});
	let loading = $state(true);
	let canManage = $state(false);
	let showForm = $state(false);
	let saving = $state(false);
	let copied = $state<string | null>(null);
	let provider = $state<ForgeProvider>('gitlab');
	let name = $state('');
	let baseUrl = $state('');
	let webhookSecret = $state('');
	let accessToken = $state('');

	onMount(async () => {
		try {
			const workspace = await getWorkspace(slug);
			canManage = hasPermission(workspace.current_user_role as Role, 'workspace:manage');
			if (canManage) await loadConnections();
		} catch {
			// The page stays read-only when the role cannot be loaded.
		} finally {
			loading = false;
		}
	});

	async function loadConnections() {
		loading = true;
		try {
			connections = await listForgeConnections(slug);
			const entries = await Promise.all(
				connections.map(async (connection) => {
					const linked = await listForgeRepos(slug, connection.id).catch(() => []);
					return [connection.id, linked] as const;
				})
			);
			repos = Object.fromEntries(entries);
		} catch {
			appToast.error(m['settings.forges.failed_load']());
		} finally {
			loading = false;
		}
	}

	function resetForm() {
		provider = 'gitlab';
		name = '';
		baseUrl = '';
		webhookSecret = '';
		accessToken = '';
	}

	async function createConnection() {
		saving = true;
		try {
			const connection = await createForgeConnection(slug, {
				provider,
				name: name.trim(),
				base_url: baseUrl.trim(),
				webhook_secret: webhookSecret,
				access_token: accessToken || undefined
			});
			connections = [...connections, connection];
			repos = { ...repos, [connection.id]: [] };
			showForm = false;
			resetForm();
			appToast.success(m['settings.forges.created']());
		} catch (error) {
			appToast.apiError(error, m['settings.forges.failed_create']());
		} finally {
			saving = false;
		}
	}

	async function removeConnection(connection: ForgeConnection) {
		if (!confirm(m['settings.forges.delete_confirm']({ name: connection.name }))) return;
		try {
			await deleteForgeConnection(slug, connection.id);
			connections = connections.filter((item) => item.id !== connection.id);
			const next = { ...repos };
			delete next[connection.id];
			repos = next;
			appToast.success(m['settings.forges.deleted']());
		} catch (error) {
			appToast.apiError(error, m['settings.forges.failed_delete']());
		}
	}

	async function copyWebhook(connection: ForgeConnection) {
		try {
			await navigator.clipboard.writeText(connection.webhook_url);
			copied = connection.id;
			setTimeout(() => {
				if (copied === connection.id) copied = null;
			}, 2000);
		} catch {
			appToast.error(m['settings.forges.failed_copy']());
		}
	}

	async function addRepo(connection: ForgeConnection, event: SubmitEvent) {
		event.preventDefault();
		const form = event.currentTarget as HTMLFormElement;
		const data = new FormData(form);
		const externalId = Number(data.get('external_repo_id'));
		const fullName = String(data.get('full_name') ?? '').trim();
		if (!Number.isInteger(externalId) || externalId <= 0 || !fullName) return;
		try {
			const repo = await linkForgeRepo(slug, connection.id, {
				external_repo_id: externalId,
				full_name: fullName,
				web_url: String(data.get('web_url') ?? '').trim(),
				default_branch: String(data.get('default_branch') ?? '').trim() || 'main'
			});
			repos = { ...repos, [connection.id]: [...(repos[connection.id] ?? []), repo] };
			form.reset();
			appToast.success(m['settings.forges.repo_linked']());
		} catch (error) {
			appToast.apiError(error, m['settings.forges.failed_link']());
		}
	}

	async function removeRepo(connection: ForgeConnection, repo: ForgeRepo) {
		try {
			await unlinkForgeRepo(slug, connection.id, repo.id);
			repos = {
				...repos,
				[connection.id]: (repos[connection.id] ?? []).filter((item) => item.id !== repo.id)
			};
			appToast.success(m['settings.forges.repo_unlinked']());
		} catch (error) {
			appToast.apiError(error, m['settings.forges.failed_unlink']());
		}
	}
</script>

<PageTitle parts={[m['settings.forges.title'](), m['settings.title']()]} />

<div class="mx-auto max-w-3xl space-y-8 p-6">
	<div>
		<h2 class="text-lg font-semibold text-[var(--color-text-primary)]">{m['settings.forges.title']()}</h2>
		<p class="mt-1 text-sm text-[var(--color-text-tertiary)]">{m['settings.forges.desc']()}</p>
	</div>

	{#if loading}
		<div class="flex items-center justify-center py-12">
			<Loader2 size={20} class="animate-spin text-[var(--color-text-tertiary)]" />
		</div>
	{:else}
		{#if !canManage}
			<p class="rounded-lg border border-dashed border-[var(--app-border)] p-6 text-center text-sm text-[var(--color-text-tertiary)]">
				{m['settings.forges.admins_only']()}
			</p>
		{/if}
		{#if canManage}
			<div class="flex justify-end">
				<Button size="sm" onclick={() => (showForm = !showForm)}>
					<Plus size={14} />{m['settings.forges.add_connection']()}
				</Button>
			</div>
		{/if}

		{#if showForm && canManage}
			<form
				class="space-y-4 rounded-lg border border-[var(--app-border)] p-4"
				onsubmit={(event) => {
					event.preventDefault();
					void createConnection();
				}}
			>
				<div class="grid gap-4 sm:grid-cols-2">
					<div class="space-y-1">
						<Label for="forge-provider">{m['settings.forges.provider']()}</Label>
						<Select.Root
							type="single"
							value={provider}
							onValueChange={(value) => value && (provider = value as ForgeProvider)}
						>
							<Select.Trigger class="w-full" id="forge-provider">
								{provider}
							</Select.Trigger>
							<Select.Content>
								<Select.Item value="gitlab" label="GitLab">GitLab</Select.Item>
								<Select.Item value="gitea" label="Gitea">Gitea</Select.Item>
								<Select.Item value="forgejo" label="Forgejo">Forgejo</Select.Item>
							</Select.Content>
						</Select.Root>
					</div>
					<div class="space-y-1">
						<Label for="forge-name">{m['settings.forges.name']()}</Label>
						<Input id="forge-name" bind:value={name} placeholder={provider} />
					</div>
					<div class="space-y-1 sm:col-span-2">
						<Label for="forge-base-url">{m['settings.forges.base_url']()}</Label>
						<Input
							id="forge-base-url"
							bind:value={baseUrl}
							placeholder="https://gitlab.example.com"
							required
						/>
					</div>
					<div class="space-y-1">
						<Label for="forge-secret">{m['settings.forges.webhook_secret']()}</Label>
						<Input id="forge-secret" type="password" bind:value={webhookSecret} required />
					</div>
					<div class="space-y-1">
						<Label for="forge-token">{m['settings.forges.access_token']()}</Label>
						<Input
							id="forge-token"
							type="password"
							bind:value={accessToken}
							placeholder={m['settings.forges.access_token_optional']()}
						/>
					</div>
				</div>
				<div class="flex justify-end gap-2">
					<Button type="button" variant="outline" size="sm" onclick={() => (showForm = false)}>
						{m['settings.cancel']()}
					</Button>
					<Button type="submit" size="sm" disabled={saving}>
						{#if saving}<Loader2 size={14} class="animate-spin" />{:else}{m['settings.forges.create']()}{/if}
					</Button>
				</div>
			</form>
		{/if}

		{#if connections.length === 0}
			<p class="rounded-lg border border-dashed border-[var(--app-border)] p-6 text-center text-sm text-[var(--color-text-tertiary)]">
				{m['settings.forges.no_connections']()}
			</p>
		{/if}

		{#each connections as connection (connection.id)}
			<section class="space-y-4 rounded-lg border border-[var(--app-border)] p-4">
				<div class="flex flex-wrap items-start justify-between gap-3">
					<div class="flex items-start gap-3">
						<div class="flex h-9 w-9 items-center justify-center rounded-lg bg-[var(--color-bg-tertiary)]">
							<GitBranch size={16} class="text-[var(--color-text-secondary)]" />
						</div>
						<div>
							<div class="flex items-center gap-2">
								<span class="text-sm font-medium text-[var(--color-text-primary)]">{connection.name}</span>
								<Badge variant="outline" class="text-[10px] capitalize">{connection.provider}</Badge>
								{#if connection.has_access_token}
									<Badge variant="outline" class="text-[10px]">{m['settings.forges.token_set']()}</Badge>
								{/if}
							</div>
							<p class="text-xs text-[var(--color-text-tertiary)]">{connection.base_url}</p>
						</div>
					</div>
					{#if canManage}
						<Button variant="destructive" size="sm" onclick={() => removeConnection(connection)}>
							<Trash2 size={13} />{m['settings.forges.delete']()}
						</Button>
					{/if}
				</div>

				<div class="space-y-1">
					<p class="text-xs font-medium text-[var(--color-text-secondary)]">{m['settings.forges.webhook_url']()}</p>
					<div class="flex items-center gap-2">
						<code class="min-w-0 flex-1 truncate rounded bg-[var(--color-bg-tertiary)] px-2 py-1 text-xs">{connection.webhook_url}</code>
						<Button variant="outline" size="sm" onclick={() => copyWebhook(connection)}>
							{#if copied === connection.id}<Check size={13} />{:else}<Copy size={13} />{/if}
							{copied === connection.id ? m['settings.forges.copied']() : m['settings.forges.copy']()}
						</Button>
					</div>
				</div>

				<div class="space-y-2">
					<p class="text-xs font-medium text-[var(--color-text-secondary)]">{m['settings.forges.repos']()}</p>
					{#if (repos[connection.id] ?? []).length === 0}
						<p class="text-xs text-[var(--color-text-tertiary)]">{m['settings.forges.no_repos']()}</p>
					{:else}
						<ul class="space-y-1">
							{#each repos[connection.id] ?? [] as repo (repo.id)}
								<li class="flex items-center gap-2 rounded px-2 py-1 text-xs hover:bg-[var(--color-bg-hover)]">
									<span class="min-w-0 flex-1 truncate">{repo.full_name}</span>
									<span class="text-[var(--color-text-tertiary)]">#{repo.external_repo_id}</span>
									{#if safeForgeUrl(repo.web_url)}
										<a href={safeForgeUrl(repo.web_url) ?? undefined} target="_blank" rel="noopener noreferrer" class="text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)]">
											<ExternalLink size={12} />
										</a>
									{/if}
									{#if canManage}
										<Button variant="plain" onclick={() => removeRepo(connection, repo)} class="p-1 text-[var(--color-text-tertiary)] hover:text-[var(--color-error)]">
											<Trash2 size={12} />
										</Button>
									{/if}
								</li>
							{/each}
						</ul>
					{/if}

					{#if canManage}
						<form class="grid gap-2 sm:grid-cols-[8rem_1fr_1fr_7rem_auto]" onsubmit={(event) => addRepo(connection, event)}>
							<Input name="external_repo_id" type="number" min="1" required placeholder={m['settings.forges.external_repo_id']()} />
							<Input name="full_name" required placeholder={m['settings.forges.full_name']()} />
							<Input name="web_url" placeholder={m['settings.forges.web_url']()} />
							<Input name="default_branch" placeholder={m['settings.forges.default_branch']()} />
							<Button type="submit" size="sm">{m['settings.forges.link_repo']()}</Button>
						</form>
					{/if}
				</div>
			</section>
		{/each}

		<p class="rounded-md bg-[var(--color-bg-secondary)] px-4 py-3 text-xs text-[var(--color-text-tertiary)]">
			{m['settings.forges.rules_note']()}
		</p>
	{/if}
</div>
