import { goto } from '$app/navigation';
import { NOTIFICATION_NAVIGATE_ACK, notificationNavigatePath } from './notification-click';

let listening = false;

// The service worker asks an open page to route to a clicked notification's
// target, so the page keeps its state instead of being reloaded. The page
// confirms on the message's port; without a confirmation the worker reloads
// the window onto the target itself.
export function listenForNotificationNavigation(): void {
	if (listening || typeof navigator === 'undefined' || !('serviceWorker' in navigator)) return;
	listening = true;
	navigator.serviceWorker.addEventListener('message', (event) => {
		// Only same-origin targets are followed; anything else stays unanswered.
		const path = notificationNavigatePath(event.data, window.location.origin);
		if (!path) return;
		event.ports[0]?.postMessage({ type: NOTIFICATION_NAVIGATE_ACK });
		void goto(path).catch(() => {
			// The router refused (for example mid-navigation): load the page.
			window.location.assign(path);
		});
	});
}
