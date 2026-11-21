<script lang="ts">
	import { onMount } from 'svelte';
	import { goto, replaceState } from '$app/navigation';
	import { page } from '$app/state';
	import { slack, slackInstallKey } from '$lib/api/slack';
	import { m } from '$lib/features/i18n/messages';
	import { appToast, getErrorMessage } from '$lib/features/toast/toast';
	import { Button } from '$lib/components/ui/button';

	let errorMessage = $state('');
	let back = $state('/');
	onMount(async () => {
		const code = page.url.searchParams.get('code');
		const state = page.url.searchParams.get('state');
		replaceState(page.url.pathname, page.state);
		try {
			const pending = JSON.parse(sessionStorage.getItem(slackInstallKey) ?? 'null');
			if (!pending?.slug || !state || pending.state !== state) {
				errorMessage = m['slack.connection_failed']();
				return;
			}
			sessionStorage.removeItem(slackInstallKey);
			back = `/${encodeURIComponent(pending.slug)}/settings/slack`;
			if (code) await slack.connect(pending.slug, state, code);
			else appToast.error(m['slack.connection_failed']());
			await goto(back, { replaceState: true });
		} catch (err) {
			errorMessage = getErrorMessage(err, m['slack.connection_failed']());
		}
	});
</script>

<div class="mx-auto max-w-md space-y-4 p-6">
	<h1 class="text-lg font-semibold">Slack</h1>
	{#if errorMessage}
		<p role="alert">{errorMessage}</p>
		<Button href={back} variant="outline">{m['settings.back']()}</Button>
	{:else}
		<p role="status">{m['common.loading']()}</p>
	{/if}
</div>
