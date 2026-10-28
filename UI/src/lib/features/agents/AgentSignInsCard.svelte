<script lang="ts">
	import { onMount } from 'svelte';
	import { Trash2 } from '@lucide/svelte';
	import * as AlertDialog from '$lib/components/ui/alert-dialog/index.js';
	import * as Item from '$lib/components/ui/item/index.js';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { agentSignIns, type AgentSignIn } from '$lib/api/agent-sign-ins';
	import { appToast } from '$lib/features/toast/toast';
	import { getLocale } from '$lib/paraglide/runtime.js';
	import { m } from '$lib/paraglide/messages.js';
	import { formatDate } from '$lib/utils/format';
	import AgentLogo from './AgentLogo.svelte';
	import ClaudeTokenForm from './ClaudeTokenForm.svelte';
	import { agentInfo } from './agents';
	import { signInState } from './sign-in';

	// The signed-in user's own agent sign-ins. Every user manages only theirs;
	// the API never returns the sign-ins themselves.
	let { slug }: { slug: string } = $props();
	let signIns = $state<AgentSignIn[]>([]),
		loading = $state(true),
		failed = $state(false),
		removing = $state<{ provider: string; kind: 'token' | 'credentials' } | null>(null),
		removeBusy = $state(false);

	async function load() {
		loading = true;
		failed = false;
		try {
			signIns = await agentSignIns.list(slug);
		} catch {
			failed = true;
		} finally {
			loading = false;
		}
	}
	onMount(load);

	const date = (value: string | null | undefined) => (value ? formatDate(value, getLocale()) : '');
	const badges = {
		token: { label: () => m['settings.agents.sign_in_badge_token'](), variant: 'secondary' },
		terminal: { label: () => m['settings.agents.sign_in_badge_terminal'](), variant: 'secondary' },
		expired: { label: () => m['settings.agents.sign_in_badge_expired'](), variant: 'destructive' },
		none: { label: () => m['settings.agents.sign_in_badge_none'](), variant: 'outline' }
	} as const;

	async function saved(signIn: AgentSignIn) {
		signIns = signIns.map((item) => (item.provider === signIn.provider ? signIn : item));
		appToast.success(m['settings.agents.claude_token_saved']());
	}

	async function remove() {
		if (!removing || removeBusy) return;
		removeBusy = true;
		try {
			await agentSignIns.remove(slug, removing.provider, removing.kind);
			appToast.success(m['settings.agents.sign_in_removed']());
			removing = null;
			await load();
		} catch (error) {
			appToast.apiError(error, m['settings.agents.sign_in_remove_failed']());
		} finally {
			removeBusy = false;
		}
	}
</script>

<section id="your-sign-ins" aria-labelledby="agent-sign-ins-title" class="space-y-4 rounded-xl border bg-card p-5">
	<div>
		<h2 id="agent-sign-ins-title" class="text-sm font-semibold">{m['settings.agents.sign_ins_title']()}</h2>
		<p class="text-xs text-muted-foreground">{m['settings.agents.sign_ins_desc']()}</p>
	</div>
	{#if loading}<p role="status" class="text-xs text-muted-foreground">{m['common.loading']()}</p>
	{:else if failed}
		<div class="flex items-center justify-between gap-3">
			<p role="alert" class="text-sm text-destructive">{m['settings.agents.sign_ins_failed']()}</p>
			<Button size="sm" variant="outline" onclick={load}>{m['machines.refresh']()}</Button>
		</div>
	{:else}
		<div class="space-y-3">
			{#each signIns as signIn (signIn.provider)}
				{@const name = agentInfo(signIn.provider).name}
				{@const state = signInState(signIn)}
				<div class="space-y-3 rounded-lg border p-3" data-testid="sign-in-{signIn.provider}">
					<Item.Root size="sm" class="p-0">
						<Item.Media><AgentLogo agent={signIn.provider} size="lg" /></Item.Media>
						<Item.Content class="min-w-0">
							<Item.Title class="flex flex-wrap items-center gap-2"
								>{name}<Badge variant={badges[state].variant}>{badges[state].label()}</Badge></Item.Title
							>
							{#if signIn.token}
								<Item.Description class="text-xs"
									>{signIn.token.expired
										? m['settings.agents.sign_in_token_expired_info']({
												saved: date(signIn.token.saved_at),
												expires: date(signIn.token.expires_at)
											})
										: m['settings.agents.sign_in_token_info']({
												saved: date(signIn.token.saved_at),
												expires: date(signIn.token.expires_at)
											})}</Item.Description
								>
							{/if}
							{#if signIn.terminal}
								<Item.Description class="text-xs"
									>{m['settings.agents.sign_in_terminal_info']({ saved: date(signIn.terminal.saved_at) })}</Item.Description
								>
							{/if}
						</Item.Content>
						{#if signIn.token || signIn.terminal}
							<Item.Actions class="flex-wrap justify-end">
								{#if signIn.token}<Button
										size="sm"
										variant="ghost"
										class="text-destructive hover:text-destructive"
										aria-label={m['settings.agents.remove_token_named']({ name })}
										onclick={() => (removing = { provider: signIn.provider, kind: 'token' })}
										><Trash2 />{m['settings.agents.remove_token']()}</Button
									>{/if}
								{#if signIn.terminal}<Button
										size="sm"
										variant="ghost"
										class="text-destructive hover:text-destructive"
										aria-label={m['settings.agents.remove_terminal_named']({ name })}
										onclick={() => (removing = { provider: signIn.provider, kind: 'credentials' })}
										><Trash2 />{m['settings.agents.remove_terminal']()}</Button
									>{/if}
							</Item.Actions>
						{/if}
					</Item.Root>
					{#if signIn.supports_token}
						<div class="space-y-2 text-xs text-muted-foreground">
							<p>{m['settings.agents.claude_token_help']()}</p>
							<code class="block w-fit rounded-md border bg-muted px-2 py-1 font-mono text-foreground"
								>claude setup-token</code
							>
						</div>
						<ClaudeTokenForm {slug} saved={!!signIn.token} onsaved={saved} />
						<p class="text-xs text-muted-foreground">{m['settings.agents.claude_terminal_help']()}</p>
					{:else}
						<p class="text-xs text-muted-foreground">{m['settings.agents.codex_sign_in_help']()}</p>
					{/if}
				</div>
			{/each}
		</div>
	{/if}
</section>

<AlertDialog.Root open={!!removing} onOpenChange={(open) => !open && (removing = null)}>
	<AlertDialog.Content>
		<AlertDialog.Header>
			<AlertDialog.Title>{m['settings.agents.remove_sign_in_title']()}</AlertDialog.Title>
			<AlertDialog.Description
				>{m['settings.agents.remove_sign_in_desc']({ name: agentInfo(removing?.provider ?? '').name })}</AlertDialog.Description
			>
		</AlertDialog.Header>
		<AlertDialog.Footer>
			<AlertDialog.Cancel>{m['common.cancel']()}</AlertDialog.Cancel>
			<AlertDialog.Action variant="destructive" disabled={removeBusy} onclick={remove}
				>{m['common.delete']()}</AlertDialog.Action
			>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>
