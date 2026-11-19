<script lang="ts" module>
	let dismissed = false;
</script>

<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { Copy, X } from '@lucide/svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import { copyText } from '$lib/features/assistant/message-text';
	import { safeLoginNext, withoutSignInToken } from '$lib/features/auth/sign-in-link';
	import { m } from '$lib/features/i18n/messages';
	import { appToast } from '$lib/features/toast/toast';
	import { browserGuide, type BrowserGuide } from './browser-guide';
	import { isInstalledApp } from './installed-viewport';

	let guide = $state<BrowserGuide>(null);

	onMount(() => {
		if (dismissed) return;
		// Telegram's iOS browser can use Safari's UA and injects its bridge at document start.
		guide = browserGuide(
			navigator.userAgent,
			isInstalledApp(window),
			'TelegramWebviewProxy' in window || 'TelegramWebviewProxyProto' in window || 'TelegramWebview' in window,
			navigator.maxTouchPoints
		);
	});

	async function copyLink() {
		const target = page.url.pathname === '/login' && safeLoginNext(page.url.searchParams.get('next'), page.url.origin);
		const url = target ? new URL(target, page.url.origin) : withoutSignInToken(new URL(page.url.href));
		if (await copyText(url.href)) appToast.success(m['common.link_copied']());
		else appToast.error(m['pwa.browser.copy_failed']());
	}

	function dismiss() {
		dismissed = true;
		guide = null;
	}
</script>

{#if guide}
	<aside class="flex shrink-0 items-center gap-2 border-b border-border bg-muted/55 px-3 py-2 text-sm text-foreground sm:px-4">
		<p class="mr-auto min-w-0">
			{#if guide === 'home-screen'}
				{m['pwa.browser.home_screen']()}
			{:else if guide.ios}
				{m['pwa.browser.in_app_ios']({ app: guide.app })}
			{:else}
				{m['pwa.browser.in_app']({ app: guide.app })}
			{/if}
		</p>
		<Button variant="outline" size="sm" class="min-h-9" onclick={copyLink}>
			<Copy aria-hidden="true" />
			{m['common.copy_link']()}
		</Button>
		<Button variant="ghost" size="icon-lg" onclick={dismiss} aria-label={m['common.dismiss']()}>
			<X aria-hidden="true" />
		</Button>
	</aside>
{/if}
