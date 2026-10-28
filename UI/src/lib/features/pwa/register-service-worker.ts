import { dev } from '$app/environment';
import { appToast } from '$lib/features/toast/toast';
import { m } from '$lib/paraglide/messages.js';
import { shouldRegisterServiceWorker } from './service-worker-policy';

// Browser tests set this key before loading the page to exercise the worker.
export const SERVICE_WORKER_OPT_IN_KEY = 'kuayle-service-worker-tests';
const UPDATE_TOAST_ID = 'kuayle-app-update';

function optedIn(): boolean {
	try {
		return localStorage.getItem(SERVICE_WORKER_OPT_IN_KEY) === '1';
	} catch {
		return false;
	}
}

// Registers the installable-app worker in production builds. When a new
// version is waiting, a toast offers to reload into it; otherwise it activates
// on the next load after every Kuayle tab has closed.
export function registerServiceWorker(): void {
	const supported = typeof navigator !== 'undefined' && 'serviceWorker' in navigator;
	if (!shouldRegisterServiceWorker({ dev, supported, webdriver: supported && navigator.webdriver, optIn: optedIn() })) return;
	const container = navigator.serviceWorker;
	let reloading = false;
	container.addEventListener('controllerchange', () => {
		// Only the user's choice reloads; the first install also changes controller.
		if (!reloading) return;
		window.location.reload();
	});

	const offerUpdate = (waiting: ServiceWorker) => {
		appToast.info(m['pwa.update_available'](), {
			id: UPDATE_TOAST_ID,
			duration: Number.POSITIVE_INFINITY,
			action: {
				label: m['pwa.reload'](),
				onclick: () => {
					reloading = true;
					waiting.postMessage({ type: 'SKIP_WAITING' });
				}
			}
		});
	};

	container
		.register('/service-worker.js', { scope: '/' })
		.then((registration) => {
			// Long-lived tabs look for a new deployment once an hour.
			setInterval(() => void registration.update().catch(() => undefined), 60 * 60 * 1000);
			// A version installed by an earlier visit may already be waiting.
			if (registration.waiting && container.controller) offerUpdate(registration.waiting);
			registration.addEventListener('updatefound', () => {
				const installing = registration.installing;
				installing?.addEventListener('statechange', () => {
					if (installing.state === 'installed' && container.controller) offerUpdate(installing);
				});
			});
		})
		.catch(() => {
			// The app works without the worker; installation simply stays unavailable.
		});
}
