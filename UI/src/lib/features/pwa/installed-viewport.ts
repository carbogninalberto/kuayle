// Zoom handling for the installed app. Launched from the home screen or an
// app window, Kuayle behaves like a native app: pinch, double-tap and
// focus zoom are off. Browser tabs keep the browser's normal zoom.

// Display modes an installed web app runs in; `browser` and `minimal-ui` are tabs.
const INSTALLED_DISPLAY_MODES = ['standalone', 'fullscreen', 'window-controls-overlay'];
export const INSTALLED_APP_QUERY = INSTALLED_DISPLAY_MODES.map((mode) => `(display-mode: ${mode})`).join(', ');
// Set on <html> while installed; app.css turns off touch zoom gestures with it.
export const INSTALLED_APP_ATTRIBUTE = 'data-installed-app';

const ZOOM_KEYS = new Set(['maximum-scale', 'user-scalable']);

// The page's viewport settings with zoom locked at the initial scale.
export function installedAppViewport(content: string): string {
	const kept = content
		.split(',')
		.map((part) => part.trim())
		.filter((part) => part && !ZOOM_KEYS.has(part.split('=')[0].trim().toLowerCase()));
	return [...kept, 'maximum-scale=1', 'user-scalable=no'].join(', ');
}

interface InstalledAppEnvironment {
	matchMedia?: (query: string) => { matches: boolean };
	// iOS Safari reports home-screen launches here rather than through display-mode.
	navigator?: object & { standalone?: boolean };
}

export function isInstalledApp(environment: InstalledAppEnvironment): boolean {
	if (environment.navigator?.standalone === true) return true;
	return environment.matchMedia?.(INSTALLED_APP_QUERY).matches ?? false;
}

// Safari ignores user-scalable=no for pinches and reports them as gesture events.
function preventGesture(event: Event) {
	event.preventDefault();
}

// Applies the installed-app zoom lock and follows later display-mode changes
// (a desktop tab moved into its app window). Returns a cleanup function.
export function lockInstalledAppZoom(): () => void {
	if (typeof window === 'undefined') return () => {};
	const meta = document.querySelector<HTMLMetaElement>('meta[name="viewport"]');
	const original = meta?.getAttribute('content') ?? '';
	let locked = false;
	const apply = () => {
		const installed = isInstalledApp(window);
		if (installed === locked) return;
		locked = installed;
		meta?.setAttribute('content', installed ? installedAppViewport(original) : original);
		document.documentElement.toggleAttribute(INSTALLED_APP_ATTRIBUTE, installed);
		for (const type of ['gesturestart', 'gesturechange']) {
			if (installed) document.addEventListener(type, preventGesture, { passive: false });
			else document.removeEventListener(type, preventGesture);
		}
	};
	apply();
	const query = window.matchMedia?.(INSTALLED_APP_QUERY);
	query?.addEventListener?.('change', apply);
	return () => query?.removeEventListener?.('change', apply);
}
