<script lang="ts">
	import * as Alert from '$lib/components/ui/alert';
	import { Button } from '$lib/components/ui/button';
	import { m } from '$lib/features/i18n/messages';
	import type { SSOAccessAction } from './sso-rules';

	let {
		action,
		scopedWorkspaceSlug = null,
		busy = false,
		oncontinue,
		onback,
		onsignout
	}: {
		action: SSOAccessAction;
		scopedWorkspaceSlug?: string | null;
		busy?: boolean;
		oncontinue?: () => void;
		onback?: () => void;
		onsignout?: () => void;
	} = $props();

	let primary = $state<HTMLElement | null>(null);

	$effect(() => {
		primary?.focus();
	});
</script>

<div class="flex h-dvh items-center justify-center bg-[var(--color-bg)] p-6">
	<div class="w-full max-w-md space-y-3">
		<Alert.Root class="border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
			<Alert.Title>
				{action.kind === 'sso_required'
					? m['sso.access.required_title']()
					: m['settings.profile.sso_linked_title']()}
			</Alert.Title>
			<Alert.Description>
				{#if action.kind === 'sso_required'}
					{m['login.sso.required']()}
				{:else if scopedWorkspaceSlug}
					{m['sso.access.scoped_desc']({ workspace: scopedWorkspaceSlug })}
				{:else}
					{m['login.sso.error_session_scoped']()}
				{/if}
			</Alert.Description>
		</Alert.Root>

		<div class="flex flex-wrap items-center gap-2">
			{#if action.kind === 'sso_required'}
				<Button bind:ref={primary} onclick={oncontinue} disabled={busy}>
					{busy ? m['settings.profile.sso_linking']() : m['sso.access.continue']()}
				</Button>
			{:else}
				{#if scopedWorkspaceSlug}
					<Button bind:ref={primary} onclick={onback}>
						{m['sso.access.back']({ workspace: scopedWorkspaceSlug })}
					</Button>
				{:else}
					<Button bind:ref={primary} onclick={onsignout}>
						{m['sidebar.log_out']()}
					</Button>
				{/if}
				{#if scopedWorkspaceSlug}
					<Button variant="outline" onclick={onsignout}>{m['sidebar.log_out']()}</Button>
				{/if}
			{/if}
		</div>
	</div>
</div>
