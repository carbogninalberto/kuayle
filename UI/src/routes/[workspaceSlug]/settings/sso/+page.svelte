<script lang="ts">
	import PageTitle from '$lib/components/shared/PageTitle.svelte';
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { getWorkspace } from '$lib/api/workspaces';
	import { sso, type SSOConfig, type SSODefaultRole } from '$lib/api/sso';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Password } from '$lib/components/ui/password';
	import { Switch } from '$lib/components/ui/switch';
	import * as Select from '$lib/components/ui/select';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/features/i18n/messages';
	import { Check, Copy, Loader2, ShieldCheck, Trash2 } from '@lucide/svelte';

	const slug = $derived(page.params.workspaceSlug ?? '');
	let config = $state<SSOConfig | null>(null);
	let canManage = $state(false);
	let loading = $state(true);
	let saving = $state(false);
	let copied = $state(false);

	let providerName = $state('');
	let issuerUrl = $state('');
	let clientId = $state('');
	let clientSecret = $state('');
	let allowedDomains = $state('');
	let defaultRole = $state<SSODefaultRole>('member');
	let requireSso = $state(false);
	let enabled = $state(false);

	const readOnly = $derived(config?.managed_by_instance === true || !canManage);

	function apply(next: SSOConfig) {
		config = next;
		providerName = next.provider_name;
		issuerUrl = next.issuer_url;
		clientId = next.client_id;
		clientSecret = '';
		allowedDomains = next.allowed_domains.join(', ');
		defaultRole = next.default_role === 'guest' ? 'guest' : 'member';
		requireSso = next.require_sso;
		enabled = next.enabled;
	}

	onMount(async () => {
		try {
			const workspace = await getWorkspace(slug);
			const role = workspace.current_user_role;
			canManage = role === 'owner' || role === 'admin';
			if (!canManage) return;
			apply(await sso.get(slug));
		} catch (err) {
			appToast.apiError(err, m['settings.sso.failed_load']());
		} finally {
			loading = false;
		}
	});

	function domains(value: string): string[] {
		return value
			.split(',')
			.map((domain) => domain.trim().toLowerCase())
			.filter(Boolean);
	}

	async function save(event: SubmitEvent) {
		event.preventDefault();
		if (saving || readOnly) return;
		saving = true;
		try {
			const next = await sso.save(slug, {
				provider_name: providerName.trim(),
				issuer_url: issuerUrl.trim(),
				client_id: clientId.trim(),
				...(clientSecret.trim() ? { client_secret: clientSecret } : {}),
				allowed_domains: domains(allowedDomains),
				default_role: defaultRole,
				require_sso: enabled && requireSso,
				enabled
			});
			apply(next);
			appToast.success(m['settings.sso.saved']());
		} catch (err) {
			appToast.apiError(err, m['settings.sso.failed_save']());
		} finally {
			saving = false;
		}
	}

	async function remove() {
		if (!confirm(m['settings.sso.delete_confirm']())) return;
		try {
			await sso.remove(slug);
			apply({
				issuer_url: '',
				client_id: '',
				has_client_secret: false,
				allowed_domains: [],
				default_role: 'member',
				require_sso: false,
				enabled: false,
				provider_name: '',
				managed_by_instance: false,
				redirect_uri: config?.redirect_uri ?? ''
			});
			appToast.success(m['settings.sso.deleted']());
		} catch (err) {
			appToast.apiError(err, m['settings.sso.failed_delete']());
		}
	}

	async function copyRedirectURI() {
		if (!config?.redirect_uri) return;
		try {
			await navigator.clipboard.writeText(config.redirect_uri);
			copied = true;
			setTimeout(() => (copied = false), 2000);
		} catch {
			appToast.error(m['settings.sso.failed_copy']());
		}
	}

	let roleLabel = $derived(
		defaultRole === 'guest' ? m['settings.sso.role_guest']() : m['settings.sso.role_member']()
	);
</script>

