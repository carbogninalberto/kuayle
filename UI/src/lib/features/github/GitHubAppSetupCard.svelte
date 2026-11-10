<script lang="ts">
	import { onMount } from 'svelte';
	import { getGitHubAppSetup } from '$lib/api/github';
	import * as Card from '$lib/components/ui/card/index.js';
	import * as Alert from '$lib/components/ui/alert/index.js';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { CircleAlert, CircleCheck, Download, ExternalLink, LoaderCircle, RefreshCw } from '@lucide/svelte';
	import { m } from '$lib/features/i18n/messages';
	import {
		GITHUB_APP_LOGO_PATH,
		githubAppLogoSettingsUrl,
		githubAppSetupView,
		type GitHubAppSetup,
		type GitHubAppSetupMessage
	} from './app-setup';

	let { slug }: { slug: string } = $props();

	let setup = $state<GitHubAppSetup | null>(null);
	let checking = $state(false);
	let failed = $state(false);
	const view = $derived(setup ? githubAppSetupView(setup) : null);
	const account = $derived(setup?.account_login || setup?.app_slug || 'GitHub');
	const logoSettingsUrl = $derived(setup ? githubAppLogoSettingsUrl(setup) : null);

	async function check() {
		if (checking) return;
		checking = true;
		try {
			setup = await getGitHubAppSetup(slug);
			failed = false;
		} catch {
			failed = true;
		} finally {
			checking = false;
		}
	}

	onMount(() => {
		void check();
	});

	// After fixing things on GitHub in another tab, coming back re-checks.
	function onWindowFocus() {
		if (failed || view?.recheckOnFocus) void check();
	}

	function message(key: GitHubAppSetupMessage): string {
		switch (key) {
			case 'ok':
				return m['settings.github.app_setup.ok']();
			case 'app_missing':
				return m['settings.github.app_setup.app_missing']();
			case 'app_missing_managed':
				return m['settings.github.app_setup.app_missing_managed']();
			case 'installation_pending':
				return m['settings.github.app_setup.installation_pending']({ account });
			case 'installation_suspended':
				return m['settings.github.app_setup.installation_suspended']({ account });
			case 'installation_missing':
				return m['settings.github.app_setup.installation_missing']();
			case 'unavailable':
				return m['settings.github.app_setup.unavailable']();
			default:
				return m['settings.github.app_setup.not_configured']();
		}
	}

	const badge = $derived.by(() => {
		switch (view?.tone) {
			case 'ok':
				return { label: m['settings.github.app_setup.badge_ok'](), variant: 'secondary' as const };
			case 'warning':
				return { label: m['settings.github.app_setup.badge_action'](), variant: 'outline' as const };
			case 'error':
				return { label: m['settings.github.app_setup.badge_problem'](), variant: 'destructive' as const };
			default:
				return { label: m['settings.github.app_setup.badge_unknown'](), variant: 'outline' as const };
		}
	});

	const checklistTitle = $derived(
		view?.message === 'app_missing_managed'
			? m['settings.github.app_setup.checklist_managed']()
			: view?.action?.kind === 'accept'
				? m['settings.github.app_setup.checklist_pending']()
				: m['settings.github.app_setup.checklist_fix']()
	);

	const actionLabel = $derived(
		view?.action?.kind === 'fix'
			? m['settings.github.app_setup.fix']()
			: view?.action?.kind === 'accept'
				? m['settings.github.app_setup.accept']()
				: m['settings.github.app_setup.open_installation']()
	);
</script>

<svelte:window onfocus={onWindowFocus} />

<Card.Root data-testid="github-app-setup">
	<Card.Header>
		<Card.Title class="text-sm">{m['settings.github.app_setup.title']()}</Card.Title>
		<Card.Description>{m['settings.github.app_setup.desc']()}</Card.Description>
		{#if view}
			<Card.Action><Badge variant={badge.variant}>{badge.label}</Badge></Card.Action>
		{/if}
	</Card.Header>
	<Card.Content class="space-y-3">
		{#if failed}
			<Alert.Root variant="destructive">
				<CircleAlert />
				<Alert.Description>{m['settings.github.app_setup.load_failed']()}</Alert.Description>
			</Alert.Root>
		{:else if !view}
			<p class="flex items-center gap-2 text-sm text-muted-foreground" role="status">
				<LoaderCircle class="size-4 animate-spin" />{m['settings.github.app_setup.checking']()}
			</p>
		{:else}
			<Alert.Root variant={view.tone === 'error' ? 'destructive' : 'default'}>
				{#if view.tone === 'ok'}<CircleCheck />{:else}<CircleAlert />{/if}
				<Alert.Description class="space-y-3">
					<p>{message(view.message)}</p>
					{#if view.checklist.length > 0}
						<div class="space-y-1">
							<p class="font-medium text-foreground">{checklistTitle}</p>
							<ul class="list-disc space-y-0.5 pl-5">
								{#each view.checklist as item (item)}<li>{item}</li>{/each}
							</ul>
						</div>
					{/if}
					{#if view.steps.length > 0}
						<ol class="list-decimal space-y-1 pl-5">
							{#each view.steps as step (step)}
								<li>
									{step === 'save_and_confirm'
										? m['settings.github.app_setup.step_save_and_confirm']()
										: m['settings.github.app_setup.step_review_and_accept']()}
								</li>
							{/each}
						</ol>
						<p class="text-xs">{m['settings.github.app_setup.recheck_hint']()}</p>
					{/if}
				</Alert.Description>
			</Alert.Root>
		{/if}
		<div class="flex flex-wrap gap-2">
			{#if view?.action}
				<Button size="sm" href={view.action.url} target="_blank" rel="noopener noreferrer">
					<ExternalLink />{actionLabel}<span class="sr-only">{m['settings.github.app_setup.opens_new_tab']()}</span>
				</Button>
			{/if}
			<Button size="sm" variant="outline" disabled={checking} onclick={() => void check()}>
				{#if checking}<LoaderCircle class="animate-spin" />{m['settings.github.app_setup.checking']()}{:else}<RefreshCw />{m[
						'settings.github.app_setup.check_again'
					]()}{/if}
			</Button>
		</div>
		{#if logoSettingsUrl}
			<div class="space-y-2 border-t pt-3" data-testid="github-app-logo">
				<p class="text-xs text-muted-foreground">{m['settings.github.app_setup.logo_hint']()}</p>
				<div class="flex flex-wrap gap-2">
					<Button size="sm" variant="outline" href={GITHUB_APP_LOGO_PATH} download="kuayle-logo.png">
						<Download />{m['settings.github.logo_download']()}
					</Button>
					<Button size="sm" variant="outline" href={logoSettingsUrl} target="_blank" rel="noopener noreferrer">
						<ExternalLink />{m['settings.github.app_setup.update_logo']()}<span class="sr-only"
							>{m['settings.github.app_setup.opens_new_tab']()}</span
						>
					</Button>
				</div>
			</div>
		{/if}
	</Card.Content>
</Card.Root>
