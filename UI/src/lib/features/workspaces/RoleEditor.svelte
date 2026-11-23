<script lang="ts">
	import { onMount } from 'svelte';
	import {
		deleteRole,
		listRolePermissions,
		saveRole,
		type PermissionGroup,
		type WorkspaceRole
	} from '$lib/api/workspace-roles';
	import SelectField from '$lib/components/shared/SelectField.svelte';
	import { Button } from '$lib/components/ui/button';
	import { Checkbox } from '$lib/components/ui/checkbox';
	import * as Dialog from '$lib/components/ui/dialog';
	import { Input } from '$lib/components/ui/input';
	import { Textarea } from '$lib/components/ui/textarea';
	import { m } from '$lib/features/i18n/messages';
	import { getErrorMessage } from '$lib/features/toast/toast';
	import { isBuiltinRole, roleName } from './role-name';

	let {
		slug,
		role,
		roles,
		onclose,
		onchanged
	}: {
		slug: string;
		role?: WorkspaceRole;
		roles: WorkspaceRole[];
		onclose: () => void;
		onchanged: () => void;
	} = $props();

	let name = $state('');
	let description = $state('');
	let template = $state('none');
	let permissions = $state<string[]>([]);
	let groups = $state<PermissionGroup[]>([]);
	let loading = $state(true);
	let loadFailed = $state(false);
	let busy = $state(false);
	let error = $state('');
	const inUse = $derived(!!role && (role.member_count > 0 || role.team_assignments > 0));
	const templates = $derived([
		{ value: 'none', label: m['sharedComponents.create_issue.no_template']() },
		...roles.filter((r) => isBuiltinRole(r.key)).map((r) => ({ value: r.key, label: roleName(r.key) }))
	]);

	onMount(() => {
		name = role?.name ?? '';
		description = role?.description ?? '';
		permissions = [...(role?.permissions ?? [])];
		void load();
	});

	async function load() {
		loading = true;
		loadFailed = false;
		try {
			groups = await listRolePermissions(slug);
		} catch {
			loadFailed = true;
		} finally {
			loading = false;
		}
	}

	function applyTemplate(key: string) {
		template = key;
		permissions = [...(roles.find((r) => r.key === key)?.permissions ?? [])];
	}

	function areaName(area: string) {
		if (area === 'settings') return m['settings.title']();
		if (area === 'agents_and_workflows')
			return m['settings.nav.agents']() + ' / ' + m['settings.tokens.res_workflows']();
		return (m as unknown as Record<string, () => string>)[`settings.tokens.res_${area}`]?.() ?? area;
	}

	async function submit(remove = false) {
		if (busy || (remove && (!role || inUse))) return;
		busy = true;
		error = '';
		try {
			if (remove && role) await deleteRole(slug, role.key);
			else
				await saveRole(slug, role?.key, {
					name: name.trim(),
					description: description.trim(),
					permissions,
					template: template === 'none' ? undefined : template
				});
			onchanged();
			onclose();
		} catch (err) {
			error = getErrorMessage(err, m['settings.members.failed_update_role']());
		} finally {
			busy = false;
		}
	}
</script>

<Dialog.Root
	open
	onOpenChange={(next) => {
		if (!next && !busy) onclose();
	}}
>
	<Dialog.Content class="top-[5dvh] flex max-h-[90dvh] flex-col gap-0 p-0 sm:max-w-2xl" showCloseButton={!busy}>
		<Dialog.Header class="px-5 py-4">
			<Dialog.Title>{role ? m['settings.roles.edit']() : m['settings.roles.create']()}</Dialog.Title>
		</Dialog.Header>
		<div class="min-h-0 space-y-4 overflow-y-auto border-y border-[var(--app-border)] px-5 py-4">
			<p class="text-sm text-muted-foreground">{m['settings.roles.session_limits']()}</p>
			<div class="grid gap-3 sm:grid-cols-2">
				<div class="space-y-1.5">
					<label for="role-name">{m['cycles.field.name']()}</label>
					<Input id="role-name" bind:value={name} maxlength={100} disabled={busy} />
				</div>
				{#if !role}
					<div class="space-y-1.5">
						<label for="role-template">{m['sharedComponents.create_issue.template']()}</label>
						<SelectField
							id="role-template"
							bind:value={() => template, applyTemplate}
							options={templates}
							disabled={busy}
						/>
					</div>
				{/if}
			</div>
			<div class="space-y-1.5">
				<label for="role-description">{m['cycles.field.description']()}</label>
				<Textarea id="role-description" bind:value={description} maxlength={2000} rows={2} disabled={busy} />
			</div>
			{#if loading}
				<p role="status">{m['common.loading']()}</p>
			{:else if loadFailed}
				<p role="alert">{m['settings.roles.load_failed']()}</p>
				<Button variant="outline" onclick={load}>{m['settings.tokens.retry']()}</Button>
			{:else}
				{#each groups as group (group.area)}
					<fieldset class="space-y-2" disabled={busy}>
						<legend class="mb-2 font-semibold">{areaName(group.area)}</legend>
						<div class="grid gap-x-6 gap-y-3 sm:grid-cols-2">
							{#each group.permissions as permission (permission.code)}
								<div class="flex items-start gap-2.5">
									<Checkbox
										id={'grant-' + permission.code}
										class="mt-0.5"
										disabled={busy}
										aria-describedby={'hint-' + permission.code}
										checked={permissions.includes(permission.code)}
										onCheckedChange={(checked) => {
											permissions = checked
												? [...permissions, permission.code]
												: permissions.filter((p) => p !== permission.code);
										}}
									/>
									<div class="min-w-0">
										<label for={'grant-' + permission.code} class="break-all font-mono text-xs">{permission.code}</label
										>
										<p id={'hint-' + permission.code} class="text-xs leading-relaxed text-muted-foreground">
											{permission.hint}
										</p>
									</div>
								</div>
							{/each}
						</div>
					</fieldset>
				{/each}
			{/if}
		</div>
		{#if inUse}<p class="px-5 pt-3 text-xs text-muted-foreground">{m['settings.roles.in_use']()}</p>{/if}
		{#if error}<p role="alert" class="px-5 pt-3 text-sm text-destructive">{error}</p>{/if}
		<div class="flex justify-end gap-2 px-5 py-3">
			{#if role}<Button
					variant="ghost"
					class="mr-auto text-destructive"
					disabled={busy || inUse}
					title={inUse ? m['settings.roles.in_use']() : undefined}
					onclick={() => submit(true)}>{m['common.delete']()}</Button
				>{/if}
			<Button variant="outline" disabled={busy} onclick={onclose}>{m['common.cancel']()}</Button>
			<Button disabled={busy || !name.trim() || loading || loadFailed} onclick={() => submit()}
				>{m['common.save']()}</Button
			>
		</div>
	</Dialog.Content>
</Dialog.Root>