<div class="mx-auto w-full max-w-2xl space-y-6 px-4 py-8 sm:px-8 sm:py-10">
	<div class="space-y-2">
		<div class="flex items-center gap-2">
			<ShieldCheck class="size-5 text-[var(--app-accent)]" aria-hidden="true" />
			<h1 class="text-2xl font-semibold text-[var(--color-text-primary)]">{m['settings.sso.title']()}</h1>
		</div>
		<p class="text-sm text-[var(--color-text-secondary)]">{m['settings.sso.description']()}</p>
	</div>

	{#if loading}
		<div class="flex h-40 items-center justify-center text-[var(--color-text-tertiary)]">
			<Loader2 class="size-5 animate-spin" aria-hidden="true" />
		</div>
	{:else if !canManage}
		<p class="rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-4 py-3 text-sm text-[var(--color-text-secondary)]">
			{m['settings.sso.admin_only']()}
		</p>
	{:else if config}
		{#if config.managed_by_instance}
			<p class="rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-4 py-3 text-sm text-[var(--color-text-secondary)]">
				{m['settings.sso.managed_by_instance']()}
			</p>
		{/if}

		<form onsubmit={save} class="space-y-5">
			<div class="space-y-1.5">
				<Label for="sso-provider-name">{m['settings.sso.provider_name']()}</Label>
				<Input
					id="sso-provider-name"
					bind:value={providerName}
					disabled={readOnly}
					placeholder={m['settings.sso.provider_name_placeholder']()}
					autocomplete="off"
				/>
			</div>

			<div class="space-y-1.5">
				<Label for="sso-issuer">{m['settings.sso.issuer_url']()}</Label>
				<Input
					id="sso-issuer"
					bind:value={issuerUrl}
					disabled={readOnly}
					placeholder="https://idp.example.com/realms/acme"
					autocomplete="off"
				/>
				<p class="text-xs text-[var(--color-text-tertiary)]">{m['settings.sso.issuer_help']()}</p>
			</div>

			<div class="grid gap-4 sm:grid-cols-2">
				<div class="space-y-1.5">
					<Label for="sso-client-id">{m['settings.sso.client_id']()}</Label>
					<Input id="sso-client-id" bind:value={clientId} disabled={readOnly} autocomplete="off" />
				</div>
				<div class="space-y-1.5">
					<Label for="sso-client-secret">{m['settings.sso.client_secret']()}</Label>
					<Password
						id="sso-client-secret"
						bind:value={clientSecret}
						disabled={readOnly}
						autocomplete="new-password"
						placeholder={config.has_client_secret
							? m['settings.sso.secret_stored']()
							: m['settings.sso.secret_empty']()}
					/>
					<p class="text-xs text-[var(--color-text-tertiary)]">{m['settings.sso.secret_help']()}</p>
				</div>
			</div>

			<div class="space-y-1.5">
				<Label for="sso-redirect-uri">{m['settings.sso.redirect_uri']()}</Label>
				<div class="flex gap-2">
					<Input id="sso-redirect-uri" value={config.redirect_uri} readonly class="font-mono text-xs" />
					<Button type="button" variant="outline" size="sm" onclick={copyRedirectURI} aria-label={m['settings.sso.copy_redirect_uri']()}>
						{#if copied}<Check class="size-4" />{:else}<Copy class="size-4" />{/if}
					</Button>
				</div>
				<p class="text-xs text-[var(--color-text-tertiary)]">{m['settings.sso.redirect_uri_help']()}</p>
			</div>

			<div class="space-y-1.5">
				<Label for="sso-domains">{m['settings.sso.allowed_domains']()}</Label>
				<Input
					id="sso-domains"
					bind:value={allowedDomains}
					disabled={readOnly}
					placeholder="example.com, example.org"
					autocomplete="off"
				/>
				<p class="text-xs text-[var(--color-text-tertiary)]">{m['settings.sso.allowed_domains_help']()}</p>
			</div>

			<div class="space-y-1.5">
				<Label for="sso-default-role">{m['settings.sso.default_role']()}</Label>
				<Select.Root
					type="single"
					value={defaultRole}
					disabled={readOnly}
					onValueChange={(value) => value && (defaultRole = value as SSODefaultRole)}
				>
					<Select.Trigger id="sso-default-role" class="w-full sm:w-56">{roleLabel}</Select.Trigger>
					<Select.Content>
						<Select.Item value="member" label={m['settings.sso.role_member']()}>
							{m['settings.sso.role_member']()}
						</Select.Item>
						<Select.Item value="guest" label={m['settings.sso.role_guest']()}>
							{m['settings.sso.role_guest']()}
						</Select.Item>
					</Select.Content>
				</Select.Root>
				<p class="text-xs text-[var(--color-text-tertiary)]">{m['settings.sso.default_role_help']()}</p>
			</div>

			<div class="space-y-3 rounded-lg border border-[var(--app-border)] p-4">
				<div class="flex items-start justify-between gap-4">
					<div class="space-y-1">
						<Label for="sso-enabled">{m['settings.sso.enabled']()}</Label>
						<p class="text-xs text-[var(--color-text-tertiary)]">{m['settings.sso.enabled_help']()}</p>
					</div>
					<Switch id="sso-enabled" bind:checked={enabled} disabled={readOnly} />
				</div>
				<div class="flex items-start justify-between gap-4 border-t border-[var(--app-border)] pt-3">
					<div class="space-y-1">
						<Label for="sso-require">{m['settings.sso.require_sso']()}</Label>
						<p class="text-xs text-[var(--color-text-tertiary)]">{m['settings.sso.require_sso_help']()}</p>
						<p class="text-xs text-[var(--status-warning,#b45309)]">{m['settings.sso.require_sso_warning']()}</p>
					</div>
					<Switch id="sso-require" bind:checked={requireSso} disabled={readOnly || !enabled} />
				</div>
			</div>

			{#if !readOnly}
				<div class="flex flex-wrap items-center justify-between gap-3">
					<Button type="submit" disabled={saving}>
						{#if saving}<Loader2 class="size-4 animate-spin" aria-hidden="true" />{/if}
						{m['settings.sso.save']()}
					</Button>
					<Button type="button" variant="destructive" onclick={remove} disabled={saving}>
						<Trash2 class="size-4" aria-hidden="true" />
						{m['settings.sso.remove']()}
					</Button>
				</div>
			{/if}
		</form>
	{/if}
</div>

<PageTitle parts={[m['settings.sso.title'](), m['settings.title']()]} />
