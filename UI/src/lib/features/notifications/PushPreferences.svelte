<script lang="ts">
	import { onMount } from 'svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Switch } from '$lib/components/ui/switch';
	import { disconnectPushDevice, enrollPushDevice, getVapidPublicKey, revokePushDevice } from '$lib/api/push';
	import { m } from '$lib/features/i18n/messages';
	import { preferencesState } from '$lib/features/preferences/preferences.state.svelte';
	import { readPermission } from './permissions';
	import { pushDeviceStorage, type PushDevice } from './push-device';
	import { needsPushHomeScreen, PUSH_DEVICE_CHANGED, subscribeToPush } from './push';

	let { workspaceSlug, userId, onpermissionchange }: {
		workspaceSlug: string;
		userId: string;
		onpermissionchange: () => void;
	} = $props();

	type State = 'loading' | 'unsupported' | 'homeScreen' | 'unavailable' | 'denied' | 'ready' | 'subscribed' | 'reenable';
	let status = $state<State>('loading');
	let busy = $state(false);
	let failed = $state(false);
	let publicKey = '';
	let registration: ServiceWorkerRegistration | undefined;
	let device: PushDevice | undefined;
	let subscription: PushSubscription | null = null;
	const labels = $derived<Record<State, string>>({
		loading: m['notify.push.loading'](),
		unsupported: m['notify.push.unsupported'](),
		homeScreen: m['notify.push.home_screen'](),
		unavailable: m['notify.push.unavailable'](),
		denied: m['notify.permission.denied'](),
		ready: m['notify.push.ready'](),
		subscribed: m['notify.push.subscribed'](),
		reenable: m['notify.push.reenable']()
	});

	async function refresh() {
		if (needsPushHomeScreen({
			userAgent: navigator.userAgent,
			maxTouchPoints: navigator.maxTouchPoints,
			standalone: (navigator as Navigator & { standalone?: boolean }).standalone,
			standaloneDisplay: matchMedia('(display-mode: standalone)').matches
		})) {
			status = 'homeScreen';
			return;
		}
		if (!navigator.serviceWorker || !('PushManager' in window) || readPermission() === 'unsupported') {
			status = 'unsupported';
			return;
		}
		if (readPermission() === 'denied') {
			status = 'denied';
			return;
		}
		try {
			[publicKey, registration] = await Promise.all([
				getVapidPublicKey(),
				navigator.serviceWorker.getRegistration()
			]);
			if (!publicKey || !registration?.active || !registration.pushManager) {
				status = 'unavailable';
				return;
			}
			[device, subscription] = await Promise.all([
				pushDeviceStorage.read(),
				registration.pushManager.getSubscription()
			]);
			if (device?.userId === userId) {
				status = !device.needsReenable && device.publicKey === publicKey && subscription ? 'subscribed' : 'reenable';
			} else {
				status = 'ready';
			}
		} catch {
			status = 'unavailable';
		}
	}

	onMount(() => {
		void refresh();
		const refreshWhenIdle = () => { if (!busy) void refresh(); };
		const onMessage = (event: MessageEvent) => {
			if (event.data?.type === PUSH_DEVICE_CHANGED) refreshWhenIdle();
		};
		window.addEventListener('focus', refreshWhenIdle);
		navigator.serviceWorker?.addEventListener('controllerchange', refreshWhenIdle);
		navigator.serviceWorker?.addEventListener('message', onMessage);
		return () => {
			window.removeEventListener('focus', refreshWhenIdle);
			navigator.serviceWorker?.removeEventListener('controllerchange', refreshWhenIdle);
			navigator.serviceWorker?.removeEventListener('message', onMessage);
		};
	});

	async function enable() {
		if (!registration || busy) return;
		busy = true;
		failed = false;
		try {
			const manager = registration.pushManager;
			if (device && (device.userId !== userId || device.publicKey !== publicKey || device.needsReenable)) {
				await subscription?.unsubscribe();
				subscription = null;
			}
			const id = await subscribeToPush(manager, publicKey, workspaceSlug, enrollPushDevice, subscription);
			try {
				await pushDeviceStorage.write({ id, userId, workspaceSlug, publicKey });
			} catch (error) {
				await revokePushDevice(id);
				throw error;
			}
		} catch {
			failed = readPermission() !== 'denied';
		} finally {
			onpermissionchange();
			await refresh();
			busy = false;
		}
	}

	async function disable() {
		if (busy) return;
		busy = true;
		failed = false;
		try {
			await disconnectPushDevice();
		} catch {
			failed = true;
		} finally {
			await refresh();
			busy = false;
		}
	}
</script>

<div class="flex flex-wrap items-center justify-between gap-4 border-t border-[var(--app-border)] px-5 py-4">
	<div class="min-w-0">
		<p class="text-sm font-medium text-[var(--color-text-primary)]">{m['notify.push.device']()}</p>
		<p class="text-xs text-[var(--color-text-secondary)]" role="status" data-testid="push-status" data-state={status}>
			{labels[status]}
		</p>
		{#if failed}
			<p class="text-xs text-[var(--color-text-secondary)]" role="alert">{m['notify.push.error']()}</p>
		{/if}
	</div>
	{#if status === 'ready' || status === 'reenable'}
		<Button variant="outline" size="sm" disabled={busy} onclick={enable}>{m['notify.push.enable']()}</Button>
	{:else if status === 'subscribed'}
		<Button variant="outline" size="sm" disabled={busy} onclick={disable}>{m['notify.push.disable']()}</Button>
	{/if}
</div>
<div class="flex items-center justify-between gap-4 border-t border-[var(--app-border)] px-5 py-4">
	<div>
		<p id="notify-push-label" class="text-sm font-medium text-[var(--color-text-primary)]">{m['notify.push.title']()}</p>
		<p class="text-xs text-[var(--color-text-secondary)]">{m['notify.push.desc']()}</p>
	</div>
	<Switch size="sm" aria-labelledby="notify-push-label" checked={preferencesState.pushNotifications}
		onCheckedChange={(enabled) => preferencesState.setPushNotifications(enabled)} />
</div>
